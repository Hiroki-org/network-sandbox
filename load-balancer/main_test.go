package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestSelectWorker_RoundRobin(t *testing.T) {
	lb := NewLoadBalancer()
	lb.SetAlgorithm("round-robin")
	lb.AddWorker("w1", "http://w1", "", 1, 100)
	lb.AddWorker("w2", "http://w2", "", 1, 100)

	// roundRobin increments index before selection.
	// 0 -> 1. (1+0)%2 = 1. Returns w2.
	w1 := lb.SelectWorker()
	// 1 -> 2. (2+0)%2 = 0. Returns w1.
	w2 := lb.SelectWorker()
	// 2 -> 3. (3+0)%2 = 1. Returns w2.
	w3 := lb.SelectWorker()

	if w1 == nil || w2 == nil || w3 == nil {
		t.Fatal("SelectWorker returned nil")
	}

	if w1.Name != "w2" {
		t.Errorf("Expected w2, got %s", w1.Name)
	}
	if w2.Name != "w1" {
		t.Errorf("Expected w1, got %s", w2.Name)
	}
	if w3.Name != "w2" {
		t.Errorf("Expected w2, got %s", w3.Name)
	}
}

func TestConcurrentStats(t *testing.T) {
	lb := NewLoadBalancer()
	lb.AddWorker("w1", "http://w1", "", 1, 100)
	w := lb.workers[0]

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt64(&w.CurrentLoad, 1)
			atomic.AddInt64(&w.TotalRequests, 1)
		}()
	}
	wg.Wait()

	if val := atomic.LoadInt64(&w.CurrentLoad); val != 100 {
		t.Errorf("Expected Load 100, got %d", val)
	}
	if val := atomic.LoadInt64(&w.TotalRequests); val != 100 {
		t.Errorf("Expected Total 100, got %d", val)
	}
}

func TestSelectWorker_LeastConnections(t *testing.T) {
	lb := NewLoadBalancer()
	lb.SetAlgorithm("least-connections")
	lb.AddWorker("w1", "http://w1", "", 1, 100)
	lb.AddWorker("w2", "http://w2", "", 1, 100)
	lb.AddWorker("w3", "http://w3", "", 1, 100)

	atomic.StoreInt64(&lb.workers[0].CurrentLoad, 10)
	atomic.StoreInt64(&lb.workers[1].CurrentLoad, 2) // Lowest
	atomic.StoreInt64(&lb.workers[2].CurrentLoad, 20)

	w := lb.SelectWorker()
	if w == nil {
		t.Fatal("SelectWorker returned nil")
	}
	if w.Name != "w2" {
		t.Errorf("Expected w2 (load 2), got %s (load %d)", w.Name, w.CurrentLoad)
	}
}

func TestSelectWorker_NoAllocations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping alloc test in short mode")
	}
	lb := NewLoadBalancer()
	lb.AddWorker("w1", "http://w1", "", 1, 100)
	lb.AddWorker("w2", "http://w2", "", 1, 100)

	allocs := testing.AllocsPerRun(100, func() {
		lb.SelectWorker()
	})

	if allocs > 0 {
		t.Errorf("Expected 0 allocations per SelectWorker call, got %f", allocs)
	}
}
