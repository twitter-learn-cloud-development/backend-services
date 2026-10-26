package schema

import (
	"errors"
	"gorm.io/gorm/schema"
	"reflect"
	"sort"
	"sync"
	"twitter-clone/internal/domain"
)

type Index struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}
type Column struct {
	Name     string `json:"name"`
	Unsigned bool   `json:"unsigned"`
	Signed64 bool   `json:"signed_64"`
	NotNull  bool   `json:"not_null"`
}
type Table struct {
	Name    string   `json:"name"`
	Owner   string   `json:"owner"`
	Columns []Column `json:"columns"`
	Indexes []Index  `json:"indexes"`
}

var owners = map[string][]interface{}{
	"user":      {&domain.User{}},
	"tweet":     {&domain.Tweet{}, &domain.TweetCreateIdempotency{}, &domain.TweetSubjectWriteFence{}, &domain.TweetSubjectDeletionOperation{}, &domain.Like{}, &domain.Comment{}, &domain.Retweet{}, &domain.Bookmark{}, &domain.Poll{}, &domain.PollOption{}, &domain.PollVote{}},
	"follow":    {&domain.Follow{}, &domain.FollowPairLock{}},
	"messenger": {&domain.Message{}}, "notification": {&domain.Notification{}},
	"outbox": {&domain.OutboxEvent{}, &domain.OutboxTask{}, &domain.OutboxPublication{}}, "agent": {&domain.AgentPersona{}},
}

func OwnerModels(owner string) ([]interface{}, error) {
	models, ok := owners[owner]
	if !ok {
		return nil, errors.New("unknown schema owner")
	}
	return append([]interface{}{}, models...), nil
}
func OwnerNames() []string {
	names := []string{}
	for name := range owners {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func TableOwner(name string) string {
	for owner, models := range owners {
		for _, model := range models {
			if model.(interface{ TableName() string }).TableName() == name {
				return owner
			}
		}
	}
	if name == "follow_author_projections" || name == "follow_projection_receipts" {
		return "follow"
	}
	return ""
}
func RuntimeModels(role string) ([]interface{}, error) {
	owned := func(owner string) []interface{} { models, _ := OwnerModels(owner); return models }
	switch role {
	case "user-service":
		return owned("user"), nil
	case "tweet-service":
		return append(owned("tweet"), &domain.Follow{}, &domain.OutboxEvent{}), nil
	case "follow-service":
		return append(owned("follow"), &domain.OutboxEvent{}), nil
	case "messenger-service":
		return owned("messenger"), nil
	case "notification-service":
		return owned("notification"), nil
	case "agent-service":
		return owned("agent"), nil
	case "consumer":
		return []interface{}{&domain.Tweet{}, &domain.Follow{}, &domain.OutboxTask{}}, nil
	case "canal-relay":
		return []interface{}{&domain.OutboxEvent{}}, nil
	}
	return nil, errors.New("unknown runtime role")
}
func allowedRuntimeTable(role, name string) bool {
	models, err := RuntimeModels(role)
	if err != nil {
		return false
	}
	for _, model := range models {
		if model.(interface{ TableName() string }).TableName() == name {
			return true
		}
	}
	return (role == "consumer" && (name == "follow_author_projections" || name == "follow_projection_receipts")) || (role == "canal-relay" && name == "outbox_publications")
}
func Describe(models []interface{}) ([]Table, error) {
	if len(models) == 0 || len(models) > 32 {
		return nil, errors.New("schema table bound exceeded")
	}
	tables := []Table{}
	seen := map[string]bool{}
	for _, model := range models {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			return nil, errors.New("invalid schema model")
		}
		owner := TableOwner(parsed.Table)
		if owner == "" || seen[parsed.Table] {
			return nil, errors.New("unowned or repeated table")
		}
		seen[parsed.Table] = true
		table := Table{Name: parsed.Table, Owner: owner, Columns: []Column{}, Indexes: []Index{}}
		for _, field := range parsed.Fields {
			if field.DBName != "" {
				table.Columns = append(table.Columns, Column{Name: field.DBName, Unsigned: field.GORMDataType == schema.Uint, Signed64: field.IndirectFieldType.Kind() == reflect.Int64, NotNull: field.NotNull || field.PrimaryKey})
			}
		}
		if len(parsed.PrimaryFields) > 0 {
			index := Index{Name: "PRIMARY", Unique: true}
			for _, field := range parsed.PrimaryFields {
				index.Columns = append(index.Columns, field.DBName)
			}
			table.Indexes = append(table.Indexes, index)
		}
		for _, index := range parsed.ParseIndexes() {
			item := Index{Name: index.Name, Unique: index.Class == "UNIQUE"}
			for _, field := range index.Fields {
				item.Columns = append(item.Columns, field.DBName)
			}
			table.Indexes = append(table.Indexes, item)
		}
		// Reviewed read indexes that are not encoded in the legacy GORM models.
		switch table.Name {
		case "messages":
			table.Indexes = append(table.Indexes, Index{Name: "idx_messages_conversation_id", Columns: []string{"conversation_id", "id"}}, Index{Name: "idx_messages_unread_page", Columns: []string{"receiver_id", "is_read", "conversation_id", "id"}})
		case "follows":
			table.Indexes = append(table.Indexes, Index{Name: "idx_follows_active_pair", Columns: []string{"follower_id", "followee_id", "deleted_at", "id"}}, Index{Name: "idx_follows_author_history", Columns: []string{"followee_id", "id"}})
		case "tweets":
			table.Indexes = append(table.Indexes, Index{Name: "idx_tweets_author_history", Columns: []string{"user_id", "id"}})
		case "outbox_tasks":
			table.Indexes = append(table.Indexes, Index{Name: "idx_outbox_projection_lane", Columns: []string{"task_type", "status", "id"}})
		}
		sort.Slice(table.Columns, func(i, j int) bool { return table.Columns[i].Name < table.Columns[j].Name })
		sort.Slice(table.Indexes, func(i, j int) bool { return table.Indexes[i].Name < table.Indexes[j].Name })
		tables = append(tables, table)
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables, nil
}
