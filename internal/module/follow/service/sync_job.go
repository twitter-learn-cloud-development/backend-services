package service

import (
	"context"
	"errors"
)

// AlignCelebrityCache no longer runs a second, uncheckpointed projection owner.
// Explicit reviewed maintenance must enqueue work for the durable Worker.
func (s *FollowService) AlignCelebrityCache(context.Context) error {
	return errors.New("celebrity alignment requires reviewed durable projection tasks")
}
