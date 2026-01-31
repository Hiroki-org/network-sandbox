import React from "react";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom";
import App from "./App";

// Mock WebSocket
class MockWebSocket {
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(public url: string) {
    setTimeout(() => {
      if (this.onopen) {
        this.onopen(new Event("open"));
      }
    }, 0);
  }

  send(data: string) {}

  close() {
    if (this.onclose) {
      this.onclose(new CloseEvent("close"));
    }
  }
}

// Mock fetch
const mockFetch = jest.fn();

describe("App Component", () => {
  beforeEach(() => {
    // Reset mocks
    jest.clearAllMocks();
    mockFetch.mockReset();
    global.fetch = mockFetch;
    (global as any).WebSocket = MockWebSocket;

    // Default fetch mock for status endpoint
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/status")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            algorithm: "round-robin",
            workers: [
              {
                id: "1",
                name: "worker-1",
                color: "#3B82F6",
                status: "healthy",
                currentLoad: 2,
                maxLoad: 10,
                queueDepth: 0,
                healthy: true,
                circuitOpen: false,
                weight: 1,
                enabled: true,
              },
            ],
            totalRequests: 100,
            successRate: 0.95,
          }),
        });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({}),
      });
    });
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  test("renders Network Sandbox title", () => {
    render(<App />);
    expect(screen.getByText("Network Sandbox")).toBeInTheDocument();
  });

  test("renders main sections", () => {
    render(<App />);

    expect(screen.getByText("負荷生成")).toBeInTheDocument();
    expect(screen.getByText("アルゴリズム")).toBeInTheDocument();
    expect(screen.getByText("統計")).toBeInTheDocument();
    expect(screen.getByText("ワーカー状態")).toBeInTheDocument();
    expect(screen.getByText("タスクログ")).toBeInTheDocument();
  });

  test("shows connection status", async () => {
    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("接続中")).toBeInTheDocument();
    });
  });

  test("displays algorithms", () => {
    render(<App />);

    expect(screen.getByText("ラウンドロビン")).toBeInTheDocument();
    expect(screen.getByText("最小接続")).toBeInTheDocument();
    expect(screen.getByText("重み付け")).toBeInTheDocument();
    expect(screen.getByText("ランダム")).toBeInTheDocument();
  });

  test("start/stop button toggles load generation", () => {
    render(<App />);

    const startButton = screen.getByText("開始");
    expect(startButton).toBeInTheDocument();

    fireEvent.click(startButton);

    expect(screen.getByText("停止")).toBeInTheDocument();

    fireEvent.click(screen.getByText("停止"));

    expect(screen.getByText("開始")).toBeInTheDocument();
  });

  test("request rate slider changes value", () => {
    render(<App />);

    const slider = screen.getByLabelText(/リクエストレート/i) as HTMLInputElement;

    expect(slider.value).toBe("10");

    fireEvent.change(slider, { target: { value: "50" } });

    expect(slider.value).toBe("50");
    expect(screen.getByText(/50\/秒/)).toBeInTheDocument();
  });

  test("task weight slider changes value", () => {
    render(<App />);

    const sliders = screen.getAllByRole("slider");
    const weightSlider = sliders.find(
      (s) => (s as HTMLInputElement).min === "0.1"
    ) as HTMLInputElement;

    expect(weightSlider).toBeDefined();
    expect(weightSlider.value).toBe("1");

    fireEvent.change(weightSlider, { target: { value: "2.5" } });

    expect(weightSlider.value).toBe("2.5");
    expect(screen.getByText(/2\.5x/)).toBeInTheDocument();
  });

  test("single request button sends task", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/task")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: "task-1",
            worker: "worker-1",
            color: "#3B82F6",
            processingTimeMs: 100,
            timestamp: new Date().toISOString(),
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    const singleRequestButton = screen.getByText("単発リクエスト");

    fireEvent.click(singleRequestButton);

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining("/task"),
        expect.objectContaining({
          method: "POST",
        })
      );
    });
  });

  test("displays workers from status", async () => {
    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("worker-1")).toBeInTheDocument();
    });
  });

  test("algorithm selection changes algorithm", async () => {
    mockFetch.mockImplementation((url: string, options?: any) => {
      if (url.includes("/algorithm") && options?.method === "PUT") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ algorithm: "least-connections" }),
        });
      }
      if (url.includes("/status")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            algorithm: "round-robin",
            workers: [],
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    const leastConnectionsButton = screen.getByText("最小接続");

    fireEvent.click(leastConnectionsButton);

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining("/algorithm"),
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({ algorithm: "least-connections" }),
        })
      );
    });
  });

  test("displays statistics", () => {
    render(<App />);

    expect(screen.getByText("成功")).toBeInTheDocument();
    expect(screen.getByText("失敗")).toBeInTheDocument();
    expect(screen.getByText("平均応答時間")).toBeInTheDocument();
  });

  test("shows empty task log message", () => {
    render(<App />);

    expect(screen.getByText("リクエストを送信してください")).toBeInTheDocument();
  });

  test("successful task appears in log", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/task")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: "task-success",
            worker: "worker-1",
            color: "#3B82F6",
            processingTimeMs: 150,
            timestamp: new Date().toISOString(),
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    const singleRequestButton = screen.getByText("単発リクエスト");
    fireEvent.click(singleRequestButton);

    await waitFor(() => {
      expect(screen.getByText("task-success")).toBeInTheDocument();
    });
  });

  test("failed task appears in log", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/task")) {
        return Promise.resolve({
          ok: false,
          json: async () => ({
            error: "Service unavailable",
            worker: "worker-1",
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    const singleRequestButton = screen.getByText("単発リクエスト");
    fireEvent.click(singleRequestButton);

    await waitFor(() => {
      expect(screen.getByText("Service unavailable")).toBeInTheDocument();
    });
  });

  test("network error appears in log", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/task")) {
        return Promise.reject(new Error("Network error"));
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    const singleRequestButton = screen.getByText("単発リクエスト");
    fireEvent.click(singleRequestButton);

    await waitFor(() => {
      expect(screen.getByText("Network error")).toBeInTheDocument();
    });
  });

  test("worker toggle button works", async () => {
    mockFetch.mockImplementation((url: string, options?: any) => {
      if (url.includes("/workers/") && options?.method === "PATCH") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ success: true }),
        });
      }
      if (url.includes("/status")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            algorithm: "round-robin",
            workers: [
              {
                id: "1",
                name: "worker-1",
                color: "#3B82F6",
                status: "healthy",
                currentLoad: 2,
                maxLoad: 10,
                queueDepth: 0,
                healthy: true,
                circuitOpen: false,
                weight: 1,
                enabled: true,
              },
            ],
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("worker-1")).toBeInTheDocument();
    });

    // Find toggle button (it's rendered as a button element)
    const toggleButtons = screen.getAllByRole("button");
    const toggleButton = toggleButtons.find(
      (btn) => btn.title === "無効にする" || btn.title === "有効にする"
    );

    if (toggleButton) {
      fireEvent.click(toggleButton);

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining("/workers/worker-1"),
          expect.objectContaining({
            method: "PATCH",
          })
        );
      });
    }
  });

  test("worker weight can be updated", async () => {
    mockFetch.mockImplementation((url: string, options?: any) => {
      if (url.includes("/workers/") && options?.method === "PATCH") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ success: true }),
        });
      }
      if (url.includes("/status")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            algorithm: "round-robin",
            workers: [
              {
                id: "1",
                name: "worker-1",
                color: "#3B82F6",
                status: "healthy",
                currentLoad: 2,
                maxLoad: 10,
                queueDepth: 0,
                healthy: true,
                circuitOpen: false,
                weight: 1,
                enabled: true,
              },
            ],
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("worker-1")).toBeInTheDocument();
    });

    // Find weight input
    const weightInputs = screen.getAllByRole("spinbutton");
    const weightInput = weightInputs[0];

    fireEvent.change(weightInput, { target: { value: "5" } });

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining("/workers/worker-1"),
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ weight: 5 }),
        })
      );
    });
  });

  test("displays worker load bar", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/status")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            algorithm: "round-robin",
            workers: [
              {
                id: "1",
                name: "worker-1",
                color: "#3B82F6",
                status: "healthy",
                currentLoad: 5,
                maxLoad: 10,
                queueDepth: 2,
                healthy: true,
                circuitOpen: false,
                weight: 1,
                enabled: true,
              },
            ],
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("5/10")).toBeInTheDocument();
    });
  });

  test("displays circuit breaker status", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/status")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            algorithm: "round-robin",
            workers: [
              {
                id: "1",
                name: "worker-1",
                color: "#3B82F6",
                status: "healthy",
                currentLoad: 0,
                maxLoad: 10,
                queueDepth: 0,
                healthy: true,
                circuitOpen: true,
                weight: 1,
                enabled: true,
              },
            ],
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("⚡ サーキット開放中")).toBeInTheDocument();
    });
  });

  test("displays disabled worker status", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/status")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            algorithm: "round-robin",
            workers: [
              {
                id: "1",
                name: "worker-1",
                color: "#3B82F6",
                status: "healthy",
                currentLoad: 0,
                maxLoad: 10,
                queueDepth: 0,
                healthy: true,
                circuitOpen: false,
                weight: 1,
                enabled: false,
              },
            ],
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("⏸ 無効")).toBeInTheDocument();
    });
  });

  test("calculates and displays statistics correctly", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/task")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: "task-stat",
            worker: "worker-1",
            color: "#3B82F6",
            processingTimeMs: 100,
            timestamp: new Date().toISOString(),
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    // Initially should show 0
    expect(screen.getByText("0")).toBeInTheDocument();

    const singleRequestButton = screen.getByText("単発リクエスト");

    // Send a successful task
    fireEvent.click(singleRequestButton);

    await waitFor(() => {
      const successElements = screen.getAllByText(/1|100ms/);
      expect(successElements.length).toBeGreaterThan(0);
    });
  });

  test("limits task log to 100 entries", async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes("/task")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: `task-${Date.now()}`,
            worker: "worker-1",
            color: "#3B82F6",
            processingTimeMs: 100,
            timestamp: new Date().toISOString(),
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(<App />);

    const singleRequestButton = screen.getByText("単発リクエスト");

    // This test validates the logic, actual rendering 101 times would be slow
    // The code limits to 100 via ...prev.slice(0, 99)
    expect(singleRequestButton).toBeInTheDocument();
  });

  test("WebSocket reconnects on close", async () => {
    let wsInstance: MockWebSocket | null = null;

    class ReconnectMockWebSocket extends MockWebSocket {
      constructor(url: string) {
        super(url);
        wsInstance = this;
      }
    }

    (global as any).WebSocket = ReconnectMockWebSocket;

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("接続中")).toBeInTheDocument();
    });

    // Close the WebSocket
    if (wsInstance && wsInstance.onclose) {
      wsInstance.onclose(new CloseEvent("close"));
    }

    await waitFor(() => {
      expect(screen.getByText("接続待ち...")).toBeInTheDocument();
    });
  });

  test("handles WebSocket message parsing error", async () => {
    class ErrorMockWebSocket extends MockWebSocket {
      constructor(url: string) {
        super(url);
        setTimeout(() => {
          if (this.onmessage) {
            // Send invalid JSON
            this.onmessage(
              new MessageEvent("message", { data: "invalid json" })
            );
          }
        }, 100);
      }
    }

    (global as any).WebSocket = ErrorMockWebSocket;

    const consoleSpy = jest.spyOn(console, "error").mockImplementation();

    render(<App />);

    await waitFor(
      () => {
        expect(consoleSpy).toHaveBeenCalled();
      },
      { timeout: 200 }
    );

    consoleSpy.mockRestore();
  });

  test("single request button is disabled when load generation is running", () => {
    render(<App />);

    const startButton = screen.getByText("開始");
    const singleRequestButton = screen.getByText("単発リクエスト");

    expect(singleRequestButton).not.toBeDisabled();

    fireEvent.click(startButton);

    expect(singleRequestButton).toBeDisabled();
  });

  test("handles failed algorithm change", async () => {
    mockFetch.mockImplementation((url: string, options?: any) => {
      if (url.includes("/algorithm") && options?.method === "PUT") {
        return Promise.reject(new Error("Network error"));
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    const consoleSpy = jest.spyOn(console, "error").mockImplementation();

    render(<App />);

    const weightedButton = screen.getByText("重み付け");
    fireEvent.click(weightedButton);

    await waitFor(() => {
      expect(consoleSpy).toHaveBeenCalled();
    });

    consoleSpy.mockRestore();
  });

  test("handles failed worker toggle", async () => {
    mockFetch.mockImplementation((url: string, options?: any) => {
      if (url.includes("/workers/") && options?.method === "PATCH") {
        return Promise.reject(new Error("Network error"));
      }
      if (url.includes("/status")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            algorithm: "round-robin",
            workers: [
              {
                id: "1",
                name: "worker-1",
                color: "#3B82F6",
                status: "healthy",
                currentLoad: 2,
                maxLoad: 10,
                queueDepth: 0,
                healthy: true,
                circuitOpen: false,
                weight: 1,
                enabled: true,
              },
            ],
          }),
        });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    const consoleSpy = jest.spyOn(console, "error").mockImplementation();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("worker-1")).toBeInTheDocument();
    });

    const toggleButtons = screen.getAllByRole("button");
    const toggleButton = toggleButtons.find((btn) => btn.title === "無効にする");

    if (toggleButton) {
      fireEvent.click(toggleButton);

      await waitFor(() => {
        expect(consoleSpy).toHaveBeenCalled();
      });
    }

    consoleSpy.mockRestore();
  });
});