package canal

import (
	"context"
	"encoding/json"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-redis/redis/v8"
)

// RedisPositionStore 实现 PositionStore 接口，基于 Redis 进行位点存储
type RedisPositionStore struct {
	redisClient *redis.Client
	key         string
}

func NewRedisPositionStore(client *redis.Client, key string) *RedisPositionStore {
	return &RedisPositionStore{
		redisClient: client,
		key:         key,
	}
}

func (s *RedisPositionStore) SavePosition(pos mysql.Position) error {
	data, err := json.Marshal(pos)
	if err != nil {
		return err
	}
	return s.redisClient.Set(context.Background(), s.key, data, 0).Err()
}

func (s *RedisPositionStore) GetPosition() (mysql.Position, error) {
	var pos mysql.Position
	data, err := s.redisClient.Get(context.Background(), s.key).Bytes()
	if err != nil {
		return pos, err
	}
	err = json.Unmarshal(data, &pos)
	return pos, err
}
