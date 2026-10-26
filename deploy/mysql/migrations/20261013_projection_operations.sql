-- Explicit additive migration; not auto-applied by the command or services.
-- Inspect existing indexes/table, lock waits and MySQL DDL support first.
CREATE TABLE IF NOT EXISTS outbox_publications (
  event_id BIGINT UNSIGNED NOT NULL,
  event_type VARCHAR(100) NOT NULL,
  payload_digest CHAR(64) NOT NULL,
  confirmed_at BIGINT NOT NULL,
  operation VARCHAR(64) NOT NULL DEFAULT '',
  operator_digest VARCHAR(64) NOT NULL DEFAULT '',
  reason_digest VARCHAR(64) NOT NULL DEFAULT '',
  PRIMARY KEY (event_id),
  KEY idx_outbox_publication_confirmed (confirmed_at)
) ENGINE=InnoDB;

-- Apply only if no equivalent lane index exists. Retain on application rollback.
CREATE INDEX idx_outbox_projection_lane ON outbox_tasks (task_type, status, id);
