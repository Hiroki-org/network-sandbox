package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

func BenchmarkSelectWorker(b *testing.B) {
	lb := NewLoadBalancer()
	// Add some workers
	for i := 0; i < 100; i++ {
		lb.AddWorker("worker", "http://localhost:8080", "#000000", 1, 100)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lb.SelectWorker()
		}
	})
}

func BenchmarkSelectWorker_WithContention(b *testing.B) {
	lb := NewLoadBalancer()
	for i := 0; i < 100; i++ {
		lb.AddWorker("worker", "http://localhost:8080", "#000000", 1, 100)
	}

	// Simulate background load updates using atomic operations (as per new implementation)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				// Simulate load update (like handleTask)
				// No lock needed for atomic update
				if len(lb.workers) > 0 {
					atomic.AddInt64(&lb.workers[0].CurrentLoad, 1)
				}
			}
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lb.SelectWorker()
		}
	})

	close(stop)
	wg.Wait()
}
