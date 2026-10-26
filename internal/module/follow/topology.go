package follow

import "fmt"

const RelationQueue = "follow.relations.v1"

// Queue adapters return broker-specific queue metadata; keep it outside this port.
type RelationTopologyDeclarer interface {
	DeclareExchange(string, string, bool) error
	DeclareRelationQueue(string) error
	BindQueue(string, string, string) error
}

func DeclareRelationTopology(d RelationTopologyDeclarer) error {
	if err := d.DeclareExchange("twitter.events", "topic", true); err != nil {
		return fmt.Errorf("declare follow exchange: %w", err)
	}
	if err := d.DeclareRelationQueue(RelationQueue); err != nil {
		return fmt.Errorf("declare durable follow queue: %w", err)
	}
	for _, key := range []string{"user.followed", "user.unfollowed"} {
		if err := d.BindQueue(RelationQueue, key, "twitter.events"); err != nil {
			return fmt.Errorf("bind follow queue: %w", err)
		}
	}
	return nil
}
