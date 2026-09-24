// Package redisstore — хранилище stripcaptcha в Redis (6.2+, нужен GETDEL).
// Капча, выданная одним экземпляром сервера, проверяется на любом другом.
package redisstore

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// incrScript — INCR и PEXPIRE атомарно: при раздельных вызовах сбой между
// ними оставил бы счётчик без TTL, и клиент навсегда упёрся бы в лимит.
// TTL ставится только новому ключу, так что окно фиксированное.
var incrScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n
`)

type Store struct {
	client redis.UniversalClient
}

// New принимает *redis.Client, *redis.ClusterClient или *redis.Ring.
func New(client redis.UniversalClient) *Store {
	return &Store{client: client}
}

func (s *Store) Save(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *Store) Take(ctx context.Context, key string) (string, bool, error) {
	v, err := s.client.GetDel(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	return incrScript.Run(ctx, s.client, []string{key}, window.Milliseconds()).Int64()
}
