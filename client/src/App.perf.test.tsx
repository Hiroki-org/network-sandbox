import React from 'react';
import { render, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import App from './App';

// Mock WebSocket (Simplified from App.test.tsx)
class MockWebSocket {
  static instances: MockWebSocket[] = [];
  url: string;
  onmessage: ((event: MessageEvent) => void) | null = null;
  readyState: number = WebSocket.CONNECTING;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
    setTimeout(() => {
      this.readyState = WebSocket.OPEN;
    }, 10);
  }
  send() {}
  close() {}
  static reset() { MockWebSocket.instances = []; }
}
const mockFetch = jest.fn();

describe('App Performance', () => {
  const originalWebSocket = global.WebSocket;
  const originalFetch = global.fetch;

  beforeEach(() => {
    jest.clearAllMocks();
    MockWebSocket.reset();
    global.WebSocket = MockWebSocket as any;
    global.fetch = mockFetch;
    mockFetch.mockReset();
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
    global.WebSocket = originalWebSocket;
    global.fetch = originalFetch;
  });

  it('should fetch worker configs in parallel', async () => {
    // Setup fetch to take a long time (1000ms)
    // This ensures that if sequential, the second fetch won't start until we advance time significantly.
    mockFetch.mockImplementation(() => new Promise(resolve => setTimeout(() => resolve({
        ok: true,
        json: async () => ({})
    }), 1000)));

    render(<App />);

    // Wait for WS connection
    act(() => { jest.advanceTimersByTime(20); });

    const ws = MockWebSocket.instances[0];
    const workers = [
        { name: 'w1', id: '1', url: '', color: '#3b82f6', weight: 1, maxLoad: 5, healthy: true, currentLoad: 0, enabled: true, totalRequests: 0, failedRequests: 0, circuitOpen: false },
        { name: 'w2', id: '2', url: '', color: '#22c55e', weight: 1, maxLoad: 5, healthy: true, currentLoad: 0, enabled: true, totalRequests: 0, failedRequests: 0, circuitOpen: false },
        { name: 'w3', id: '3', url: '', color: '#eab308', weight: 1, maxLoad: 5, healthy: true, currentLoad: 0, enabled: true, totalRequests: 0, failedRequests: 0, circuitOpen: false }
    ];

    // Trigger update
    await act(async () => {
        if (ws && ws.onmessage) {
            ws.onmessage(new MessageEvent('message', {
                data: JSON.stringify({ workers })
            }));
        }
    });

    // Advance time slightly to let the effect run and fetches start.
    // But NOT enough for the first sequential fetch to finish (it needs 1000ms).
    // We advance 100ms.
    act(() => { jest.advanceTimersByTime(100); });

    // In sequential:
    // fetch(w1) called. returns promise pending. loop awaits.
    // T=100ms. Promise still pending (needs 900ms more).
    // fetch(w2) NOT called yet.
    // Call count = 2 (1 worker + 1 initial).

    // In parallel:
    // fetch(w1), fetch(w2), fetch(w3) all called immediately.
    // Call count = 4 (3 workers + 1 initial).

    // We expect 4 calls (3 parallel + 1 initial status fetch).
    expect(mockFetch).toHaveBeenCalledTimes(4);
  });
});
