package events

import "fmt"

const EngagementSchemaVersion = "engagement-v1"

func LikeEventKey(id uint64) string    { return fmt.Sprintf("like:%d", id) }
func CommentEventKey(id uint64) string { return fmt.Sprintf("comment:%d", id) }
