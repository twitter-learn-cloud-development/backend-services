-- Manual additive migration; review before applying, no application AutoMigrate.
-- Freeze all old Follow writers. Inspect duplicate active relations first:
-- SELECT follower_id,followee_id,COUNT(*) FROM follows WHERE deleted_at=0
-- GROUP BY follower_id,followee_id HAVING COUNT(*)>1;
-- Do not delete/merge duplicates automatically; affected pairs fail closed.
CREATE TABLE IF NOT EXISTS follow_pair_locks (
  follower_id BIGINT UNSIGNED NOT NULL,
  followee_id BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (follower_id,followee_id)
) ENGINE=InnoDB;
-- Inspect and apply only if no equivalent index exists; no repeated CREATE INDEX.
CREATE INDEX idx_follows_active_pair ON follows (follower_id,followee_id,deleted_at,id);
-- Existing outbox_events and uk_outbox_events_dedup_key must already be present,
-- use the reviewed existing Outbox migration rather than destructive recreation.
-- Keep rows/indexes/events on application rollback; locks are not age-GC data.
