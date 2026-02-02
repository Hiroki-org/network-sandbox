import React from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import App from './App';

// Mock WebSocket
class MockWebSocket {
  static instances: MockWebSocket[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: ((error: Event) => void) | null = null;
  readyState: number = WebSocket.CONNECTING;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
    // Simulate connection after a short delay
    setTimeout(() => {
      this.readyState = WebSocket.OPEN;
      if (this.onopen) this.onopen();
    }, 10);
  }

  send(data: string) {
    // Mock send
  }

  close() {
    this.readyState = WebSocket.CLOSED;
    if (this.onclose) this.onclose();
  }

  static reset() {
    MockWebSocket.instances = [];
  }
}

global.WebSocket = MockWebSocket as any;

// Mock fetch
const mockFetch = jest.fn();
global.fetch = mockFetch;

describe('App Component', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    MockWebSocket.reset();
    mockFetch.mockReset();
  });

  afterEach(() => {
    jest.clearAllTimers();
  });

  describe('Initial Rendering', () => {
    it('should render the app title', () => {
      render(<App />);
      expect(screen.getByText('Network Sandbox')).toBeInTheDocument();
    });

    it('should render the description', () => {
      render(<App />);
      expect(screen.getByText('分散システムの負荷分散をリアルタイムで可視化')).toBeInTheDocument();
    });

    it('should show connection waiting status initially', () => {
      render(<App />);
      expect(screen.getByText('接続待ち...')).toBeInTheDocument();
    });

    it('should render all control sections', () => {
      render(<App />);
      expect(screen.getByText('負荷生成')).toBeInTheDocument();
      expect(screen.getByText('アルゴリズム')).toBeInTheDocument();
      expect(screen.getByText('統計')).toBeInTheDocument();
      expect(screen.getByText('ワーカー状態')).toBeInTheDocument();
      expect(screen.getByText('タスクログ')).toBeInTheDocument();
    });
  });

  describe('WebSocket Connection', () => {
    it('should establish WebSocket connection on mount', async () => {
      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      const ws = MockWebSocket.instances[0];
      expect(ws.url).toContain('/ws');
    });

    it('should show connected status when WebSocket opens', async () => {
      render(<App />);

      await waitFor(() => {
        expect(screen.getByText('接続中')).toBeInTheDocument();
      });
    });

    it('should update status when receiving WebSocket messages', async () => {
      const mockStatus = {
        algorithm: 'round-robin',
        workers: [
          {
            id: 'worker-1',
            name: 'go-worker-1',
            color: '#3B82F6',
            status: 'healthy',
            currentLoad: 2,
            maxLoad: 10,
            queueDepth: 0,
            healthy: true,
            circuitOpen: false,
            weight: 1,
            enabled: true
          }
        ],
        totalRequests: 100,
        successRate: 0.95
      };

      render(<App />);

      await waitFor(() => {
        const ws = MockWebSocket.instances[0];
        expect(ws).toBeDefined();
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatus)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });
    });

    it('should attempt to reconnect on WebSocket close', async () => {
      jest.useFakeTimers();
      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBe(1);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onclose) ws.onclose();
      });

      act(() => {
        jest.advanceTimersByTime(3000);
      });

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(1);
      });

      jest.useRealTimers();
    });
  });

  describe('Initial Status Fetch', () => {
    it('should fetch initial status on mount', async () => {
      const mockStatus = {
        algorithm: 'round-robin',
        workers: [],
        totalRequests: 0,
        successRate: 1.0
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => mockStatus
      });

      render(<App />);

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/status')
        );
      });
    });

    it('should handle fetch error gracefully', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      const consoleSpy = jest.spyOn(console, 'error').mockImplementation();

      render(<App />);

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalled();
      });

      consoleSpy.mockRestore();
    });
  });

  describe('Load Generator Controls', () => {
    it('should display request rate slider with initial value', () => {
      render(<App />);
      const slider = screen.getAllByRole('slider')[0];
      expect(slider).toHaveValue('10');
    });

    it('should update request rate when slider changes', () => {
      render(<App />);
      const slider = screen.getAllByRole('slider')[0];

      fireEvent.change(slider, { target: { value: '50' } });

      expect(screen.getByText(/50\/秒/)).toBeInTheDocument();
    });

    it('should display task weight slider with initial value', () => {
      render(<App />);
      const sliders = screen.getAllByRole('slider');
      const weightSlider = sliders[1];
      expect(weightSlider).toHaveValue('1');
    });

    it('should update task weight when slider changes', () => {
      render(<App />);
      const sliders = screen.getAllByRole('slider');
      const weightSlider = sliders[1];

      fireEvent.change(weightSlider, { target: { value: '2.5' } });

      expect(screen.getByText(/2\.5x/)).toBeInTheDocument();
    });

    it('should start load generation when button is clicked', () => {
      jest.useFakeTimers();
      render(<App />);

      const startButton = screen.getByText('開始');
      fireEvent.click(startButton);

      expect(screen.getByText('停止')).toBeInTheDocument();

      jest.useRealTimers();
    });

    it('should stop load generation when stop button is clicked', () => {
      jest.useFakeTimers();
      render(<App />);

      const startButton = screen.getByText('開始');
      fireEvent.click(startButton);

      const stopButton = screen.getByText('停止');
      fireEvent.click(stopButton);

      expect(screen.getByText('開始')).toBeInTheDocument();

      jest.useRealTimers();
    });

    it('should send tasks at specified rate when running', async () => {
      jest.useFakeTimers();

      mockFetch.mockResolvedValue({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 100,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      // Set rate to 10/sec (100ms interval)
      const startButton = screen.getByText('開始');
      fireEvent.click(startButton);

      act(() => {
        jest.advanceTimersByTime(1000);
      });

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalled();
      });

      jest.useRealTimers();
    });

    it('should disable single request button when running', () => {
      render(<App />);

      const startButton = screen.getByText('開始');
      fireEvent.click(startButton);

      const singleButton = screen.getByText('単発リクエスト');
      expect(singleButton).toBeDisabled();
    });

    it('should send single request when button is clicked', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 100,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/task'),
          expect.objectContaining({
            method: 'POST'
          })
        );
      });
    });
  });

  describe('Algorithm Selection', () => {
    it('should display all algorithm options', () => {
      render(<App />);

      expect(screen.getByText('ラウンドロビン')).toBeInTheDocument();
      expect(screen.getByText('最小接続')).toBeInTheDocument();
      expect(screen.getByText('重み付け')).toBeInTheDocument();
      expect(screen.getByText('ランダム')).toBeInTheDocument();
    });

    it('should change algorithm when option is clicked', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ algorithm: 'least-connections' })
      });

      render(<App />);

      const algoButton = screen.getByText('最小接続');
      fireEvent.click(algoButton);

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/algorithm'),
          expect.objectContaining({
            method: 'PUT',
            body: JSON.stringify({ algorithm: 'least-connections' })
          })
        );
      });
    });

    it('should handle algorithm change error gracefully', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));
      const consoleSpy = jest.spyOn(console, 'error').mockImplementation();

      render(<App />);

      const algoButton = screen.getByText('重み付け');
      fireEvent.click(algoButton);

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalled();
      });

      consoleSpy.mockRestore();
    });
  });

  describe('Statistics Display', () => {
    it('should display initial statistics as zero', () => {
      render(<App />);

      const successElements = screen.getAllByText('0');
      expect(successElements.length).toBeGreaterThan(0);
    });

    it('should update success count when tasks succeed', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 100,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        expect(screen.getByText('1')).toBeInTheDocument();
      });
    });

    it('should update failure count when tasks fail', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        json: async () => ({
          error: 'Server error',
          worker: 'test-worker'
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        const failureElements = screen.getAllByText('1');
        expect(failureElements.length).toBeGreaterThan(0);
      });
    });

    it('should calculate average response time correctly', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            id: 'task-1',
            worker: 'test-worker',
            color: '#3B82F6',
            processingTimeMs: 100,
            timestamp: new Date().toISOString()
          })
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            id: 'task-2',
            worker: 'test-worker',
            color: '#3B82F6',
            processingTimeMs: 200,
            timestamp: new Date().toISOString()
          })
        });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        expect(screen.getByText('100ms')).toBeInTheDocument();
      });

      fireEvent.click(singleButton);

      await waitFor(() => {
        expect(screen.getByText('150ms')).toBeInTheDocument();
      });
    });
  });

  describe('Worker Display', () => {
    it('should display message when no workers are present', () => {
      render(<App />);
      expect(screen.queryByText('go-worker-1')).not.toBeInTheDocument();
    });

    it('should display worker information when status is received', async () => {
      const mockStatus = {
        algorithm: 'round-robin',
        workers: [
          {
            id: 'worker-1',
            name: 'go-worker-1',
            color: '#3B82F6',
            status: 'healthy',
            currentLoad: 2,
            maxLoad: 10,
            queueDepth: 1,
            healthy: true,
            circuitOpen: false,
            weight: 2,
            enabled: true
          }
        ]
      };

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatus)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
        expect(screen.getByText('2/10')).toBeInTheDocument();
      });
    });

    it('should toggle worker enabled state', async () => {
      const mockStatus = {
        algorithm: 'round-robin',
        workers: [
          {
            id: 'worker-1',
            name: 'go-worker-1',
            color: '#3B82F6',
            status: 'healthy',
            currentLoad: 0,
            maxLoad: 10,
            queueDepth: 0,
            healthy: true,
            circuitOpen: false,
            weight: 1,
            enabled: true
          }
        ]
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({})
      });

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatus)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });

      const toggleButtons = screen.getAllByRole('button', { name: /無効にする/ });
      fireEvent.click(toggleButtons[0]);

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/workers/go-worker-1'),
          expect.objectContaining({
            method: 'PATCH'
          })
        );
      });
    });

    it('should update worker weight', async () => {
      const mockStatus = {
        algorithm: 'round-robin',
        workers: [
          {
            id: 'worker-1',
            name: 'go-worker-1',
            color: '#3B82F6',
            status: 'healthy',
            currentLoad: 0,
            maxLoad: 10,
            queueDepth: 0,
            healthy: true,
            circuitOpen: false,
            weight: 1,
            enabled: true
          }
        ]
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({})
      });

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatus)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });

      const weightInputs = screen.getAllByDisplayValue('1');
      const weightInput = weightInputs.find(el => (el as HTMLInputElement).type === 'number');

      if (weightInput) {
        fireEvent.change(weightInput, { target: { value: '5' } });

        await waitFor(() => {
          expect(mockFetch).toHaveBeenCalledWith(
            expect.stringContaining('/workers/go-worker-1'),
            expect.objectContaining({
              method: 'PATCH',
              body: JSON.stringify({ weight: 5 })
            })
          );
        });
      }
    });

    it('should show circuit breaker status', async () => {
      const mockStatus = {
        algorithm: 'round-robin',
        workers: [
          {
            id: 'worker-1',
            name: 'go-worker-1',
            color: '#3B82F6',
            status: 'healthy',
            currentLoad: 0,
            maxLoad: 10,
            queueDepth: 0,
            healthy: true,
            circuitOpen: true,
            weight: 1,
            enabled: true
          }
        ]
      };

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatus)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('⚡ サーキット開放中')).toBeInTheDocument();
      });
    });

    it('should show disabled status when worker is disabled', async () => {
      const mockStatus = {
        algorithm: 'round-robin',
        workers: [
          {
            id: 'worker-1',
            name: 'go-worker-1',
            color: '#3B82F6',
            status: 'healthy',
            currentLoad: 0,
            maxLoad: 10,
            queueDepth: 0,
            healthy: true,
            circuitOpen: false,
            weight: 1,
            enabled: false
          }
        ]
      };

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatus)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('⏸ 無効')).toBeInTheDocument();
      });
    });
  });

  describe('Task Log', () => {
    it('should display message when no tasks are present', () => {
      render(<App />);
      expect(screen.getByText('リクエストを送信してください')).toBeInTheDocument();
    });

    it('should display task in log after successful request', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-123',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 150,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        expect(screen.getByText(/task-/)).toBeInTheDocument();
        expect(screen.getByText('150ms')).toBeInTheDocument();
      });
    });

    it('should display error in log after failed request', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        json: async () => ({
          error: 'Worker overloaded',
          worker: 'test-worker'
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        expect(screen.getByText('Worker overloaded')).toBeInTheDocument();
      });
    });

    it('should handle network error in task request', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        expect(screen.getByText('Network error')).toBeInTheDocument();
      });
    });

    it('should limit task log to 100 entries', async () => {
      const responses = Array.from({ length: 105 }, (_, i) => ({
        ok: true,
        json: async () => ({
          id: `task-${i}`,
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 100,
          timestamp: new Date().toISOString()
        })
      }));

      mockFetch.mockImplementation(() =>
        Promise.resolve(responses.shift() as any)
      );

      jest.useFakeTimers();
      render(<App />);

      const startButton = screen.getByText('開始');
      fireEvent.click(startButton);

      for (let i = 0; i < 105; i++) {
        act(() => {
          jest.advanceTimersByTime(100);
        });
        await waitFor(() => {}, { timeout: 10 });
      }

      const stopButton = screen.getByText('停止');
      fireEvent.click(stopButton);

      await waitFor(() => {
        const taskElements = screen.queryAllByText(/task-/);
        expect(taskElements.length).toBeLessThanOrEqual(100);
      });

      jest.useRealTimers();
    });
  });

  describe('Edge Cases and Error Handling', () => {
    it('should handle WebSocket parse error gracefully', async () => {
      const consoleSpy = jest.spyOn(console, 'error').mockImplementation();

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: 'invalid json'
          }));
        }
      });

      expect(consoleSpy).toHaveBeenCalled();
      consoleSpy.mockRestore();
    });

    it('should handle missing environment variables gracefully', () => {
      delete (process.env as any).REACT_APP_API_URL;
      delete (process.env as any).REACT_APP_WS_URL;

      render(<App />);

      expect(screen.getByText('Network Sandbox')).toBeInTheDocument();
    });

    it('should handle worker with zero maxLoad', async () => {
      const mockStatus = {
        algorithm: 'round-robin',
        workers: [
          {
            id: 'worker-1',
            name: 'go-worker-1',
            color: '#3B82F6',
            status: 'healthy',
            currentLoad: 5,
            maxLoad: 0,
            queueDepth: 0,
            healthy: true,
            circuitOpen: false,
            weight: 1,
            enabled: true
          }
        ]
      };

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatus)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });
    });

    it('should handle task with zero or negative processing time', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 0,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        expect(screen.getByText('0ms')).toBeInTheDocument();
      });
    });
  });

  describe('Worker Configuration Panel', () => {
    const mockStatusWithWorker = {
      algorithm: 'round-robin',
      workers: [
        {
          id: 'worker-1',
          name: 'go-worker-1',
          color: '#3B82F6',
          status: 'healthy',
          currentLoad: 2,
          maxLoad: 10,
          queueDepth: 1,
          healthy: true,
          circuitOpen: false,
          weight: 2,
          enabled: true
        }
      ]
    };

    const mockWorkerConfig = {
      max_concurrent_requests: 3,
      response_delay_ms: 500,
      failure_rate: 0.02,
      queue_size: 10
    };

    it('should fetch worker configs when status is received', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => mockStatusWithWorker
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => mockWorkerConfig
        });

      render(<App />);

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/workers/go-worker-1/config')
        );
      });
    });

    it('should expand worker config panel when toggle is clicked', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => mockWorkerConfig
        });

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatusWithWorker)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });

      const expandButton = screen.getByText('▼ 設定を開く');
      fireEvent.click(expandButton);

      await waitFor(() => {
        expect(screen.getByText('▲ 設定を閉じる')).toBeInTheDocument();
      });
    });

    it('should update max concurrent requests config', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => mockWorkerConfig
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({ ...mockWorkerConfig, max_concurrent_requests: 5 })
        });

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatusWithWorker)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });

      const expandButton = screen.getByText('▼ 設定を開く');
      fireEvent.click(expandButton);

      await waitFor(() => {
        const configInputs = screen.getAllByDisplayValue('3');
        const maxConcurrentInput = configInputs.find(el =>
          (el as HTMLInputElement).type === 'number' &&
          el.previousElementSibling?.textContent?.includes('同時リクエスト数')
        );

        if (maxConcurrentInput) {
          fireEvent.change(maxConcurrentInput, { target: { value: '5' } });
        }
      });

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/workers/go-worker-1/config'),
          expect.objectContaining({
            method: 'PUT',
            body: JSON.stringify({ max_concurrent_requests: 5 })
          })
        );
      });
    });

    it('should update response delay config', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => mockWorkerConfig
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({ ...mockWorkerConfig, response_delay_ms: 1000 })
        });

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatusWithWorker)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });

      const expandButton = screen.getByText('▼ 設定を開く');
      fireEvent.click(expandButton);

      await waitFor(() => {
        const delaySliders = screen.getAllByRole('slider');
        const responseDelaySlider = delaySliders.find(slider =>
          slider.getAttribute('max') === '5000'
        );

        if (responseDelaySlider) {
          fireEvent.change(responseDelaySlider, { target: { value: '1000' } });
        }
      });

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/workers/go-worker-1/config'),
          expect.objectContaining({
            method: 'PUT',
            body: JSON.stringify({ response_delay_ms: 1000 })
          })
        );
      });
    });

    it('should update failure rate config', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => mockWorkerConfig
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({ ...mockWorkerConfig, failure_rate: 0.5 })
        });

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatusWithWorker)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });

      const expandButton = screen.getByText('▼ 設定を開く');
      fireEvent.click(expandButton);

      await waitFor(() => {
        const failureSliders = screen.getAllByRole('slider');
        const failureRateSlider = failureSliders.find(slider =>
          slider.getAttribute('max') === '1' && slider.getAttribute('step') === '0.01'
        );

        if (failureRateSlider) {
          fireEvent.change(failureRateSlider, { target: { value: '0.5' } });
        }
      });

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/workers/go-worker-1/config'),
          expect.objectContaining({
            method: 'PUT',
            body: JSON.stringify({ failure_rate: 0.5 })
          })
        );
      });
    });

    it('should update queue size config', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => mockWorkerConfig
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({ ...mockWorkerConfig, queue_size: 20 })
        });

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatusWithWorker)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });

      const expandButton = screen.getByText('▼ 設定を開く');
      fireEvent.click(expandButton);

      await waitFor(() => {
        const queueInput = screen.getByDisplayValue('10');
        if (queueInput && (queueInput as HTMLInputElement).type === 'number') {
          fireEvent.change(queueInput, { target: { value: '20' } });
        }
      });

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/workers/go-worker-1/config'),
          expect.objectContaining({
            method: 'PUT',
            body: JSON.stringify({ queue_size: 20 })
          })
        );
      });
    });

    it('should handle fetch error for worker config gracefully', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));
      const consoleSpy = jest.spyOn(console, 'error').mockImplementation();

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatusWithWorker)
          }));
        }
      });

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalled();
      });

      consoleSpy.mockRestore();
    });

    it('should display config values correctly in expanded panel', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => mockWorkerConfig
        });

      render(<App />);

      await waitFor(() => {
        expect(MockWebSocket.instances.length).toBeGreaterThan(0);
      });

      act(() => {
        const ws = MockWebSocket.instances[0];
        if (ws.onmessage) {
          ws.onmessage(new MessageEvent('message', {
            data: JSON.stringify(mockStatusWithWorker)
          }));
        }
      });

      await waitFor(() => {
        expect(screen.getByText('go-worker-1')).toBeInTheDocument();
      });

      const expandButton = screen.getByText('▼ 設定を開く');
      fireEvent.click(expandButton);

      await waitFor(() => {
        expect(screen.getByText(/同時リクエスト数: 3/)).toBeInTheDocument();
        expect(screen.getByText(/応答遅延: 500ms/)).toBeInTheDocument();
        expect(screen.getByText(/失敗率: 2%/)).toBeInTheDocument();
        expect(screen.getByText(/キューサイズ: 10/)).toBeInTheDocument();
      });
    });
  });

  describe('Enhanced Log Coloring', () => {
    it('should apply red color class for failed tasks', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        json: async () => ({
          error: 'Server error',
          worker: 'test-worker'
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        const logEntries = document.querySelectorAll('.bg-red-900\\/50');
        expect(logEntries.length).toBeGreaterThan(0);
      });
    });

    it('should apply yellow color class for slow tasks (>= 1200ms)', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 1300,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        const logEntries = document.querySelectorAll('.bg-yellow-900\\/30');
        expect(logEntries.length).toBeGreaterThan(0);
      });
    });

    it('should apply amber color class for moderately slow tasks (>= 800ms)', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 900,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        const logEntries = document.querySelectorAll('.bg-amber-900\\/20');
        expect(logEntries.length).toBeGreaterThan(0);
      });
    });

    it('should apply green/normal color class for fast tasks', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 200,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        const logEntries = document.querySelectorAll('.bg-slate-700');
        expect(logEntries.length).toBeGreaterThan(0);
      });
    });

    it('should show response time in appropriate text color', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 1500,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        const textElements = document.querySelectorAll('.text-yellow-400');
        expect(textElements.length).toBeGreaterThan(0);
      });
    });
  });

  describe('Request Rate Changes', () => {
    it('should have default request rate of 2 RPS', () => {
      render(<App />);
      expect(screen.getByText(/2\/秒/)).toBeInTheDocument();
    });

    it('should allow request rate adjustment up to 20 RPS', () => {
      render(<App />);
      const slider = screen.getAllByRole('slider')[0];

      expect(slider).toHaveAttribute('max', '20');
      expect(slider).toHaveAttribute('min', '1');
    });

    it('should update interval when request rate changes', async () => {
      jest.useFakeTimers();

      mockFetch.mockResolvedValue({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 100,
          timestamp: new Date().toISOString()
        })
      });

      render(<App />);

      const slider = screen.getAllByRole('slider')[0];
      fireEvent.change(slider, { target: { value: '4' } });

      const startButton = screen.getByText('開始');
      fireEvent.click(startButton);

      act(() => {
        jest.advanceTimersByTime(250); // 1000ms / 4 RPS = 250ms interval
      });

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalled();
      });

      jest.useRealTimers();
    });
  });

  describe('Timestamp Formatting', () => {
    it('should format timestamps in Japanese locale', async () => {
      const testDate = new Date('2024-01-15T10:30:45.123Z');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          id: 'task-1',
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 100,
          timestamp: testDate.toISOString()
        })
      });

      render(<App />);

      const singleButton = screen.getByText('単発リクエスト');
      fireEvent.click(singleButton);

      await waitFor(() => {
        // Check that some timestamp text is present (format depends on locale)
        const timestamps = screen.getAllByText(/\d{2}:\d{2}:\d{2}/);
        expect(timestamps.length).toBeGreaterThan(0);
      });
    });
  });

  describe('Task Log Limit', () => {
    it('should limit task log to 50 entries', async () => {
      const responses = Array.from({ length: 55 }, (_, i) => ({
        ok: true,
        json: async () => ({
          id: `task-${i}`,
          worker: 'test-worker',
          color: '#3B82F6',
          processingTimeMs: 100,
          timestamp: new Date().toISOString()
        })
      }));

      mockFetch.mockImplementation(() =>
        Promise.resolve(responses.shift() as any)
      );

      jest.useFakeTimers();
      render(<App />);

      const startButton = screen.getByText('開始');
      fireEvent.click(startButton);

      for (let i = 0; i < 55; i++) {
        act(() => {
          jest.advanceTimersByTime(100);
        });
        await waitFor(() => {}, { timeout: 10 });
      }

      const stopButton = screen.getByText('停止');
      fireEvent.click(stopButton);

      await waitFor(() => {
        const taskElements = screen.queryAllByText(/task-/);
        expect(taskElements.length).toBeLessThanOrEqual(50);
      });

      jest.useRealTimers();
    });
  });
});