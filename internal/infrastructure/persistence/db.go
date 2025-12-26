package persistence

import (
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"strconv"
	"time"
	"twitter-clone/internal/database"
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
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=%t&loc=%s", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName, cfg.Charset, cfg.ParseTime, cfg.Loc)

	//Configure GORM
	gormConfig := &gorm.Config{
		Logger: database.NewCustomLogger(cfg.LogLevel, cfg.SlowThreshold),

		//禁用外键约束（高并发场景推荐）
		DisableNestedTransaction: true,

		//预编译语句（提高性能）
		PrepareStmt: true,

		//命名策略
		NamingStrategy: nil, //使用默认策略（表名复数、字段名蛇形）

		// 【新增】跳过默认事务
		// 只有当你确信你的单条 SQL 不需要事务保护，或者你会在上层手动开启事务时才开启此项。
		// 对于推特这种读多写多，且容忍少量数据不一致的场景，开启它能显著提升写入吞吐量。
		SkipDefaultTransaction: true,
	}

	//连接数据库
	db, err := gorm.Open(mysql.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	//获取底层的 sql.DB
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	//配置连接池（非常重要！）
	// SetMaxIdleConns 设置空闲连接池中连接的最大数量
	// 推荐值：10-100（根据并发量调整）
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)

	// SetMaxOpenConns 设置打开数据库连接的最大数量
	// 推荐值：MaxIdleConns 的 2-3 倍
	// 注意：不要超过 MySQL 的 max_connections（默认151）
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)

	// SetConnMaxLifetime 设置连接可复用的最大时间
	// 推荐值：5分钟到1小时
	// 原因：防止连接过期、防止 MySQL 的 wait_timeout 导致连接失效
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// SetConnMaxIdleTime 设置连接空闲的最大时间
	// 推荐值：10分钟到30分钟
	// 原因：及时释放长时间不用的连接
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	// 6. 测试连接
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// DefaultDBConfig 返回默认的数据库配置（生产环境推荐值）
func DefaultDBConfig() *DBConfig {
	return &DBConfig{
		Host:      getEnv("DB_HOST", "127.0.0.1"),
		Port:      getEnvInt("DB_PORT", 3306),
		User:      getEnv("DB_USER", "root"),
		Password:  getEnv("DB_PASSWORD", "31415927"),
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
