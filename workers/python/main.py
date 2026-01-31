import asyncio
import os
import random
import time
from datetime import datetime, timezone
from threading import Lock
from typing import Optional

from fastapi import FastAPI, HTTPException, Response
from fastapi.middleware.cors import CORSMiddleware
from prometheus_client import (
    CONTENT_TYPE_LATEST,
    Counter,
    Gauge,
    Histogram,
    generate_latest,
)
from pydantic import BaseModel


class Configuration(BaseModel):
    max_concurrent_requests: int = 10
    response_delay_ms: int = 100
    failure_rate: float = 0.0
    queue_size: int = 50


class TaskRequest(BaseModel):
    id: str
    weight: Optional[float] = 1.0


class TaskResponse(BaseModel):
    id: str
    worker: str
    color: str
    processingTimeMs: int
    timestamp: str


class ErrorResponse(BaseModel):
    error: str
    worker: str


class HealthResponse(BaseModel):
    status: str
    currentLoad: int
    queueDepth: int


# Environment configuration
WORKER_NAME = os.getenv("WORKER_NAME", "python-worker-1")
WORKER_COLOR = os.getenv("WORKER_COLOR", "#10B981")
PORT = int(os.getenv("PORT", "8080"))


def load_config() -> Configuration:
    return Configuration(
        max_concurrent_requests=int(os.getenv("MAX_CONCURRENT_REQUESTS", "10")),
        response_delay_ms=int(os.getenv("RESPONSE_DELAY_MS", "100")),
        failure_rate=float(os.getenv("FAILURE_RATE", "0.0")),
        queue_size=int(os.getenv("QUEUE_SIZE", "50")),
    )


# Global state
config = load_config()
config_lock = Lock()
active_requests = 0
requests_lock = Lock()
queue_semaphore: asyncio.Semaphore = None
queue_depth = 0
queue_depth_lock = Lock()

# Prometheus metrics
requests_total = Counter(
    "worker_requests_total",
    "Total number of requests",
    ["worker", "status"],
)
request_duration = Histogram(
    "worker_request_duration_ms",
    "Request duration in milliseconds",
    ["worker"],
    buckets=[1, 2, 4, 8, 16, 32, 64, 128, 256, 512],
)
current_load = Gauge(
    "worker_current_load",
    "Current number of active requests",
    ["worker"],
)

app = FastAPI(title="Python Worker")

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.on_event("startup")
async def startup():
    global queue_semaphore
    queue_semaphore = asyncio.Semaphore(config.queue_size)
    print(f"Starting {WORKER_NAME} on port {PORT} (color: {WORKER_COLOR})")
    print(
        f"Config: max_concurrent={config.max_concurrent_requests}, "
        f"delay={config.response_delay_ms}ms, "
        f"failure_rate={config.failure_rate:.2f}, "
        f"queue_size={config.queue_size}"
    )


@app.post("/task")
async def handle_task(task: TaskRequest):
    global active_requests, queue_depth

    # Try to acquire queue slot with timeout (non-blocking)
    try:
        await asyncio.wait_for(queue_semaphore.acquire(), timeout=0.01)
        with queue_depth_lock:
            queue_depth += 1
    except asyncio.TimeoutError:
        requests_total.labels(worker=WORKER_NAME, status="rejected").inc()
        raise HTTPException(
            status_code=503,
            detail={"error": "Queue full - service overloaded", "worker": WORKER_NAME},
        )

    try:
        # Check concurrent request limit
        with requests_lock:
            active_requests += 1
            current = active_requests
            current_load.labels(worker=WORKER_NAME).set(current)

        with config_lock:
            max_concurrent = config.max_concurrent_requests
            delay_ms = config.response_delay_ms
            failure_rate = config.failure_rate

        if current > max_concurrent:
            with requests_lock:
                active_requests -= 1
                current_load.labels(worker=WORKER_NAME).set(active_requests)
            requests_total.labels(worker=WORKER_NAME, status="overloaded").inc()
            raise HTTPException(
                status_code=503,
                detail={
                    "error": f"Max concurrent requests exceeded ({current}/{max_concurrent})",
                    "worker": WORKER_NAME,
                },
            )

        start_time = time.time()

        # Simulate processing with delay
        weight = max(task.weight or 1.0, 0.1)
        delay_seconds = (delay_ms * weight) / 1000
        await asyncio.sleep(delay_seconds)

        processing_time_ms = int((time.time() - start_time) * 1000)
        request_duration.labels(worker=WORKER_NAME).observe(processing_time_ms)

        # Cleanup active requests
        with requests_lock:
            active_requests -= 1
            current_load.labels(worker=WORKER_NAME).set(active_requests)

        # Simulate failure based on failure rate
        if random.random() < failure_rate:
            requests_total.labels(worker=WORKER_NAME, status="failed").inc()
            raise HTTPException(
                status_code=500,
                detail={"error": "Simulated failure", "worker": WORKER_NAME},
            )

        # Success response
        requests_total.labels(worker=WORKER_NAME, status="success").inc()

        return TaskResponse(
            id=task.id,
            worker=WORKER_NAME,
            color=WORKER_COLOR,
            processingTimeMs=processing_time_ms,
            timestamp=datetime.now(timezone.utc).isoformat(),
        )

    finally:
        queue_semaphore.release()
        with queue_depth_lock:
            queue_depth -= 1


@app.get("/health")
async def handle_health():
    with config_lock:
        max_concurrent = config.max_concurrent_requests
        max_queue = config.queue_size

    with requests_lock:
        load = active_requests

    with queue_depth_lock:
        depth = queue_depth

    load_ratio = load / max_concurrent if max_concurrent > 0 else 0
    queue_ratio = depth / max_queue if max_queue > 0 else 0

    if load_ratio >= 0.9 or queue_ratio >= 0.9:
        status = "unhealthy"
    elif load_ratio >= 0.7 or queue_ratio >= 0.7:
        status = "degraded"
    else:
        status = "healthy"

    return HealthResponse(status=status, currentLoad=load, queueDepth=depth)


@app.get("/config")
async def get_config():
    with config_lock:
        return config


@app.post("/config")
@app.put("/config")
async def update_config(new_config: Configuration):
    global config
    with config_lock:
        if new_config.max_concurrent_requests > 0:
            config.max_concurrent_requests = new_config.max_concurrent_requests
        if new_config.response_delay_ms >= 0:
            config.response_delay_ms = new_config.response_delay_ms
        if 0.0 <= new_config.failure_rate <= 1.0:
            config.failure_rate = new_config.failure_rate
        if new_config.queue_size > 0:
            config.queue_size = new_config.queue_size
        print(f"Config updated: {config}")
        return config


@app.get("/metrics")
async def metrics():
    return Response(content=generate_latest(), media_type=CONTENT_TYPE_LATEST)


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=PORT)
