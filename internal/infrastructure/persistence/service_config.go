package persistence

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

var databaseRoles = map[string]string{"user-service": "USER_SERVICE", "tweet-service": "TWEET_SERVICE", "follow-service": "FOLLOW_SERVICE", "messenger-service": "MESSENGER_SERVICE", "notification-service": "NOTIFICATION_SERVICE", "consumer": "CONSUMER", "agent-service": "AGENT_SERVICE", "canal-relay": "CANAL_RELAY"}

func ServiceDBConfig(role string) (*DBConfig, error) {
	return ResolveServiceDBConfig(DefaultDBConfig(), role, os.LookupEnv)
}
func ResolveServiceDBConfig(base *DBConfig, role string, lookup func(string) (string, bool)) (*DBConfig, error) {
	prefix, ok := databaseRoles[role]
	if !ok || base == nil || lookup == nil {
		return nil, errors.New("invalid database role")
	}
	cfg := *base
	env, _ := lookup("APP_ENV")
	strict := strings.EqualFold(env, "production")
	raw, exists := lookup("DB_REQUIRE_SERVICE_CREDENTIALS")
	if exists && raw != "" {
		requested, err := strconv.ParseBool(raw)
		if err != nil || (strict && !requested) {
			return nil, errors.New("invalid database credential policy")
		}
		strict = requested
	}
	user, userSet := lookup(prefix + "_DB_USER")
	password, passwordSet := lookup(prefix + "_DB_PASSWORD")
	if strict && (!userSet || !passwordSet || strings.TrimSpace(user) == "" || password == "" || strings.EqualFold(user, "root")) {
		return nil, errors.New("dedicated non-root database credentials are required for " + role)
	}
	// Credentials are a pair: never combine a role user with a legacy password.
	if userSet != passwordSet || (userSet && (strings.TrimSpace(user) == "" || password == "")) {
		return nil, errors.New("incomplete dedicated database credentials for " + role)
	}
	if userSet {
		cfg.User = user
		cfg.Password = password
	}
	return &cfg, nil
}
