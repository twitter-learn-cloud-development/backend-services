package schema

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type Access struct {
	Table      string   `json:"table"`
	Privileges []string `json:"privileges"`
}
type AccessPlan struct {
	Role     string   `json:"role"`
	Database string   `json:"database"`
	Account  string   `json:"account"`
	Host     string   `json:"host"`
	Access   []Access `json:"access"`
	SQL      []string `json:"sql"`
}

var safeIdentifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)
var safeAccountHost = regexp.MustCompile(`^[a-zA-Z0-9.%:_-]{1,128}$`)

func RoleAccess(role string) ([]Access, error) {
	read := []string{"SELECT"}
	write := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
	appendOnly := []string{"SELECT", "INSERT", "UPDATE"}
	access := []Access{}
	add := func(table string, privileges []string) {
		access = append(access, Access{table, append([]string{}, privileges...)})
	}
	switch role {
	case "user-service":
		add("users", write)
	case "tweet-service":
		models, _ := OwnerModels("tweet")
		for _, model := range models {
			add(model.(interface{ TableName() string }).TableName(), write)
		}
		add("follows", read)
		add("outbox_events", appendOnly)
	case "follow-service":
		add("follows", write)
		add("follow_pair_locks", appendOnly)
		add("outbox_events", appendOnly)
	case "messenger-service":
		add("messages", write)
	case "notification-service":
		add("notifications", write)
	case "agent-service":
		add("agent_personas", write)
	case "consumer":
		add("tweets", read)
		add("follows", read)
		add("outbox_tasks", write)
		add("follow_author_projections", appendOnly)
		add("follow_projection_receipts", appendOnly)
	case "canal-relay":
		add("outbox_events", read)
		add("outbox_publications", appendOnly)
	default:
		return nil, errors.New("unknown runtime access role")
	}
	sort.Slice(access, func(i, j int) bool { return access[i].Table < access[j].Table })
	return access, nil
}
func BuildAccessPlan(role, database, account, host string) (AccessPlan, error) {
	if !safeIdentifier.MatchString(database) || !safeIdentifier.MatchString(account) || strings.EqualFold(account, "root") || !safeAccountHost.MatchString(host) {
		return AccessPlan{}, errors.New("invalid explicit access target")
	}
	access, err := RoleAccess(role)
	if err != nil {
		return AccessPlan{}, err
	}
	plan := AccessPlan{Role: role, Database: database, Account: account, Host: host, Access: access, SQL: []string{}}
	for _, item := range access {
		if TableOwner(item.Table) == "" {
			return AccessPlan{}, errors.New("unowned access table")
		}
		plan.SQL = append(plan.SQL, fmt.Sprintf("GRANT %s ON `%s`.`%s` TO '%s'@'%s';", strings.Join(item.Privileges, ", "), database, item.Table, account, host))
	}
	// CDC is an explicit exception. It grants replication read capability, never
	// ALL/DDL/GRANT OPTION; server-level binlog exposure must be reviewed separately.
	if role == "canal-relay" {
		plan.SQL = append(plan.SQL, fmt.Sprintf("GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO '%s'@'%s';", account, host))
	}
	return plan, nil
}
