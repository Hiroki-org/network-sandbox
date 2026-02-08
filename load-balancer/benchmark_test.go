package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func BenchmarkBroadcastStatus(b *testing.B) {
	// Setup LoadBalancer
	bl := NewLoadBalancer()
	// Add some dummy status to broadcast
	bl.AddWorker("bench-worker", "http://localhost:8080", "#000000", 1, 10)

	// Setup WebSocket server
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		bl.wsClientsMu.Lock()
		bl.wsClients[conn] = &sync.Mutex{}
		bl.wsClientsMu.Unlock()

		// Keep connection open and read
		go func() {
			defer func() {
				bl.wsClientsMu.Lock()
				delete(bl.wsClients, conn)
				bl.wsClientsMu.Unlock()
				conn.Close()
			}()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}))
	defer s.Close()

	// Connect N clients
	clientCount := 100
	clients := make([]*websocket.Conn, clientCount)
	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")

	var wg sync.WaitGroup
	// Dialing concurrently
	for i := 0; i < clientCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				b.Logf("Failed to dial: %v", err)
				return
			}
			clients[idx] = c
			// Read loop to consume messages
			go func() {
				defer c.Close()
				for {
					_, _, err := c.ReadMessage()
					if err != nil {
						return
					}
				}
			}()
		}(i)
	}
	wg.Wait()

	// Wait for clients to be registered
	// Simple polling
	for {
		bl.wsClientsMu.Lock()
		count := len(bl.wsClients)
		bl.wsClientsMu.Unlock()
		if count >= clientCount {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bl.BroadcastStatus()
	}
    b.StopTimer()
	bl.wsClientsMu.Lock()
	finalCount := len(bl.wsClients)
	bl.wsClientsMu.Unlock()
	b.Logf("Final client count: %d", finalCount)

	// Cleanup
	for _, c := range clients {
		if c != nil {
			c.Close()
		}
	}
}
