-- Additive manual migration. Verify InnoDB, existing unique Outbox key/lease
-- columns, lane/history indexes, DB allocator and least privileges first.
CREATE TABLE IF NOT EXISTS follow_author_projections (
  author_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  revision BIGINT UNSIGNED NOT NULL DEFAULT 0,
  epoch BIGINT UNSIGNED NOT NULL DEFAULT 0,
  celebrity BOOLEAN NOT NULL DEFAULT FALSE,
  cursor BIGINT UNSIGNED NOT NULL DEFAULT 0,
  complete BOOLEAN NOT NULL DEFAULT FALSE,
  last_repair VARCHAR(64) NOT NULL DEFAULT ''
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS follow_projection_receipts (
  event_key VARCHAR(191) NOT NULL PRIMARY KEY,
  digest CHAR(64) NOT NULL,
  registration_token VARCHAR(64) NOT NULL,
  created_at BIGINT NOT NULL
) ENGINE=InnoDB;
-- Apply only when equivalent indexes do not exist, after online DDL review.
CREATE INDEX idx_follows_author_history ON follows (followee_id,id);
CREATE INDEX idx_tweets_author_history ON tweets (user_id,id);
-- Keep ingress receipts, author state, events and tasks during rollback.
