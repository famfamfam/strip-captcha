package redisstore

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// STRIPCAPTCHA_REDIS=localhost:6379 go test ./redisstore
func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	addr := os.Getenv("STRIPCAPTCHA_REDIS")
	if addr == "" {
		t.Skip("STRIPCAPTCHA_REDIS not set")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { client.Close() })
	prefix := "stripcaptcha-test:" + t.Name() + ":" + time.Now().Format("150405.000000") + ":"
	return New(client), prefix
}

func TestSaveTake(t *testing.T) {
	s, p := testStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, p+"a", "12345|1|", time.Minute); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.Take(ctx, p+"a")
	if err != nil || !ok || v != "12345|1|" {
		t.Fatalf("Take = %q, %v, %v", v, ok, err)
	}
	if _, ok, err := s.Take(ctx, p+"a"); ok || err != nil {
		t.Fatalf("second Take = %v, %v; want miss", ok, err)
	}
}

// Из многих параллельных Take одного ключа значение получает ровно один.
func TestTakeIsAtomic(t *testing.T) {
	s, p := testStore(t)
	ctx := context.Background()
	_ = s.Save(ctx, p+"k", "v", time.Minute)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok, _ := s.Take(ctx, p+"k"); ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d winners, want 1", wins.Load())
	}
}

func TestIncrWindow(t *testing.T) {
	s, p := testStore(t)
	ctx := context.Background()
	for want := int64(1); want <= 3; want++ {
		n, err := s.Incr(ctx, p+"c", 300*time.Millisecond)
		if err != nil || n != want {
			t.Fatalf("Incr = %d, %v; want %d", n, err, want)
		}
	}
	time.Sleep(400 * time.Millisecond)
	if n, _ := s.Incr(ctx, p+"c", time.Second); n != 1 {
		t.Fatalf("Incr after window = %d, want 1", n)
	}
}
