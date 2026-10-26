package repository

import (
	"context"
	"errors"
	"twitter-clone/internal/domain"
)

func (r *outboxRepo) ProjectionBacklog(ctx context.Context) ([]domain.ProjectionBacklog, error) {
	var rows []domain.ProjectionBacklog
	err := r.db.WithContext(ctx).Raw(`SELECT /*+ MAX_EXECUTION_TIME(2000) */ task_type,
CASE WHEN status = 3 THEN 'processing' WHEN status = 2 AND retries >= max_retries THEN 'exhausted' WHEN status = 2 THEN 'failed' ELSE 'pending' END AS state,
COUNT(*) AS count, MIN(created_at) AS oldest_at
FROM outbox_tasks WHERE task_type IN ('sync_es','sync_text','sync_vector','delete_es','delete_vector') AND status IN (0,2,3)
GROUP BY task_type,state`).Scan(&rows).Error
	if err != nil {
		return nil, errors.New("projection backlog unavailable")
	}
	return rows, nil
}
