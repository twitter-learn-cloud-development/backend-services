package mongo

import (
	"context"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var MongoClient *mongo.Client

func InitMongoDB() {
	mongoPassword := getEnv("MONGO_PASSWORD", "")
	uri := "mongodb://root:" + mongoPassword + "@localhost:27017/twitter_agent?authSource=admin"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		panic(err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		panic(err)
	}
	MongoClient = client
}

func GetCollection(collectionName string) *mongo.Collection {
	return MongoClient.Database("twitter_agent").Collection(collectionName)
}

func getEnv(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
