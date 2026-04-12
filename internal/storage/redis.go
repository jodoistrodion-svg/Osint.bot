package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(addr, password string) *RedisStore {
	return &RedisStore{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       0,
		}),
	}
}

func (r *RedisStore) Ping(ctx context.Context) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("redis is not configured")
	}
	return r.client.Ping(ctx).Err()
}

func (r *RedisStore) DBSize(ctx context.Context) (int64, error) {
	if r == nil || r.client == nil {
		return 0, fmt.Errorf("redis is not configured")
	}
	return r.client.DBSize(ctx).Result()
}

func (r *RedisStore) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, payload, ttl).Err()
}

func (r *RedisStore) GetJSON(ctx context.Context, key string, out any) error {
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
