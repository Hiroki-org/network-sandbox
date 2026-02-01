package main

import (
	"testing"
)

func BenchmarkSelectWorker(b *testing.B) {
	lb := NewLoadBalancer()
	// Add some workers
	for i := 0; i < 20; i++ {
		lb.AddWorker("w", "url", "c", 1, 100)
	}
	// Make some unhealthy or disabled to exercise the filter logic
	lb.workers[0].Healthy = false
	lb.workers[1].Enabled = false
	lb.workers[2].CircuitOpen = true
	lb.workers[10].Healthy = false
	lb.workers[11].Enabled = false
	lb.workers[12].CircuitOpen = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb.SelectWorker()
	}
}
