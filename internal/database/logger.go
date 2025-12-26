package database

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"gorm.io/gorm/logger"
	"time"
)

type CustomLogger struct {
	ZapLogger                 *zap.Logger
	LogLevel                  logger.LogLevel
	SlowThreshold             time.Duration
	IgnoreRecordNotFoundError bool
}

func NewCustomLogger(logLevel logger.LogLevel, slowThreshold time.Duration) logger.Interface {
	//create zap logger
	var zapLogger *zap.Logger
	var err error

	if logLevel == logger.Info {
		//Development environment
		zapLogger, err = zap.NewDevelopment()
	} else {
		//Production environment
		zapLogger, err = zap.NewProduction()
	}

	if err != nil {
		panic(fmt.Sprintf("failed to create zap logger: %v", err))
	}

	return &CustomLogger{
		ZapLogger:                 zapLogger,
		LogLevel:                  logLevel,
		SlowThreshold:             slowThreshold,
		IgnoreRecordNotFoundError: true,
	}
}

// logMode achieve logger.Interface
func (l *CustomLogger) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := *l
	newLogger.LogLevel = level
	return &newLogger
}

// Info achieve logger.Interface
func (l *CustomLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= logger.Info {
		l.ZapLogger.Sugar().Infof(msg, data...)
	}
}

// Warn achieve logger.Interface
func (l *CustomLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= logger.Warn {
		l.ZapLogger.Sugar().Warnf(msg, data...)
	}
}

// Error achieve logger.Interface
func (l *CustomLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= logger.Error {
		l.ZapLogger.Sugar().Errorf(msg, data...)
	}
}

// trace achieve logger.Interface(The most important way to reword SQL)
func (l *CustomLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.LogLevel <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && l.LogLevel >= logger.Error && (!l.IgnoreRecordNotFoundError || err != logger.ErrRecordNotFound):
		l.ZapLogger.Error("SQL Error",
			zap.Error(err),
			zap.Duration("elapsed", elapsed),
			zap.String("sql", sql),
			zap.Int64("rows", rows),
		)
	case elapsed > l.SlowThreshold && l.SlowThreshold != 0 && l.LogLevel >= logger.Warn:
		l.ZapLogger.Warn("Slow SQL",
			zap.Duration("elapsed", elapsed),
			zap.String("sql", sql),
			zap.Int64("rows", rows),
		)
	case l.LogLevel == logger.Info:
		l.ZapLogger.Info("SQL",
			zap.Duration("elapsed", elapsed),
			zap.String("sql", sql),
			zap.Int64("rows", rows),
		)
	}
}
