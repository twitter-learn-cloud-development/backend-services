package mongo

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var MongoClient *mongo.Client

// InitMongoDB 初始化 MongoDB 客户端
// 支持通过环境变量配置连接参数，兼容 Docker 部署
func InitMongoDB() {
	mongoHost := getEnv("MONGO_HOST", "localhost")
	mongoPort := getEnv("MONGO_PORT", "27017")
	mongoPassword := getEnv("MONGO_PASSWORD", "")
	mongoUser := getEnv("MONGO_USER", "root")
	mongoDBName := getEnv("MONGO_DB_NAME", "twitter_agent")

	uri := fmt.Sprintf("mongodb://%s:%s@%s:%s/%s?authSource=admin",
		mongoUser, mongoPassword, mongoHost, mongoPort, mongoDBName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		panic(fmt.Sprintf("failed to connect to mongodb: %v", err))
	}
	if err := client.Ping(ctx, nil); err != nil {
		panic(fmt.Sprintf("failed to ping mongodb: %v", err))
	}

	MongoClient = client
	log.Printf("✅ MongoDB connected (host=%s, db=%s)", mongoHost, mongoDBName)
}

// GetDB 获取指定数据库实例
func GetDB() *mongo.Database {
	dbName := getEnv("MONGO_DB_NAME", "twitter_agent")
	return MongoClient.Database(dbName)
}

// GetCollection 获取指定集合
func GetCollection(collectionName string) *mongo.Collection {
	return GetDB().Collection(collectionName)
}

// Close 关闭 MongoDB 连接
func Close() {
	if MongoClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := MongoClient.Disconnect(ctx); err != nil {
			log.Printf("⚠️ MongoDB disconnect error: %v", err)
		}
	}
}

func getEnv(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
