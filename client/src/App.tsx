import React, {
  useState,
  useEffect,
  useCallback,
  useRef,
  useMemo,
} from "react";
import { LoadBalancerStatus, TaskResult, WorkerConfig } from "./types";
import WorkerCard from "./components/WorkerCard";

const API_URL = process.env.REACT_APP_API_URL || "http://localhost:8000";
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
        const response = await fetch(`${API_URL}/status`);
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

  // Fetch worker configs when status changes
  const workerNames = useMemo(
    () => status?.workers?.map((w) => w.name).join(",") ?? "",
    [status?.workers]
  );

  useEffect(() => {
    if (!status?.workers) return;

    const fetchConfigs = async () => {
      const configs: Record<string, WorkerConfig> = {};
      for (const worker of status.workers) {
        try {
          const response = await fetch(`${API_URL}/workers/${worker.name}/config`);
          if (response.ok) {
            const data = await response.json();
            configs[worker.name] = data;
          }
        } catch (e) {
          console.error(`Failed to fetch config for ${worker.name}:`, e);
        }
      }
      setWorkerConfigs(configs);
    };

    fetchConfigs();
  }, [workerNames, status?.workers]);

  const sendTask = useCallback(async () => {
    const taskId = `task-${++taskIdRef.current}`;
    try {
      const response = await fetch(`${API_URL}/task`, {
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
      const response = await fetch(`${API_URL}/algorithm`, {
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

  const toggleWorker = useCallback(async (workerName: string, enabled: boolean) => {
    try {
      const response = await fetch(`${API_URL}/workers/${workerName}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled }),
      });
      if (response.ok) {
        setStatus((prev) => {
          if (!prev) return null;
          return {
            ...prev,
            workers: prev.workers.map((w) =>
              w.name === workerName ? { ...w, enabled } : w,
            ),
          };
        });
      }
    } catch (e) {
      console.error("Failed to toggle worker:", e);
    }
  }, []);

  const updateWorkerWeight = useCallback(async (workerName: string, weight: number) => {
    try {
      await fetch(`${API_URL}/workers/${workerName}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ weight }),
      });
    } catch (e) {
      console.error("Failed to update worker weight:", e);
    }
  }, []);

  const updateWorkerConfig = useCallback(async (workerName: string, config: Partial<WorkerConfig>) => {
    try {
      const response = await fetch(`${API_URL}/workers/${workerName}/config`, {
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
      }
    } catch (e) {
      console.error("Failed to update worker config:", e);
    }
  }, []);

  const onToggleExpand = useCallback((workerName: string) => {
    setExpandedWorker(prev => prev === workerName ? null : workerName);
  }, []);

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
                  <label className="block text-sm text-slate-400 mb-2">
                    リクエストレート: {requestRate}/秒
                  </label>
                  <input
                    type="range"
                    min="1"
                    max="20"
                    value={requestRate}
                    onChange={(e) => setRequestRate(Number(e.target.value))}
                    className="w-full"
                  />
                </div>
                <div>
                  <label className="block text-sm text-slate-400 mb-2">
                    タスク重み: {taskWeight.toFixed(1)}x
                  </label>
                  <input
                    type="range"
                    min="0.1"
                    max="5"
                    step="0.1"
                    value={taskWeight}
                    onChange={(e) => setTaskWeight(Number(e.target.value))}
                    className="w-full"
                  />
                </div>
                <button
                  onClick={() => setIsRunning(!isRunning)}
                  className={`w-full py-3 px-4 rounded-lg font-semibold transition ${isRunning
                    ? "bg-red-600 hover:bg-red-700"
                    : "bg-blue-600 hover:bg-blue-700"
                    }`}
                >
                  {isRunning ? "停止" : "開始"}
                </button>
                <button
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
                  const isExpanded = expandedWorker === worker.name;

                  return (
                    <WorkerCard
                      key={worker.id || worker.name}
                      worker={worker}
                      config={config}
                      isExpanded={isExpanded}
                      onToggle={toggleWorker}
                      onUpdateWeight={updateWorkerWeight}
                      onUpdateConfig={updateWorkerConfig}
                      onToggleExpand={onToggleExpand}
                    />
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
