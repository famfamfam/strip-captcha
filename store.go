package stripcaptcha

import (
	"context"
	"sync"
	"time"
)

// Store хранит ответы выданных капч до проверки.
type Store interface {
	// Save сохраняет value под key на ttl.
	Save(ctx context.Context, key, value string, ttl time.Duration) error
	// Take атомарно читает и удаляет key: капча одноразовая, и два
	// параллельных запроса с одним id не должны пройти оба. ok=false —
	// ключа нет или он истёк.
	Take(ctx context.Context, key string) (value string, ok bool, err error)
}

// Counter — необязательное расширение Store для лимита выдач.
type Counter interface {
	// Incr увеличивает счётчик key и возвращает новое значение. Окно
	// фиксированное: отсчёт от первого инкремента, следующие его не продлевают.
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
}

// MemoryStore — хранилище в памяти процесса. Годится для одного экземпляра
// сервера; при нескольких экземплярах за балансировщиком капча, выданная
// одним, не пройдёт проверку на другом — берите redisstore.
type MemoryStore struct {
	mu        sync.Mutex
	items     map[string]memItem
	counters  map[string]memCounter
	lastSweep time.Time
	now       func() time.Time
}

type memItem struct {
	value   string
	expires time.Time
}

type memCounter struct {
	n       int64
	expires time.Time
}

// memorySweepEvery — как часто вычищать истёкшие записи. Чистим попутно при
// записи, без фоновой горутины, которую пришлось бы останавливать.
const memorySweepEvery = time.Minute

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		items:    make(map[string]memItem),
		counters: make(map[string]memCounter),
		now:      time.Now,
	}
}

func (m *MemoryStore) Save(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.sweepLocked(now)
	m.items[key] = memItem{value: value, expires: now.Add(ttl)}
	return nil
}

func (m *MemoryStore) Take(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[key]
	if !ok {
		return "", false, nil
	}
	delete(m.items, key)
	if !m.now().Before(it.expires) {
		return "", false, nil
	}
	return it.value, true, nil
}

func (m *MemoryStore) Incr(_ context.Context, key string, window time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.sweepLocked(now)
	c, ok := m.counters[key]
	if !ok || !now.Before(c.expires) {
		c = memCounter{expires: now.Add(window)}
	}
	c.n++
	m.counters[key] = c
	return c.n, nil
}

func (m *MemoryStore) sweepLocked(now time.Time) {
	if now.Sub(m.lastSweep) < memorySweepEvery {
		return
	}
	m.lastSweep = now
	for k, it := range m.items {
		if !now.Before(it.expires) {
			delete(m.items, k)
		}
	}
	for k, c := range m.counters {
		if !now.Before(c.expires) {
			delete(m.counters, k)
		}
	}
}
