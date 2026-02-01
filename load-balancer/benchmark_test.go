package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func BenchmarkBroadcastStatus(b *testing.B) {
	lb := NewLoadBalancer()

	// Start a dummy server to accept WS connections
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader.Upgrade(w, r, nil)
	}))
	defer s.Close()

	// Create clients
	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	numClients := 100
	clients := make([]*websocket.Conn, numClients)

	for i := 0; i < numClients; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatalf("Failed to dial: %v", err)
		}
		clients[i] = c
		lb.wsClientsMu.Lock()
		lb.wsClients[c] = true
		lb.wsClientsMu.Unlock()
	}

	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb.BroadcastStatus()
	}
}

func BenchmarkBroadcastLockContention(b *testing.B) {
	lb := NewLoadBalancer()

	// Start a dummy server to accept WS connections
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader.Upgrade(w, r, nil)
	}))
	defer s.Close()

	// Create clients
	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	numClients := 100
	clients := make([]*websocket.Conn, numClients)

	for i := 0; i < numClients; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatalf("Failed to dial: %v", err)
		}
		clients[i] = c
		lb.wsClientsMu.Lock()
		lb.wsClients[c] = true
		lb.wsClientsMu.Unlock()
	}

	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// Run BroadcastStatus in background continuously
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				lb.BroadcastStatus()
			}
		}
	}()
	defer close(stop)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate a new client connecting
		// We just acquire and release the lock
		lb.wsClientsMu.Lock()
		// Do nothing
		lb.wsClientsMu.Unlock()

		// Small sleep to allow broadcast to run
		// time.Sleep(1 * time.Microsecond)
	}
}
