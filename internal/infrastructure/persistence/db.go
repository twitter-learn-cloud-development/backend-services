package persistence

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"strconv"
	"time"

	"twitter-clone/internal/database"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DBConfig struct {
	Host      string
	Port      int
	User      string
	Password  string
	DBName    string
	Charset   string
	ParseTime bool
	Loc       string

	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	LogLevel      logger.LogLevel
	SlowThreshold time.Duration
}

func NewDB(cfg *DBConfig) (*gorm.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return NewDBWithContext(ctx, cfg)
}

func connectionDSN(cfg *DBConfig) (string, error) {
	if cfg == nil || cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 || cfg.User == "" || cfg.DBName == "" || cfg.MaxOpenConns < 1 || cfg.MaxIdleConns < 0 || cfg.MaxIdleConns > cfg.MaxOpenConns {
		return "", errors.New("invalid database configuration")
	}
	location, err := time.LoadLocation(cfg.Loc)
	if err != nil {
		return "", errors.New("invalid database location")
	}
	driverCfg := mysqlDriver.NewConfig()
	driverCfg.User = cfg.User
	driverCfg.Passwd = cfg.Password
	driverCfg.Net = "tcp"
	driverCfg.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	driverCfg.DBName = cfg.DBName
	driverCfg.ParseTime = cfg.ParseTime
	driverCfg.Loc = location
	driverCfg.Params = map[string]string{"charset": cfg.Charset}
	driverCfg.Timeout = 5 * time.Second
	driverCfg.ReadTimeout = 5 * time.Second
	driverCfg.WriteTimeout = 5 * time.Second
	return driverCfg.FormatDSN(), nil
}

func NewDBWithContext(ctx context.Context, cfg *DBConfig) (*gorm.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dsn, err := connectionDSN(cfg)
	if err != nil {
		return nil, err
	}
	raw, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, errors.New("database configuration unavailable")
	}
	raw.SetMaxIdleConns(cfg.MaxIdleConns)
	raw.SetMaxOpenConns(cfg.MaxOpenConns)
	raw.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	raw.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	if err := raw.PingContext(ctx); err != nil {
		_ = raw.Close()
		return nil, errors.New("database connection unavailable")
	}
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: raw, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: database.NewCustomLogger(cfg.LogLevel, cfg.SlowThreshold), DisableNestedTransaction: true, PrepareStmt: true, SkipDefaultTransaction: true, DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		_ = raw.Close()
		return nil, errors.New("database adapter unavailable")
	}
	return db, nil
}

// DefaultDBConfig 返回默认的数据库配置（生产环境推荐值）
func DefaultDBConfig() *DBConfig {
	return &DBConfig{
		Host:      getEnv("DB_HOST", "127.0.0.1"),
		Port:      getEnvInt("DB_PORT", 3306),
		User:      getEnv("DB_USER", "root"),
		Password:  getEnv("DB_PASSWORD", ""),
		DBName:    getEnv("DB_NAME", "twitter"),
		Charset:   "utf8mb4",
		ParseTime: true,
		Loc:       "Local",

		// 连接池配置（高并发场景推荐值）
		MaxIdleConns:    50,               // 空闲连接数
		MaxOpenConns:    100,              // 最大连接数
		ConnMaxLifetime: 30 * time.Minute, // 连接最大生命周期
		ConnMaxIdleTime: 10 * time.Minute, // 空闲连接超时

		// 日志配置
		LogLevel:      logger.Info,            // 生产环境用 logger.Error
		SlowThreshold: 200 * time.Millisecond, // 慢查询阈值
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

// DevDBConfig 返回开发环境的数据库配置
func DevDBConfig() *DBConfig {
	cfg := DefaultDBConfig()

	// 开发环境：打印所有 SQL
	cfg.LogLevel = logger.Info

	// 开发环境：慢查询阈值更低
	cfg.SlowThreshold = 100 * time.Millisecond

	// 开发环境：连接数可以少一些
	cfg.MaxIdleConns = 10
	cfg.MaxOpenConns = 20

	return cfg
}

// ProdDBConfig 返回生产环境的数据库配置
func ProdDBConfig() *DBConfig {
	cfg := DefaultDBConfig()

	// 生产环境：只打印错误和慢查询
	cfg.LogLevel = logger.Warn

	// 生产环境：慢查询阈值可以高一些
	cfg.SlowThreshold = 500 * time.Millisecond

	// 生产环境：更多连接数
	cfg.MaxIdleConns = 50
	cfg.MaxOpenConns = 100

	return cfg
}
