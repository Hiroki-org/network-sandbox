import React, { memo } from "react";
import { Worker, WorkerConfig } from "../types";

interface WorkerCardProps {
  worker: Worker;
  config?: WorkerConfig;
  isExpanded: boolean;
  onToggle: (workerName: string, enabled: boolean) => void;
  onUpdateWeight: (workerName: string, weight: number) => void;
  onUpdateConfig: (workerName: string, config: Partial<WorkerConfig>) => void;
  onToggleExpand: (workerName: string) => void;
}

const getStatusColor = (worker: Worker) => {
  if (worker.circuitOpen) return "bg-red-500";
  if (!worker.healthy) return "bg-yellow-500";
  const loadRatio = worker.currentLoad / (worker.maxLoad || 1);
  if (loadRatio >= 0.9) return "bg-red-500";
  if (loadRatio >= 0.7) return "bg-yellow-500";
  return "bg-green-500";
};

const WorkerCard: React.FC<WorkerCardProps> = ({
  worker,
  config,
  isExpanded,
  onToggle,
  onUpdateWeight,
  onUpdateConfig,
  onToggleExpand,
}) => {
  return (
    <div
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
            onClick={() => onToggle(worker.name, !worker.enabled)}
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
          <input
            type="number"
            min="1"
            max="10"
            value={worker.weight}
            onChange={(e) =>
              onUpdateWeight(
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
          onClick={() => onToggleExpand(worker.name)}
          className="w-full text-sm text-slate-400 hover:text-white py-1 flex items-center justify-center gap-1"
        >
          <span>{isExpanded ? "▲ 設定を閉じる" : "▼ 設定を開く"}</span>
        </button>

        {/* Expanded Config Panel */}
        {isExpanded && config && (
          <div className="mt-3 pt-3 border-t border-slate-600 space-y-3">
            <div>
              <label className="block text-xs text-slate-400 mb-1">
                同時リクエスト数: {config.max_concurrent_requests}
              </label>
              <input
                type="number"
                min="1"
                max="100"
                value={config.max_concurrent_requests}
                onChange={(e) =>
                  onUpdateConfig(worker.name, {
                    max_concurrent_requests: Number(e.target.value),
                  })
                }
                className="w-full bg-slate-600 rounded px-2 py-1 text-sm"
              />
            </div>
            <div>
              <label className="block text-xs text-slate-400 mb-1">
                応答遅延: {config.response_delay_ms}ms
              </label>
              <input
                type="range"
                min="0"
                max="5000"
                step="100"
                value={config.response_delay_ms}
                onChange={(e) =>
                  onUpdateConfig(worker.name, {
                    response_delay_ms: Number(e.target.value),
                  })
                }
                className="w-full"
              />
            </div>
            <div>
              <label className="block text-xs text-slate-400 mb-1">
                失敗率: {(config.failure_rate * 100).toFixed(0)}%
              </label>
              <input
                type="range"
                min="0"
                max="1"
                step="0.01"
                value={config.failure_rate}
                onChange={(e) =>
                  onUpdateConfig(worker.name, {
                    failure_rate: Number(e.target.value),
                  })
                }
                className="w-full"
              />
            </div>
            <div>
              <label className="block text-xs text-slate-400 mb-1">
                キューサイズ: {config.queue_size}
              </label>
              <input
                type="number"
                min="1"
                max="1000"
                value={config.queue_size}
                onChange={(e) =>
                  onUpdateConfig(worker.name, {
                    queue_size: Number(e.target.value),
                  })
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
};

export default memo(WorkerCard);
