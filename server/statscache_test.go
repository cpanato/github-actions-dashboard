// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errNone error

func TestCachedStatsSharesConcurrentLoads(t *testing.T) {
	c := newStatsCache(context.Background(), time.Minute, 10)
	var calls atomic.Int32
	release := make(chan struct{})
	load := func(context.Context) (int, error) {
		calls.Add(1)
		<-release
		return 42, errNone
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := cachedStats(context.Background(), c, "k", load)
			if err != nil || got != 42 {
				t.Errorf("cachedStats() = %d, %v", got, err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if _, err := cachedStats(context.Background(), c, "k", load); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("loader called %d times, want 1", n)
	}
}

func TestCachedStatsServesStaleWhileRefreshing(t *testing.T) {
	c := newStatsCache(context.Background(), 20*time.Millisecond, 10)
	var n atomic.Int32
	load := func(context.Context) (int, error) { return int(n.Add(1)), nil }

	if got, _ := cachedStats(context.Background(), c, "k", load); got != 1 {
		t.Fatalf("first load = %d, want 1", got)
	}
	time.Sleep(40 * time.Millisecond)
	if got, _ := cachedStats(context.Background(), c, "k", load); got != 1 {
		t.Fatalf("stale read = %d, want stale value 1", got)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got, _ := cachedStats(context.Background(), c, "k", load); got == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cache was not refreshed in the background")
}

func TestCachedStatsDoesNotCacheErrorsAndHonorsRequestCancel(t *testing.T) {
	c := newStatsCache(context.Background(), time.Minute, 10)
	var calls atomic.Int32
	fail := func(context.Context) (int, error) {
		calls.Add(1)
		return 0, errors.New("boom")
	}
	for i := 0; i < 2; i++ {
		if _, err := cachedStats(context.Background(), c, "k", fail); err == nil {
			t.Fatal("expected error")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("failed loads were cached: %d calls", calls.Load())
	}

	block := make(chan struct{})
	defer close(block)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := cachedStats(ctx, c, "slow", func(context.Context) (int, error) { <-block; return 1, nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestCachedStatsDisabled(t *testing.T) {
	var calls int
	load := func(context.Context) (int, error) { calls++; return calls, nil }
	for _, c := range []*statsCache{nil, newStatsCache(context.Background(), 0, 10)} {
		calls = 0
		for i := 0; i < 2; i++ {
			if _, err := cachedStats(context.Background(), c, "k", load); err != nil {
				t.Fatal(err)
			}
		}
		if calls != 2 {
			t.Fatalf("disabled cache made %d loads, want 2", calls)
		}
	}
}

func TestStatsCacheBoundsEntries(t *testing.T) {
	c := newStatsCache(context.Background(), time.Minute, 2)
	for _, key := range []string{"a", "b", "c"} {
		if _, err := cachedStats(context.Background(), c, key, func(context.Context) (int, error) { return 1, nil }); err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > 2 {
		t.Fatalf("cache has %d entries, want at most 2", len(c.entries))
	}
}
