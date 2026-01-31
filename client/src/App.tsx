import React, { useState, useEffect, useCallback, useRef } from 'react';

interface Worker {
  id: string;
  name: string;
  color: string;
  status: string;
  currentLoad: number;
  maxLoad: number;
  queueDepth: number;
  healthy: boolean;
  circuitOpen: boolean;
  weight: number;
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
  totalRequests: number;
  successRate: number;
}

const API_URL = process.env.REACT_APP_API_URL || 'http://localhost:8000';
const WS_URL = process.env.REACT_APP_WS_URL || 'ws://localhost:8000/ws';

const algorithms = [
  { id: 'round-robin', name: 'ラウンドロビン', desc: '順番に振り分け' },
  { id: 'least-connections', name: '最小接続', desc: '最も空いているワーカーへ' },
  { id: 'weighted', name: '重み付け', desc: '重みに基づいて振り分け' },
  { id: 'random', name: 'ランダム', desc: 'ランダムに選択' },
];

function App() {
  const [status, setStatus] = useState<LoadBalancerStatus | null>(null);
  const [tasks, setTasks] = useState<TaskResult[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [requestRate, setRequestRate] = useState(10);
  const [taskWeight, setTaskWeight] = useState(1.0);
  const [connected, setConnected] = useState(false);
  const intervalRef = useRef<NodeJS.Timeout | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const taskIdRef = useRef(0);

  // WebSocket connection
  useEffect(() => {
    const connect = () => {
      const ws = new WebSocket(WS_URL);
      wsRef.current = ws;

      ws.onopen = () => {
        setConnected(true);
        console.log('WebSocket connected');
      };

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          setStatus(data);
        } catch (e) {
          console.error('Failed to parse WebSocket message:', e);
        }
      };

      ws.onclose = () => {
        setConnected(false);
        console.log('WebSocket disconnected, reconnecting...');
        setTimeout(connect, 3000);
      };

      ws.onerror = (error) => {
        console.error('WebSocket error:', error);
      };
    };

    connect();

    return () => {
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
        console.error('Failed to fetch status:', e);
      }
    };

    fetchStatus();
  }, []);

  const sendTask = useCallback(async () => {
    const taskId = `task-${++taskIdRef.current}`;
    try {
      const response = await fetch(`${API_URL}/task`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: taskId, weight: taskWeight }),
      });

      const result = await response.json();

      if (response.ok) {
        setTasks((prev) => [
          { ...result, success: true },
          ...prev.slice(0, 99),
        ]);
      } else {
        setTasks((prev) => [
          {
            id: taskId,
            worker: result.worker || 'unknown',
            color: '#ef4444',
            processingTimeMs: 0,
            timestamp: new Date().toISOString(),
            success: false,
            error: result.error || 'Unknown error',
          },
          ...prev.slice(0, 99),
        ]);
      }
    } catch (e) {
      setTasks((prev) => [
        {
          id: taskId,
          worker: 'unknown',
          color: '#ef4444',
          processingTimeMs: 0,
          timestamp: new Date().toISOString(),
          success: false,
          error: 'Network error',
        },
        ...prev.slice(0, 99),
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
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ algorithm }),
      });
      if (response.ok) {
        setStatus((prev) => (prev ? { ...prev, algorithm } : null));
      }
    } catch (e) {
      console.error('Failed to change algorithm:', e);
    }
  };

  const getStatusColor = (worker: Worker) => {
    if (worker.circuitOpen) return 'bg-red-500';
    if (!worker.healthy) return 'bg-yellow-500';
    const loadRatio = worker.currentLoad / worker.maxLoad;
    if (loadRatio >= 0.9) return 'bg-red-500';
    if (loadRatio >= 0.7) return 'bg-yellow-500';
    return 'bg-green-500';
  };

  const successCount = tasks.filter((t) => t.success).length;
  const failureCount = tasks.filter((t) => !t.success).length;
  const avgResponseTime =
    tasks.length > 0
      ? Math.round(
          tasks.reduce((sum, t) => sum + t.processingTimeMs, 0) / tasks.length
        )
      : 0;

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
              className={`w-3 h-3 rounded-full ${
                connected ? 'bg-green-500' : 'bg-red-500'
              }`}
            />
            <span className="text-sm text-slate-400">
              {connected ? '接続中' : '接続待ち...'}
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
                    max="100"
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
                  className={`w-full py-3 px-4 rounded-lg font-semibold transition ${
                    isRunning
                      ? 'bg-red-600 hover:bg-red-700'
                      : 'bg-blue-600 hover:bg-blue-700'
                  }`}
                >
                  {isRunning ? '停止' : '開始'}
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
                    className={`w-full text-left px-4 py-3 rounded-lg transition ${
                      status?.algorithm === algo.id
                        ? 'bg-blue-600'
                        : 'bg-slate-700 hover:bg-slate-600'
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
                {status?.workers?.map((worker) => (
                  <div
                    key={worker.id}
                    className="bg-slate-700 rounded-lg p-4 border-l-4"
                    style={{ borderColor: worker.color }}
                  >
                    <div className="flex items-center justify-between mb-2">
                      <span className="font-medium">{worker.name}</span>
                      <span
                        className={`w-3 h-3 rounded-full ${getStatusColor(worker)}`}
                      />
                    </div>
                    <div className="space-y-2">
                      <div>
                        <div className="flex justify-between text-sm text-slate-400">
                          <span>負荷</span>
                          <span>
                            {worker.currentLoad}/{worker.maxLoad}
                          </span>
                        </div>
                        <div className="w-full bg-slate-600 rounded-full h-2 mt-1">
                          <div
                            className="h-2 rounded-full transition-all"
                            style={{
                              width: `${Math.min(
                                (worker.currentLoad / worker.maxLoad) * 100,
                                100
                              )}%`,
                              backgroundColor: worker.color,
                            }}
                          />
                        </div>
                      </div>
                      <div className="flex justify-between text-sm">
                        <span className="text-slate-400">キュー</span>
                        <span>{worker.queueDepth}</span>
                      </div>
                      <div className="flex justify-between text-sm">
                        <span className="text-slate-400">重み</span>
                        <span>{worker.weight}</span>
                      </div>
                      {worker.circuitOpen && (
                        <div className="text-red-400 text-sm">
                          ⚡ サーキット開放中
                        </div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Task Log */}
            <div className="bg-slate-800 rounded-lg p-6">
              <h2 className="text-xl font-semibold mb-4">タスクログ</h2>
              <div className="max-h-96 overflow-y-auto space-y-2">
                {tasks.length === 0 ? (
                  <div className="text-slate-400 text-center py-8">
                    リクエストを送信してください
                  </div>
                ) : (
                  tasks.map((task) => (
                    <div
                      key={task.id + task.timestamp}
                      className={`flex items-center justify-between p-3 rounded-lg ${
                        task.success ? 'bg-slate-700' : 'bg-red-900/30'
                      }`}
                    >
                      <div className="flex items-center gap-3">
                        <div
                          className="w-4 h-4 rounded-full"
                          style={{ backgroundColor: task.color }}
                        />
                        <div>
                          <span className="font-mono text-sm">{task.id}</span>
                          <span className="text-slate-400 text-sm ml-2">
                            → {task.worker}
                          </span>
                        </div>
                      </div>
                      <div className="text-right">
                        {task.success ? (
                          <span className="text-green-400">
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
