package project

import (
	"context"
	"errors"
	"fmt"

	userv1 "twitter-clone/api/user/v1"
)

type GRPCUserDirectory struct {
	client userv1.UserServiceClient
}

func NewGRPCUserDirectory(client userv1.UserServiceClient) *GRPCUserDirectory {
	return &GRPCUserDirectory{client: client}
}

func (directory *GRPCUserDirectory) UserExists(ctx context.Context, userID uint64) (bool, error) {
	if directory == nil || directory.client == nil {
		return false, errors.New("User Service client is unavailable")
	}
	if userID == 0 {
		return false, nil
	}
	response, err := directory.client.GetBatchUsers(ctx, &userv1.GetBatchUsersRequest{UserIds: []uint64{userID}})
	if err != nil {
		return false, fmt.Errorf("query User Service: %w", err)
	}
	for _, user := range response.GetUsers() {
		if user.GetId() == userID {
			return true, nil
		}
	}
	return false, nil
}
