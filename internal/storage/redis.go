package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type KVStore interface {
	Ping(ctx context.Context) error
	DBSize(ctx context.Context) (int64, error)
	SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error
	GetJSON(ctx context.Context, key string, out any) error
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, key string) error
	Close() error
}

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
	if r == nil || r.client == nil {
		return fmt.Errorf("redis is not configured")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, payload, ttl).Err()
}

func (r *RedisStore) GetJSON(ctx context.Context, key string, out any) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("redis is not configured")
	}
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (r *RedisStore) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	if r == nil || r.client == nil {
		return false, fmt.Errorf("redis is not configured")
	}
	return r.client.SetNX(ctx, key, value, ttl).Result()
}

func (r *RedisStore) Delete(ctx context.Context, key string) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("redis is not configured")
	}
	return r.client.Del(ctx, key).Err()
}

func (r *RedisStore) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}
