package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/VectorSigmaOmega/photon/internal/config"
)

const pruneDLQScript = `
local key = KEYS[1]
local cutoff = tonumber(ARGV[1])
local entries = redis.call("LRANGE", key, 0, -1)

if #entries == 0 then
  return 0
end

local kept = {}
local removed = 0

for _, raw in ipairs(entries) do
  local ok, entry = pcall(cjson.decode, raw)
  local failedAtUnix = nil
  if ok and type(entry) == "table" then
    failedAtUnix = tonumber(entry["failed_at_unix"])
  end

  if failedAtUnix ~= nil and failedAtUnix < cutoff then
    removed = removed + 1
  else
    table.insert(kept, raw)
  end
end

if removed == 0 then
  return 0
end

redis.call("DEL", key)
for _, raw in ipairs(kept) do
  redis.call("RPUSH", key, raw)
end

return removed
`

type RedisQueue struct {
	client   *redis.Client
	queueKey string
	dlqKey   string
}

type DLQEntry struct {
	Attempt       int       `json:"attempt"`
	FailedAt      time.Time `json:"failed_at"`
	FailedAtUnix  int64     `json:"failed_at_unix"`
	FailureReason string    `json:"failure_reason"`
	JobID         string    `json:"job_id"`
}

func NewRedisQueue(cfg config.RedisConfig) *RedisQueue {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	return &RedisQueue{
		client:   client,
		queueKey: cfg.QueueKey,
		dlqKey:   cfg.DLQKey,
	}
}

func (q *RedisQueue) Ping(ctx context.Context) error {
	return q.client.Ping(ctx).Err()
}

func (q *RedisQueue) Enqueue(ctx context.Context, jobID string) error {
	return q.client.RPush(ctx, q.queueKey, jobID).Err()
}

func (q *RedisQueue) Dequeue(ctx context.Context, timeout time.Duration) (string, bool, error) {
	result, err := q.client.BLPop(ctx, timeout, q.queueKey).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}

	if err != nil {
		return "", false, err
	}

	if len(result) != 2 {
		return "", false, nil
	}

	return result[1], true, nil
}

func (q *RedisQueue) EnqueueDLQ(ctx context.Context, entry DLQEntry) error {
	if entry.FailedAt.IsZero() {
		entry.FailedAt = time.Now().UTC()
	}
	if entry.FailedAtUnix == 0 {
		entry.FailedAtUnix = entry.FailedAt.Unix()
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	return q.client.RPush(ctx, q.dlqKey, payload).Err()
}

func (q *RedisQueue) QueueDepth(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, q.queueKey).Result()
}

func (q *RedisQueue) DLQDepth(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, q.dlqKey).Result()
}

func (q *RedisQueue) PruneDLQBefore(ctx context.Context, cutoff time.Time) (int, error) {
	return q.client.Eval(ctx, pruneDLQScript, []string{q.dlqKey}, cutoff.UTC().Unix()).Int()
}

func (q *RedisQueue) Close() error {
	return q.client.Close()
}

func (q *RedisQueue) QueueKey() string {
	return q.queueKey
}

func (q *RedisQueue) DLQKey() string {
	return q.dlqKey
}
