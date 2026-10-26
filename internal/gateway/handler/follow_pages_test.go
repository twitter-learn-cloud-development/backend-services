package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	followv1 "twitter-clone/api/follow/v1"
	"twitter-clone/internal/domain"
	followgrpc "twitter-clone/internal/module/follow/grpc"
	"twitter-clone/internal/module/follow/service"
	"twitter-clone/pkg/logger"
)

const followRelationCursor uint64 = 9007199254740993

type httpRelationRepository struct{ domain.FollowRepository }

func (*httpRelationRepository) ListFollowerPage(_ context.Context, _ uint64, cursor uint64, limit int) (domain.FollowerPage, error) {
	if cursor == 0 {
		return domain.FollowerPage{FollowerIDs: []uint64{8}, NextCursor: followRelationCursor, HasMore: true}, nil
	}
	if cursor == followRelationCursor {
		return domain.FollowerPage{FollowerIDs: []uint64{9007199254740997}}, nil
	}
	return domain.FollowerPage{}, nil
}
func (*httpRelationRepository) ListFolloweePage(_ context.Context, _ uint64, cursor uint64, limit int) (domain.FolloweePage, error) {
	if cursor == 0 {
		return domain.FolloweePage{FolloweeIDs: []uint64{3}, NextCursor: followRelationCursor, HasMore: true}, nil
	}
	if cursor == followRelationCursor {
		return domain.FolloweePage{FolloweeIDs: []uint64{9007199254740997}}, nil
	}
	return domain.FolloweePage{}, nil
}

type followPageClient struct {
	followv1.FollowServiceClient
	srv    *followgrpc.FollowServer
	err    error
	calls  int
	cursor uint64
}

func (c *followPageClient) GetFollowers(ctx context.Context, req *followv1.GetFollowersRequest, _ ...grpc.CallOption) (*followv1.GetFollowersResponse, error) {
	c.calls++
	c.cursor = req.Cursor
	if c.err != nil {
		return nil, c.err
	}
	return c.srv.GetFollowers(ctx, req)
}
func (c *followPageClient) GetFollowees(ctx context.Context, req *followv1.GetFolloweesRequest, _ ...grpc.CallOption) (*followv1.GetFolloweesResponse, error) {
	c.calls++
	c.cursor = req.Cursor
	if c.err != nil {
		return nil, c.err
	}
	return c.srv.GetFollowees(ctx, req)
}

func followPageRouter(client *followPageClient) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewFollowHandler(client)
	r.GET("/users/:id/followers", h.GetFollowers)
	r.GET("/users/:id/followees", h.GetFollowees)
	return r
}

func TestFollowHTTPRelationCursorRoundTrip(t *testing.T) {
	logger.InitLogger()
	for _, direction := range []string{"followers", "followees"} {
		t.Run(direction, func(t *testing.T) {
			client := &followPageClient{srv: followgrpc.NewFollowServer(service.NewFollowService(&httpRelationRepository{}, nil, nil, nil))}
			r := followPageRouter(client)
			first := httptest.NewRecorder()
			r.ServeHTTP(first, httptest.NewRequest("GET", "/users/42/"+direction+"?limit=1", nil))
			if first.Code != http.StatusOK {
				t.Fatalf("first page: %d %s", first.Code, first.Body)
			}
			var body map[string]interface{}
			if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["next_cursor"] != "9007199254740993" || body["has_more"] != true {
				t.Fatalf("relation precision lost: %+v", body)
			}
			second := httptest.NewRecorder()
			r.ServeHTTP(second, httptest.NewRequest("GET", "/users/42/"+direction+"?limit=1&cursor="+body["next_cursor"].(string), nil))
			if second.Code != http.StatusOK {
				t.Fatalf("second page: %d %s", second.Code, second.Body)
			}
			if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			field := "followee_ids"
			if direction == "followers" {
				field = "follower_ids"
			}
			if !reflect.DeepEqual(body[field], []interface{}{"9007199254740997"}) || body["next_cursor"] != "0" || body["has_more"] != false || client.cursor != followRelationCursor {
				t.Fatalf("cursor returned wrong users: %+v", body)
			}
			last := httptest.NewRecorder()
			r.ServeHTTP(last, httptest.NewRequest("GET", "/users/42/"+direction+"?cursor=1", nil))
			if last.Code != http.StatusOK || !strings.Contains(last.Body.String(), `"`+field+`":[]`) {
				t.Fatalf("empty list not []: %s", last.Body)
			}
		})
	}
}

func TestFollowHTTPRejectsMalformedPagination(t *testing.T) {
	for _, direction := range []string{"followers", "followees"} {
		for _, path := range []string{"/users/0/", "/users/not-id/", "/users/42/"} {
			for _, query := range []string{"?cursor=invalid", "?cursor=-1", "?cursor=18446744073709551616", "?cursor=01", "?cursor=", "?cursor=0&cursor=5", "?limit=", "?limit=1&limit=2", "?limit=bad", "?limit=2147483648"} {
				client := &followPageClient{}
				r := followPageRouter(client)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest("GET", path+direction+query, nil))
				if w.Code != http.StatusBadRequest || client.calls != 0 {
					t.Fatalf("invalid input reached RPC: %s%s %d", path, query, w.Code)
				}
			}
		}
	}
}

func TestFollowHTTPLowSensitivityErrors(t *testing.T) {
	for _, direction := range []string{"followers", "followees"} {
		for code, want := range map[codes.Code]int{codes.InvalidArgument: 400, codes.FailedPrecondition: 503, codes.Unavailable: 503, codes.DeadlineExceeded: 504, codes.Internal: 500} {
			client := &followPageClient{err: status.Error(code, "private database detail")}
			r := followPageRouter(client)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/users/42/"+direction, nil))
			if w.Code != want || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("error leaked/mapped incorrectly: %d %s", w.Code, w.Body)
			}
		}
	}
}
