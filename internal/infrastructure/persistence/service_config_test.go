package persistence

import (
	"context"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestServiceCredentialsPairAndProductionPolicy(t *testing.T) {
	base := &DBConfig{User: "legacy", Password: "legacy-secret"}
	for role, prefix := range databaseRoles {
		values := map[string]string{"APP_ENV": "production", prefix + "_DB_USER": "role_account", prefix + "_DB_PASSWORD": "role-secret"}
		lookup := func(key string) (string, bool) { v, ok := values[key]; return v, ok }
		cfg, err := ResolveServiceDBConfig(base, role, lookup)
		require.NoError(t, err)
		require.Equal(t, "role_account", cfg.User)
		require.Equal(t, "legacy", base.User)
		delete(values, prefix+"_DB_PASSWORD")
		_, err = ResolveServiceDBConfig(base, role, lookup)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
		values[prefix+"_DB_PASSWORD"] = "role-secret"
		values[prefix+"_DB_USER"] = "root"
		_, err = ResolveServiceDBConfig(base, role, lookup)
		require.Error(t, err)
		values[prefix+"_DB_USER"] = "role_account"
		values["DB_REQUIRE_SERVICE_CREDENTIALS"] = "false"
		_, err = ResolveServiceDBConfig(base, role, lookup)
		require.Error(t, err)
	}
	cfg, err := ResolveServiceDBConfig(base, "user-service", func(string) (string, bool) { return "", false })
	require.NoError(t, err)
	require.Equal(t, base, cfg)
}
func TestDatabaseDriverConfigEscapesCredentialsAndBoundsIO(t *testing.T) {
	cfg := DefaultDBConfig()
	cfg.Host = "::1"
	cfg.User = "role"
	cfg.Password = "p@ss:/?&secret"
	cfg.DBName = "twitter"
	dsn, err := connectionDSN(cfg)
	require.NoError(t, err)
	decoded, err := mysqlDriver.ParseDSN(dsn)
	require.NoError(t, err)
	require.Equal(t, cfg.Password, decoded.Passwd)
	require.Equal(t, "[::1]:3306", decoded.Addr)
	require.Equal(t, 5*time.Second, decoded.Timeout)
	require.Equal(t, 5*time.Second, decoded.ReadTimeout)
	require.Equal(t, 5*time.Second, decoded.WriteTimeout)
	require.False(t, decoded.MultiStatements)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewDBWithContext(ctx, cfg)
	require.ErrorIs(t, err, context.Canceled)
}
