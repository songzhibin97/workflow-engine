package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/songzhibin97/workflow-engine/types"
)

const (
	workflowPrefix = "workflow:"
	instancePrefix = "instance:"
)

// ErrNotFound is returned when a requested resource is not found.
var ErrNotFound = errors.New("resource not found")

// RedisStorage is a Redis-backed implementation of the Storage interface.
type RedisStorage struct {
	client *redis.Client
}

// RedisOptions extends redis.Options with additional configuration.
type RedisOptions struct {
	Addr         string
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
	IdleTimeout  time.Duration
}

// NewRedisStorage creates a new RedisStorage instance with configurable options.
func NewRedisStorage(opts RedisOptions) (*RedisStorage, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		PoolSize:     opts.PoolSize,
		MinIdleConns: opts.MinIdleConns,
		IdleTimeout:  opts.IdleTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Ping(ctx).Result(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %v", err)
	}

	return &RedisStorage{client: client}, nil
}

// withContextError handles context cancellation for operations that only return an error.
func withContextError(ctx context.Context, fn func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fn()
	}
}

// saveToRedis saves a value to Redis with the given key prefix and ID.
func (s *RedisStorage) saveToRedis(ctx context.Context, prefix string, id uint64, value interface{}) error {
	return withContextError(ctx, func() error {
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal %s%d: %v", prefix, id, err)
		}
		key := fmt.Sprintf("%s%d", prefix, id)
		if err := s.client.Set(ctx, key, data, 0).Err(); err != nil {
			return fmt.Errorf("failed to set %s in Redis: %v", key, err)
		}
		return nil
	})
}

// getFromRedis retrieves and unmarshals a value from Redis with the given key prefix and ID.
func getFromRedis[T any](ctx context.Context, client *redis.Client, prefix string, id uint64) (T, error) {
	return withContext(ctx, func() (T, error) {
		var zero T
		key := fmt.Sprintf("%s%d", prefix, id)
		data, err := client.Get(ctx, key).Bytes()
		if err == redis.Nil {
			return zero, fmt.Errorf("%w: key=%s", ErrNotFound, key)
		} else if err != nil {
			return zero, fmt.Errorf("failed to get %s from Redis: %v", key, err)
		}

		var result T
		if err := json.Unmarshal(data, &result); err != nil {
			return zero, fmt.Errorf("failed to unmarshal %s: %v", key, err)
		}
		return result, nil
	})
}

// SaveWorkflow saves a workflow to Redis.
func (s *RedisStorage) SaveWorkflow(ctx context.Context, wf types.Workflow) error {
	return s.saveToRedis(ctx, workflowPrefix, wf.ID, wf)
}

// GetWorkflow retrieves a workflow from Redis.
func (s *RedisStorage) GetWorkflow(ctx context.Context, id uint64) (types.Workflow, error) {
	return getFromRedis[types.Workflow](ctx, s.client, workflowPrefix, id)
}

// SaveInstance saves a workflow instance to Redis.
func (s *RedisStorage) SaveInstance(ctx context.Context, inst types.WorkflowInstance) error {
	return s.saveToRedis(ctx, instancePrefix, inst.ID, inst)
}

// GetInstance retrieves a workflow instance from Redis.
func (s *RedisStorage) GetInstance(ctx context.Context, id uint64) (types.WorkflowInstance, error) {
	return getFromRedis[types.WorkflowInstance](ctx, s.client, instancePrefix, id)
}

// SaveWorkflows saves multiple workflows to Redis using pipelining.
func (s *RedisStorage) SaveWorkflows(ctx context.Context, wfs []types.Workflow) error {
	return withContextError(ctx, func() error {
		pipe := s.client.Pipeline()
		for _, wf := range wfs {
			data, err := json.Marshal(wf)
			if err != nil {
				return fmt.Errorf("failed to marshal workflow %d: %v", wf.ID, err)
			}
			key := fmt.Sprintf("%s%d", workflowPrefix, wf.ID)
			pipe.Set(ctx, key, data, 0)
		}
		_, err := pipe.Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to execute pipeline for workflows: %v", err)
		}
		return nil
	})
}

// ClearCompleted removes instances with "completed" or "failed" state from Redis.
func (s *RedisStorage) ClearCompleted(ctx context.Context) error {
	return withContextError(ctx, func() error {
		keys, err := s.client.Keys(ctx, instancePrefix+"*").Result()
		if err != nil {
			return fmt.Errorf("failed to scan instance keys: %v", err)
		}

		if len(keys) == 0 {
			return nil
		}

		pipe := s.client.Pipeline()
		for _, key := range keys {
			data, err := s.client.Get(ctx, key).Bytes()
			if errors.Is(err, redis.Nil) {
				continue
			} else if err != nil {
				return fmt.Errorf("failed to get %s: %v", key, err)
			}

			var inst types.WorkflowInstance
			if err := json.Unmarshal(data, &inst); err != nil {
				return fmt.Errorf("failed to unmarshal %s: %v", key, err)
			}

			if inst.State == "completed" || inst.State == "failed" {
				pipe.Del(ctx, key)
			}
		}

		_, err = pipe.Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to execute pipeline for deletion: %v", err)
		}
		return nil
	})
}

// Close closes the Redis client connection.
func (s *RedisStorage) Close() error {
	return s.client.Close()
}
