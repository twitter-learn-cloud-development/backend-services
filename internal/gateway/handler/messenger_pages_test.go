package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	messengerv1 "twitter-clone/api/messenger/v1"
	userv1 "twitter-clone/api/user/v1"
)

type messengerPageClient struct {
	messengerv1.MessengerServiceClient
	err                 error
	messageRequest      *messengerv1.GetMessagesRequest
	conversationRequest *messengerv1.GetConversationsRequest
	empty               bool
}

func (c *messengerPageClient) GetMessages(_ context.Context, req *messengerv1.GetMessagesRequest, _ ...grpc.CallOption) (*messengerv1.GetMessagesResponse, error) {
	c.messageRequest = req
	if c.err != nil {
		return nil, c.err
	}
	if c.empty {
		return &messengerv1.GetMessagesResponse{}, nil
	}
	return &messengerv1.GetMessagesResponse{Messages: []*messengerv1.Message{{Id: 9007199254740997, SenderId: 2, ReceiverId: 1}, {Id: 9007199254740996, SenderId: 2, ReceiverId: 1}}, NextCursor: "9007199254740996", HasMore: true}, nil
}
func (c *messengerPageClient) GetConversations(_ context.Context, req *messengerv1.GetConversationsRequest, _ ...grpc.CallOption) (*messengerv1.GetConversationsResponse, error) {
	c.conversationRequest = req
	if c.err != nil {
		return nil, c.err
	}
	if c.empty {
		return &messengerv1.GetConversationsResponse{}, nil
	}
	return &messengerv1.GetConversationsResponse{Conversations: []*messengerv1.Conversation{{PeerId: 2, LatestMessage: &messengerv1.Message{Id: 9007199254740997, SenderId: 2, ReceiverId: 1}}}, NextCursor: "c1:1:9007199254740997:9007199254740997", HasMore: true}, nil
}

type pageProfileClient struct {
	userv1.UserServiceClient
	calls int
	ids   []uint64
}

func (c *pageProfileClient) GetBatchUsers(_ context.Context, req *userv1.GetBatchUsersRequest, _ ...grpc.CallOption) (*userv1.GetBatchUsersResponse, error) {
	c.calls++
	c.ids = req.UserIds
	return &userv1.GetBatchUsersResponse{Users: []*userv1.User{{Id: 2, Username: "peer"}}}, nil
}
func messengerPagingRouter(client *messengerPageClient, profiles *pageProfileClient, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if authenticated {
		r.Use(func(c *gin.Context) { c.Set("user_id", uint64(1)); c.Next() })
	}
	h := NewMessengerHandler(client, profiles)
	r.GET("/conversations", h.GetConversations)
	r.GET("/conversations/:peer_id/messages", h.GetMessages)
	return r
}

func TestMessengerHTTPPreservesOpaqueAndLargeCursors(t *testing.T) {
	client := &messengerPageClient{}
	profiles := &pageProfileClient{}
	r := messengerPagingRouter(client, profiles, true)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/conversations/2/messages?cursor=9007199254740999&limit=2", nil))
	if w.Code != 200 || client.messageRequest.UserId != 1 || client.messageRequest.PeerId != 2 || client.messageRequest.Cursor != "9007199254740999" {
		t.Fatalf("message request: %s %+v", w.Body, client.messageRequest)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["next_cursor"] != "9007199254740996" || profiles.calls != 1 || !reflect.DeepEqual(profiles.ids, []uint64{2}) {
		t.Fatalf("cursor/duplicate profile query: %+v", body)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/conversations?cursor=c1:1:999:900", nil))
	if w.Code != 200 || client.conversationRequest.Cursor != "c1:1:999:900" || !strings.Contains(w.Body.String(), `"next_cursor":"c1:1:9007199254740997:9007199254740997"`) {
		t.Fatalf("opaque cursor altered: %s", w.Body)
	}
}

func TestMessengerHTTPEmptyErrorsAndAuthentication(t *testing.T) {
	for _, path := range []string{"/conversations", "/conversations/2/messages"} {
		client := &messengerPageClient{empty: true}
		r := messengerPagingRouter(client, &pageProfileClient{}, true)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), ":[]") {
			t.Fatalf("empty page: %s", w.Body)
		}
		for code, want := range map[codes.Code]int{codes.InvalidArgument: 400, codes.Unavailable: 503, codes.FailedPrecondition: 503, codes.DeadlineExceeded: 504, codes.DataLoss: 500} {
			client.err = status.Error(code, "private SQL body")
			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != want || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("error: %d %s", w.Code, w.Body)
			}
		}
		w = httptest.NewRecorder()
		messengerPagingRouter(&messengerPageClient{}, &pageProfileClient{}, false).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal("anonymous history access")
		}
	}
	for _, query := range []string{"?limit=bad", "?limit=2147483648", "?limit=1&limit=2", "?cursor=1&cursor=2", "?cursor=" + strings.Repeat("x", 66)} {
		client := &messengerPageClient{}
		w := httptest.NewRecorder()
		messengerPagingRouter(client, &pageProfileClient{}, true).ServeHTTP(w, httptest.NewRequest("GET", "/conversations/2/messages"+query, nil))
		if w.Code != 400 || client.messageRequest != nil {
			t.Fatalf("malformed query reached RPC: %s", query)
		}
	}
}
