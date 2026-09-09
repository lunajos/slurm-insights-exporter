package slurm

import (
	"context"
	"sync"
	"time"
)

type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	at      time.Time
	snap    Snapshot
	collect func(context.Context) Snapshot
}

func NewCache(ttl time.Duration, collect func(context.Context) Snapshot) *Cache {
	return &Cache{ttl: ttl, collect: collect}
}
func (c *Cache) Get(ctx context.Context) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < c.ttl {
		return c.snap
	}
	c.snap = c.collect(ctx)
	c.at = time.Now()
	return c.snap
}
