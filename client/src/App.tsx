import React, {
  useState,
  useEffect,
  useCallback,
  useRef,
  useMemo,
} from "react";
import { useDebouncedCallback } from "use-debounce";
import { apiUrl } from "./config/api";

interface Worker {
  id?: string;
  name: string;
  url: string;
  color: string;
  weight: number;
  maxLoad: number;
  healthy: boolean;
  currentLoad: number;
  enabled: boolean;
  totalRequests: number;
  failedRequests: number;
  circuitOpen: boolean;
  status?: string;
  queueDepth?: number;
}

interface WorkerConfig {
  max_concurrent_requests: number;
  response_delay_ms: number;
  failure_rate: number;
  queue_size: number;
}

interface TaskResult {
  id: string;
  worker: string;
  color: string;
  processingTimeMs: number;
  timestamp: string;
  success: boolean;
  error?: string;
}

interface LoadBalancerStatus {
  algorithm: string;
  workers: Worker[];
}

interface AlgorithmInfo {
  algorithm: string;
  available: string[];
}

interface RangeConfig {
  min: number;
  max: number;
  step: number;
}

interface UIConfigRanges {
  requests_per_second: RangeConfig;
  task_weight: RangeConfig;
  response_delay_ms: RangeConfig;
  failure_rate: RangeConfig;
  max_concurrent_requests: RangeConfig;
  queue_size: RangeConfig;
}

const defaultRanges: UIConfigRanges = {
  requests_per_second: { min: 1, max: 20, step: 1 },
  task_weight: { min: 0.1, max: 5, step: 0.1 },
  response_delay_ms: { min: 0, max: 5000, step: 100 },
  failure_rate: { min: 0, max: 100, step: 1 },
  max_concurrent_requests: { min: 1, max: 100, step: 1 },
  queue_size: { min: 1, max: 1000, step: 1 },
};

const WS_URL = process.env.REACT_APP_WS_URL || "ws://localhost:8000/ws";

const algorithms = [
  { id: "round-robin", name: "ラウンドロビン", desc: "順番に振り分け" },
  {
    id: "least-connections",
    name: "最小接続",
    desc: "最も空いているワーカーへ",
  },
  { id: "weighted", name: "重み付け", desc: "重みに基づいて振り分け" },
  { id: "random", name: "ランダム", desc: "ランダムに選択" },
];

// Log entry color based on response time
const getLogColor = (processingTimeMs: number, success: boolean) => {
  if (!success) return "bg-red-900/50 border-red-500";
  if (processingTimeMs >= 1200) return "bg-yellow-900/30 border-yellow-500";
  if (processingTimeMs >= 800) return "bg-amber-900/20 border-amber-400";
  return "bg-slate-700 border-green-500";
};

const getLogTextColor = (processingTimeMs: number, success: boolean) => {
  if (!success) return "text-red-400";
  if (processingTimeMs >= 1200) return "text-yellow-400";
  if (processingTimeMs >= 800) return "text-amber-400";
  return "text-green-400";
};

/**
 * 負荷分散ダッシュボードを表示し、ワーカーの状態監視・管理、負荷生成、アルゴリズム切替、及びリアルタイムのタスクログを提供するコンポーネント。
 *
 * WebSocket によるライブステータス受信、初期ステータスの取得、定期/単発のタスク送信（負荷生成）、ワーカーの有効化/無効化、重み変更・設定更新、およびタスクログの表示を行う UI を返します。
 *
 * @returns ダッシュボード全体を表す React 要素（JSX）
 */
function App() {
  const [status, setStatus] = useState<LoadBalancerStatus | null>(null);
  const [tasks, setTasks] = useState<TaskResult[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [requestRate, setRequestRate] = useState(2); // Changed from 10 to 2 RPS
  const [taskWeight, setTaskWeight] = useState(1.0);
  const [connected, setConnected] = useState(false);
  const [workerConfigs, setWorkerConfigs] = useState<Record<string, WorkerConfig>>({});
  const [workerConfigDrafts, setWorkerConfigDrafts] = useState<Record<string, WorkerConfig>>({});
  const [workerWeights, setWorkerWeights] = useState<Record<string, number>>({});
  const [ranges, setRanges] = useState<UIConfigRanges>(defaultRanges);
  const [expandedWorker, setExpandedWorker] = useState<string | null>(null);
  const intervalRef = useRef<NodeJS.Timeout | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const taskIdRef = useRef(0);

  // WebSocket connection
  useEffect(() => {
    let reconnectTimeoutId: NodeJS.Timeout | null = null;

    const connect = () => {
      const ws = new WebSocket(WS_URL);
      wsRef.current = ws;

      ws.onopen = () => {
        setConnected(true);
        console.log("WebSocket connected");
      };

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          setStatus(data);
        } catch (e) {
          console.error("Failed to parse WebSocket message:", e);
        }
      };

      ws.onclose = () => {
        setConnected(false);
        console.log("WebSocket disconnected, reconnecting...");
        reconnectTimeoutId = setTimeout(connect, 3000);
      };

      ws.onerror = (error) => {
        console.error("WebSocket error:", error);
      };
    };

    connect();

    return () => {
      if (reconnectTimeoutId) {
        clearTimeout(reconnectTimeoutId);
      }
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  // Fetch initial status
  useEffect(() => {
    const fetchStatus = async () => {
      try {
        const response = await fetch(apiUrl("/api/status"));
        if (response.ok) {
          const data = await response.json();
          setStatus(data);
        }
      } catch (e) {
        console.error("Failed to fetch status:", e);
      }
    };

    fetchStatus();
  }, []);

  // Fetch UI config ranges
  useEffect(() => {
    const fetchRanges = async () => {
      try {
        const response = await fetch(apiUrl("/api/config/ranges"));
        if (!response.ok) return;

        const data = await response.json();
        setRanges((prev) => ({
          ...prev,
          requests_per_second: {
            ...prev.requests_per_second,
            min: Number(data?.requests_per_second?.min ?? prev.requests_per_second.min),
            max: Number(data?.requests_per_second?.max ?? prev.requests_per_second.max),
          },
          task_weight: {
            ...prev.task_weight,
            min: Number(data?.task_weight?.min ?? prev.task_weight.min),
            max: Number(data?.task_weight?.max ?? prev.task_weight.max),
          },
          response_delay_ms: {
            ...prev.response_delay_ms,
            min: Number(data?.response_delay_ms?.min ?? prev.response_delay_ms.min),
            max: Number(data?.response_delay_ms?.max ?? prev.response_delay_ms.max),
          },
          failure_rate: {
            ...prev.failure_rate,
            min: Number(data?.failure_rate?.min ?? prev.failure_rate.min),
            max: Number(data?.failure_rate?.max ?? prev.failure_rate.max),
          },
          max_concurrent_requests: {
            ...prev.max_concurrent_requests,
            min: Number(data?.max_concurrent_requests?.min ?? prev.max_concurrent_requests.min),
            max: Number(data?.max_concurrent_requests?.max ?? prev.max_concurrent_requests.max),
          },
        }));
      } catch (e) {
        console.error("Failed to fetch config ranges:", e);
      }
    };

    fetchRanges();
  }, []);

  // Fetch worker configs when status changes
  const workerNames = useMemo(
    () => status?.workers?.map((w) => w.name).join(",") ?? "",
    [status?.workers]
  );

  useEffect(() => {
    if (!status?.workers) return;

    setWorkerWeights((prev) => {
      const next: Record<string, number> = {};
      for (const worker of status.workers) {
        next[worker.name] = prev[worker.name] ?? worker.weight;
      }
      return next;
    });

    const fetchConfigs = async () => {
      const results = await Promise.allSettled(
        status.workers.map(async (worker) => {
          const response = await fetch(apiUrl(`/api/workers/${worker.name}/config`));
          if (!response.ok) {
            throw new Error(`status ${response.status}`);
          }
          const data = await response.json();
          return { name: worker.name, data };
        })
      );

      const configs: Record<string, WorkerConfig> = {};
      for (const result of results) {
        if (result.status === "fulfilled") {
          configs[result.value.name] = result.value.data;
        } else {
          console.error("Failed to fetch worker config:", result.reason);
        }
      }

      setWorkerConfigs(configs);
      setWorkerConfigDrafts((prev) => ({ ...prev, ...configs }));
    };

    fetchConfigs();
  }, [workerNames, status?.workers]);

  const debouncedUpdateWorkerWeight = useDebouncedCallback(
    (workerName: string, weight: number) => {
      updateWorkerWeight(workerName, weight);
    },
    300,
  );

  const debouncedUpdateWorkerConfig = useDebouncedCallback(
    (workerName: string, config: Partial<WorkerConfig>) => {
      updateWorkerConfig(workerName, config);
    },
    300,
  );

  const sendTask = useCallback(async () => {
    const taskId = `task-${++taskIdRef.current}`;
    try {
      const response = await fetch(apiUrl("/api/task"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id: taskId, weight: taskWeight }),
      });

      const result = await response.json();

      if (response.ok) {
        setTasks((prev) => [
          { ...result, success: true },
          ...prev.slice(0, 49), // Keep max 50 entries
        ]);
      } else {
        setTasks((prev) => [
          {
            id: taskId,
            worker: result.worker || "unknown",
            color: "#ef4444",
            processingTimeMs: 0,
            timestamp: new Date().toISOString(),
            success: false,
            error: result.error || "Unknown error",
          },
          ...prev.slice(0, 49),
        ]);
      }
    } catch (e) {
      setTasks((prev) => [
        {
          id: taskId,
          worker: "unknown",
          color: "#ef4444",
          processingTimeMs: 0,
          timestamp: new Date().toISOString(),
          success: false,
          error: "Network error",
        },
        ...prev.slice(0, 49),
      ]);
    }
  }, [taskWeight]);

  // Load generator
  useEffect(() => {
    if (isRunning) {
      const interval = 1000 / requestRate;
      intervalRef.current = setInterval(sendTask, interval);
    } else if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [isRunning, requestRate, sendTask]);

  const changeAlgorithm = async (algorithm: string) => {
    try {
      const response = await fetch(apiUrl("/api/algorithm"), {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ algorithm }),
      });
      if (response.ok) {
        setStatus((prev) => (prev ? { ...prev, algorithm } : null));
      }
    } catch (e) {
      console.error("Failed to change algorithm:", e);
    }
  };

  const toggleWorker = async (workerName: string, enabled: boolean) => {
    try {
      const response = await fetch(apiUrl(`/api/workers/${workerName}`), {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled }),
      });
      if (response.ok && status) {
        setStatus({
          ...status,
          workers: status.workers.map((w) =>
            w.name === workerName ? { ...w, enabled } : w,
          ),
        });
      }
    } catch (e) {
      console.error("Failed to toggle worker:", e);
    }
  };

  const updateWorkerWeight = async (workerName: string, weight: number) => {
    try {
      await fetch(apiUrl(`/api/workers/${workerName}`), {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ weight }),
      });
    } catch (e) {
      console.error("Failed to update worker weight:", e);
    }
  };

  const updateWorkerConfig = async (workerName: string, config: Partial<WorkerConfig>) => {
    try {
      const response = await fetch(apiUrl(`/api/workers/${workerName}/config`), {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(config),
      });
      if (response.ok) {
        const updatedConfig = await response.json();
        setWorkerConfigs((prev) => ({
          ...prev,
          [workerName]: updatedConfig,
        }));
        setWorkerConfigDrafts((prev) => ({
          ...prev,
          [workerName]: updatedConfig,
        }));
      }
    } catch (e) {
      console.error("Failed to update worker config:", e);
    }
  };

  const getStatusColor = (worker: Worker) => {
    if (worker.circuitOpen) return "bg-red-500";
    if (!worker.healthy) return "bg-yellow-500";
    const loadRatio = worker.currentLoad / worker.maxLoad;
    if (loadRatio >= 0.9) return "bg-red-500";
    if (loadRatio >= 0.7) return "bg-yellow-500";
    return "bg-green-500";
  };

  const stats = useMemo(() => {
    const successCount = tasks.filter((t) => t.success).length;
    const failureCount = tasks.filter((t) => !t.success).length;
    const avgResponseTime =
      tasks.length > 0
        ? Math.round(
          tasks.reduce((sum, t) => sum + t.processingTimeMs, 0) /
          tasks.length,
        )
        : 0;
    return { successCount, failureCount, avgResponseTime };
  }, [tasks]);

  const { successCount, failureCount, avgResponseTime } = stats;

  const formatTimestamp = (timestamp: string) => {
    const date = new Date(timestamp);
    return date.toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 1 });
  };

  const handleWorkerWeightChange = (workerName: string, value: number) => {
    setWorkerWeights((prev) => ({
      ...prev,
      [workerName]: value,
    }));
    debouncedUpdateWorkerWeight(workerName, value);
  };

  const handleWorkerConfigChange = (
    workerName: string,
    key: keyof WorkerConfig,
    value: number,
  ) => {
    setWorkerConfigDrafts((prev) => ({
      ...prev,
      [workerName]: {
        ...(prev[workerName] ?? workerConfigs[workerName]),
        [key]: value,
      },
    }));
    debouncedUpdateWorkerConfig(workerName, { [key]: value } as Partial<WorkerConfig>);
  };

  return (
    <div className="min-h-screen bg-slate-900 text-white p-6">
      <div className="max-w-7xl mx-auto">
        {/* Header */}
        <header className="mb-8">
          <h1 className="text-3xl font-bold mb-2">Network Sandbox</h1>
          <p className="text-slate-400">
            分散システムの負荷分散をリアルタイムで可視化
          </p>
          <div className="flex items-center gap-2 mt-2">
            <span
              className={`w-3 h-3 rounded-full ${connected ? "bg-green-500" : "bg-red-500"
                }`}
            />
            <span className="text-sm text-slate-400">
              {connected ? "接続中" : "接続待ち..."}
            </span>
          </div>
        </header>

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Control Panel */}
          <div className="lg:col-span-1 space-y-6">
            {/* Load Generator */}
            <div className="bg-slate-800 rounded-lg p-6">
              <h2 className="text-xl font-semibold mb-4">負荷生成</h2>
              <div className="space-y-4">
                <div>
                  <label htmlFor="request-rate-slider" className="block text-sm text-slate-400 mb-2">
                    リクエストレート: {requestRate}/秒
                  </label>
                  <input
                    id="request-rate-slider"
                    type="range"
                    min={ranges.requests_per_second.min}
                    max={ranges.requests_per_second.max}
                    step={ranges.requests_per_second.step}
                    value={requestRate}
                    onChange={(e) => setRequestRate(Number(e.target.value))}
                    className="w-full"
                  />
                </div>
                <div>
                  <label htmlFor="task-weight-slider" className="block text-sm text-slate-400 mb-2">
                    タスク重み: {taskWeight.toFixed(1)}x
                  </label>
                  <input
                    id="task-weight-slider"
                    type="range"
                    min={ranges.task_weight.min}
                    max={ranges.task_weight.max}
                    step={ranges.task_weight.step}
                    value={taskWeight}
                    onChange={(e) => setTaskWeight(Number(e.target.value))}
                    className="w-full"
                  />
                </div>
                <button
                  type="button"
                  onClick={() => setIsRunning(!isRunning)}
                  className={`w-full py-3 px-4 rounded-lg font-semibold transition ${isRunning
                    ? "bg-red-600 hover:bg-red-700"
                    : "bg-blue-600 hover:bg-blue-700"
                    }`}
                >
                  {isRunning ? "停止" : "開始"}
                </button>
                <button
                  type="button"
                  onClick={sendTask}
                  disabled={isRunning}
                  className="w-full py-2 px-4 rounded-lg bg-slate-700 hover:bg-slate-600 transition disabled:opacity-50"
                >
                  単発リクエスト
                </button>
              </div>
            </div>

            {/* Algorithm Selector */}
            <div className="bg-slate-800 rounded-lg p-6">
              <h2 className="text-xl font-semibold mb-4">アルゴリズム</h2>
              <div className="space-y-2">
                {algorithms.map((algo) => (
                  <button
                    type="button"
                    key={algo.id}
                    onClick={() => changeAlgorithm(algo.id)}
                    className={`w-full text-left px-4 py-3 rounded-lg transition ${status?.algorithm === algo.id
                      ? "bg-blue-600"
                      : "bg-slate-700 hover:bg-slate-600"
                      }`}
                  >
                    <div className="font-medium">{algo.name}</div>
                    <div className="text-sm text-slate-400">{algo.desc}</div>
                  </button>
                ))}
              </div>
            </div>

            {/* Statistics */}
            <div className="bg-slate-800 rounded-lg p-6">
              <h2 className="text-xl font-semibold mb-4">統計</h2>
              <div className="grid grid-cols-2 gap-4">
                <div className="bg-slate-700 rounded-lg p-4">
                  <div className="text-2xl font-bold text-green-400">
                    {successCount}
                  </div>
                  <div className="text-sm text-slate-400">成功</div>
                </div>
                <div className="bg-slate-700 rounded-lg p-4">
                  <div className="text-2xl font-bold text-red-400">
                    {failureCount}
                  </div>
                  <div className="text-sm text-slate-400">失敗</div>
                </div>
                <div className="bg-slate-700 rounded-lg p-4 col-span-2">
                  <div className="text-2xl font-bold text-blue-400">
                    {avgResponseTime}ms
                  </div>
                  <div className="text-sm text-slate-400">平均応答時間</div>
                </div>
              </div>
            </div>
          </div>

          {/* Main Content */}
          <div className="lg:col-span-2 space-y-6">
            {/* Worker Grid */}
            <div className="bg-slate-800 rounded-lg p-6">
              <h2 className="text-xl font-semibold mb-4">ワーカー状態</h2>
              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
                {status?.workers?.map((worker) => {
                  const config = workerConfigs[worker.name];
                  const draftConfig = workerConfigDrafts[worker.name] ?? config;
                  const isExpanded = expandedWorker === worker.name;

                  return (
                    <div
                      key={worker.id || worker.name}
                      className={`bg-slate-700 rounded-lg p-4 border-l-4 transition-opacity ${!worker.enabled ? "opacity-50" : ""}`}
                      style={{ borderColor: worker.color }}
                    >
                      <div className="flex items-center justify-between mb-2">
                        <span className="font-medium">{worker.name}</span>
                        <div className="flex items-center gap-2">
                          <span
                            className={`w-3 h-3 rounded-full ${getStatusColor(worker)}`}
                          />
                          <button
                            type="button"
                            onClick={() =>
                              toggleWorker(worker.name, !worker.enabled)
                            }
                            className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${worker.enabled ? "bg-blue-600" : "bg-slate-500"}`}
                            title={worker.enabled ? "無効にする" : "有効にする"}
                          >
                            <span
                              className={`inline-block h-3 w-3 transform rounded-full bg-white transition-transform ${worker.enabled ? "translate-x-5" : "translate-x-1"}`}
                            />
                          </button>
                        </div>
                      </div>
                      <div className="space-y-2">
                        <div>
                          <div className="flex justify-between text-sm text-slate-400 mb-1">
                            <span>負荷</span>
                            <span className="font-mono">
                              {worker.currentLoad}/{worker.maxLoad}{" "}
                              <span className="text-xs">
                                ({Math.round((worker.currentLoad / (worker.maxLoad || 1)) * 100)}%)
                              </span>
                            </span>
                          </div>
                          {/* Segmented load bar */}
                          <div className="flex gap-0.5 h-3">
                            {Array.from({ length: worker.maxLoad || 5 }).map((_, idx) => {
                              const isActive = idx < worker.currentLoad;
                              const loadPercent = worker.currentLoad / (worker.maxLoad || 1);
                              let barColor = worker.color;
                              if (isActive) {
                                if (loadPercent >= 0.9) {
                                  barColor = "#ef4444"; // red
                                } else if (loadPercent >= 0.7) {
                                  barColor = "#f59e0b"; // amber
                                } else if (loadPercent >= 0.5) {
                                  barColor = "#eab308"; // yellow
                                }
                              }
                              return (
                                <div
                                  key={idx}
                                  className="flex-1 rounded-sm transition-all duration-300"
                                  style={{
                                    backgroundColor: isActive ? barColor : "#475569",
                                    opacity: isActive ? 1 : 0.3,
                                  }}
                                />
                              );
                            })}
                          </div>
                          {/* Capacity indicator */}
                          <div className="flex justify-between text-xs text-slate-500 mt-1">
                            <span>0</span>
                            <span>容量: {worker.maxLoad}</span>
                          </div>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-slate-400">キュー</span>
                          <span>{worker.queueDepth ?? 0}</span>
                        </div>
                        <div className="flex justify-between items-center text-sm">
                          <span className="text-slate-400">重み</span>
                          <label htmlFor={`worker-weight-${worker.name}`} className="sr-only">
                            {worker.name} の重み
                          </label>
                          <input
                            id={`worker-weight-${worker.name}`}
                            type="number"
                            min={ranges.task_weight.min}
                            max={ranges.task_weight.max}
                            step={ranges.task_weight.step}
                            value={workerWeights[worker.name] ?? worker.weight}
                            onChange={(e) =>
                              handleWorkerWeightChange(
                                worker.name,
                                Number(e.target.value),
                              )
                            }
                            className="w-14 bg-slate-600 rounded px-2 py-1 text-center"
                          />
                        </div>

                        {/* Config Panel Toggle */}
                        <button
                          type="button"
                          onClick={() => setExpandedWorker(isExpanded ? null : worker.name)}
                          className="w-full text-sm text-slate-400 hover:text-white py-1 flex items-center justify-center gap-1"
                        >
                          <span>{isExpanded ? "▲ 設定を閉じる" : "▼ 設定を開く"}</span>
                        </button>

                        {/* Expanded Config Panel */}
                        {isExpanded && draftConfig && (
                          <div className="mt-3 pt-3 border-t border-slate-600 space-y-3">
                            <div>
                              <label htmlFor={`worker-max-concurrent-${worker.name}`} className="block text-xs text-slate-400 mb-1">
                                同時リクエスト数: {draftConfig.max_concurrent_requests}
                              </label>
                              <input
                                id={`worker-max-concurrent-${worker.name}`}
                                type="number"
                                min={ranges.max_concurrent_requests.min}
                                max={ranges.max_concurrent_requests.max}
                                step={ranges.max_concurrent_requests.step}
                                value={draftConfig.max_concurrent_requests}
                                onChange={(e) =>
                                  handleWorkerConfigChange(
                                    worker.name,
                                    "max_concurrent_requests",
                                    Number(e.target.value),
                                  )
                                }
                                className="w-full bg-slate-600 rounded px-2 py-1 text-sm"
                              />
                            </div>
                            <div>
                              <label htmlFor={`worker-response-delay-${worker.name}`} className="block text-xs text-slate-400 mb-1">
                                応答遅延: {draftConfig.response_delay_ms}ms
                              </label>
                              <input
                                id={`worker-response-delay-${worker.name}`}
                                type="range"
                                min={ranges.response_delay_ms.min}
                                max={ranges.response_delay_ms.max}
                                step={ranges.response_delay_ms.step}
                                value={draftConfig.response_delay_ms}
                                onChange={(e) =>
                                  handleWorkerConfigChange(
                                    worker.name,
                                    "response_delay_ms",
                                    Number(e.target.value),
                                  )
                                }
                                className="w-full"
                              />
                            </div>
                            <div>
                              <label htmlFor={`worker-failure-rate-${worker.name}`} className="block text-xs text-slate-400 mb-1">
                                失敗率: {(draftConfig.failure_rate * 100).toFixed(0)}%
                              </label>
                              <input
                                id={`worker-failure-rate-${worker.name}`}
                                type="range"
                                min={ranges.failure_rate.min}
                                max={ranges.failure_rate.max}
                                step={ranges.failure_rate.step}
                                value={Math.round(draftConfig.failure_rate * 100)}
                                onChange={(e) =>
                                  handleWorkerConfigChange(
                                    worker.name,
                                    "failure_rate",
                                    Number(e.target.value) / 100,
                                  )
                                }
                                className="w-full"
                              />
                            </div>
                            <div>
                              <label htmlFor={`worker-queue-size-${worker.name}`} className="block text-xs text-slate-400 mb-1">
                                キューサイズ: {draftConfig.queue_size}
                              </label>
                              <input
                                id={`worker-queue-size-${worker.name}`}
                                type="number"
                                min={ranges.queue_size.min}
                                max={ranges.queue_size.max}
                                step={ranges.queue_size.step}
                                value={draftConfig.queue_size}
                                onChange={(e) =>
                                  handleWorkerConfigChange(
                                    worker.name,
                                    "queue_size",
                                    Number(e.target.value),
                                  )
                                }
                                className="w-full bg-slate-600 rounded px-2 py-1 text-sm"
                              />
                            </div>
                          </div>
                        )}

                        {!worker.enabled && (
                          <div className="text-yellow-400 text-sm">⏸ 無効</div>
                        )}
                        {worker.circuitOpen && (
                          <div className="text-red-400 text-sm">
                            ⚡ サーキット開放中
                          </div>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Enhanced Task Log */}
            <div className="bg-slate-800 rounded-lg p-6">
              <div className="flex items-center justify-between mb-4">
                <h2 className="text-xl font-semibold">リアルタイムログ</h2>
                <div className="flex items-center gap-4 text-xs">
                  <span className="flex items-center gap-1">
                    <span className="w-2 h-2 rounded-full bg-green-500"></span> 正常
                  </span>
                  <span className="flex items-center gap-1">
                    <span className="w-2 h-2 rounded-full bg-amber-400"></span> 遅延(800ms+)
                  </span>
                  <span className="flex items-center gap-1">
                    <span className="w-2 h-2 rounded-full bg-yellow-500"></span> 遅延(1.2s+)
                  </span>
                  <span className="flex items-center gap-1">
                    <span className="w-2 h-2 rounded-full bg-red-500"></span> エラー
                  </span>
                </div>
              </div>
              <div className="max-h-96 overflow-y-auto space-y-2">
                {tasks.length === 0 ? (
                  <div className="text-slate-400 text-center py-8">
                    リクエストを送信してください
                  </div>
                ) : (
                  tasks.map((task) => (
                    <div
                      key={task.id}
                      className={`flex items-center justify-between p-3 rounded-lg border-l-4 ${getLogColor(task.processingTimeMs, task.success)}`}
                    >
                      <div className="flex items-center gap-3">
                        <div
                          className="w-4 h-4 rounded-full flex-shrink-0"
                          style={{ backgroundColor: task.color }}
                        />
                        <div className="flex flex-col">
                          <div className="flex items-center gap-2">
                            <span className="font-mono text-sm">{task.id}</span>
                            <span className="text-slate-400 text-sm">
                              → {task.worker}
                            </span>
                          </div>
                          <span className="text-xs text-slate-500">
                            {formatTimestamp(task.timestamp)}
                          </span>
                        </div>
                      </div>
                      <div className="text-right">
                        {task.success ? (
                          <span className={`font-mono ${getLogTextColor(task.processingTimeMs, task.success)}`}>
                            {task.processingTimeMs}ms
                          </span>
                        ) : (
                          <span className="text-red-400 text-sm">
                            {task.error}
                          </span>
                        )}
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export default App;