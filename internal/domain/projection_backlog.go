package domain

import "context"

type ProjectionBacklog struct {
	TaskType string
	State    string
	Count    int64
	OldestAt int64
}
type ProjectionBacklogRepository interface {
	ProjectionBacklog(context.Context) ([]ProjectionBacklog, error)
}
