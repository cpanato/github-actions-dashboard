// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"context"
	"sync"
	"time"
)

// defaultStatsQueryTimeout bounds each aggregate query so a slow one cannot
// hold database connections indefinitely.
const defaultStatsQueryTimeout = 2 * time.Minute

type statsEntry struct {
	value   any
	expires time.Time
}

type statsFlight struct {
	done  chan struct{}
	value any
	err   error
}

// statsCache caches expensive aggregate query results. Concurrent callers for
// the same key share one query, the query runs independently of any single
// request, and expired values are served while a refresh runs in the background.
type statsCache struct {
	base       context.Context
	ttl        time.Duration
	maxEntries int
	timeout    time.Duration

	mu       sync.Mutex
	entries  map[string]statsEntry
	inflight map[string]*statsFlight
}

func newStatsCache(base context.Context, ttl time.Duration, maxEntries int) *statsCache {
	return &statsCache{
		base:       base,
		ttl:        ttl,
		maxEntries: maxEntries,
		timeout:    defaultStatsQueryTimeout,
		entries:    map[string]statsEntry{},
		inflight:   map[string]*statsFlight{},
	}
}

// cachedStats returns the cached value for key, loading it when missing. A nil
// cache or non-positive TTL disables caching.
func cachedStats[T any](ctx context.Context, c *statsCache, key string, load func(context.Context) (T, error)) (T, error) {
	if c == nil || c.ttl <= 0 {
		return load(ctx)
	}
	value, err := c.get(ctx, key, func(ctx context.Context) (any, error) { return load(ctx) })
	if err != nil {
		var zero T
		return zero, err
	}
	return value.(T), nil
}

func (c *statsCache) get(ctx context.Context, key string, load func(context.Context) (any, error)) (any, error) {
	c.mu.Lock()
	entry, ok := c.entries[key]
	if ok {
		if time.Now().After(entry.expires) {
			c.startLoad(key, load)
		}
		c.mu.Unlock()
		return entry.value, nil
	}
	flight := c.startLoad(key, load)
	c.mu.Unlock()

	select {
	case <-flight.done:
		return flight.value, flight.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// startLoad must be called with c.mu held.
func (c *statsCache) startLoad(key string, load func(context.Context) (any, error)) *statsFlight {
	if flight, ok := c.inflight[key]; ok {
		return flight
	}
	flight := &statsFlight{done: make(chan struct{})}
	c.inflight[key] = flight

	go func() {
		ctx, cancel := context.WithTimeout(c.base, c.timeout)
		defer cancel()
		value, err := load(ctx)

		c.mu.Lock()
		if err == nil {
			c.store(key, value)
		}
		delete(c.inflight, key)
		c.mu.Unlock()

		flight.value, flight.err = value, err
		close(flight.done)
	}()
	return flight
}

// store must be called with c.mu held.
func (c *statsCache) store(key string, value any) {
	if _, exists := c.entries[key]; !exists && c.maxEntries > 0 && len(c.entries) >= c.maxEntries {
		// Cache keys come from request parameters, so bound the cache size.
		c.entries = map[string]statsEntry{}
	}
	c.entries[key] = statsEntry{value: value, expires: time.Now().Add(c.ttl)}
}
