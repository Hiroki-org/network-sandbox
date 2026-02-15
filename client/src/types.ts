export interface Worker {
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

export interface WorkerConfig {
  max_concurrent_requests: number;
  response_delay_ms: number;
  failure_rate: number;
  queue_size: number;
}

export interface TaskResult {
  id: string;
  worker: string;
  color: string;
  processingTimeMs: number;
  timestamp: string;
  success: boolean;
  error?: string;
}

export interface LoadBalancerStatus {
  algorithm: string;
  workers: Worker[];
}

export interface AlgorithmInfo {
  algorithm: string;
  available: string[];
}
