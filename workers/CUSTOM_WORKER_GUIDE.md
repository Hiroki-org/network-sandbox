# Custom Worker Implementation Guide

This directory contains templates for implementing custom workers.

## Worker Interface Requirements

All workers must implement the following HTTP endpoints:

### Required Endpoints

| Endpoint   | Method | Description           |
| ---------- | ------ | --------------------- |
| `/health`  | GET    | Health check endpoint |
| `/task`    | POST   | Process a task        |
| `/metrics` | GET    | Prometheus metrics    |
| `/status`  | GET    | Worker status         |

### Health Check Response

```json
{
  "healthy": true,
  "status": "ok",
  "uptime": 12345,
  "version": "1.0.0"
}
```

### Task Request

```json
{
  "id": "task-123",
  "weight": 1.0
}
```

### Task Response

```json
{
  "id": "task-123",
  "worker": "my-worker-1",
  "color": "#10b981",
  "processing_time_ms": 42,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Status Response

```json
{
  "name": "my-worker-1",
  "color": "#10b981",
  "current_load": 5,
  "max_load": 10,
  "queue_depth": 3,
  "healthy": true,
  "weight": 1.0
}
```

### Prometheus Metrics

Expose at `/metrics` in Prometheus format:

```
# HELP worker_requests_total Total requests processed
# TYPE worker_requests_total counter
worker_requests_total{worker="my-worker-1",status="success"} 1234

# HELP worker_active_requests Current active requests
# TYPE worker_active_requests gauge
worker_active_requests{worker="my-worker-1"} 5

# HELP worker_queue_depth Current queue depth
# TYPE worker_queue_depth gauge
worker_queue_depth{worker="my-worker-1"} 3

# HELP worker_processing_time_ms Request processing time
# TYPE worker_processing_time_ms histogram
worker_processing_time_ms_bucket{worker="my-worker-1",le="10"} 100
worker_processing_time_ms_bucket{worker="my-worker-1",le="50"} 500
worker_processing_time_ms_bucket{worker="my-worker-1",le="100"} 800
worker_processing_time_ms_bucket{worker="my-worker-1",le="+Inf"} 1000
worker_processing_time_ms_sum{worker="my-worker-1"} 42000
worker_processing_time_ms_count{worker="my-worker-1"} 1000
```

## Environment Variables

Workers should accept these environment variables:

| Variable         | Required | Description             | Default      |
| ---------------- | -------- | ----------------------- | ------------ |
| `PORT`           | Yes      | HTTP server port        | -            |
| `WORKER_NAME`    | Yes      | Worker identifier       | -            |
| `WORKER_COLOR`   | Yes      | Hex color for UI        | -            |
| `MAX_CONCURRENT` | No       | Max concurrent requests | 10           |
| `QUEUE_CAPACITY` | No       | Request queue size      | 100          |
| `METRICS_PORT`   | No       | Separate metrics port   | Same as PORT |

## Template Files

- `Dockerfile.template` - Docker build template
- `main.template.go` - Go implementation template
- `main.template.py` - Python implementation template
- `main.template.rs` - Rust implementation template

## Quick Start

1. Copy the template for your preferred language
2. Implement the required endpoints
3. Build your Docker image
4. Add to `docker-compose.override.yml`:

```yaml
services:
  my-worker-1:
    build:
      context: ./workers/custom
      dockerfile: Dockerfile
    environment:
      - PORT=8090
      - WORKER_NAME=my-worker-1
      - WORKER_COLOR=#FF00FF
      - MAX_CONCURRENT=10
    networks:
      - sandbox-network
```

5. Update load balancer environment to include your worker:

```yaml
load-balancer:
  environment:
    - WORKER_CUSTOM_1_URL=http://my-worker-1:8090
```

6. Start the stack:

```bash
docker-compose up -d
```

## Testing Your Worker

```bash
# Health check
curl http://localhost:8090/health

# Send a task
curl -X POST http://localhost:8090/task \
  -H "Content-Type: application/json" \
  -d '{"id": "test-1", "weight": 1.0}'

# Check metrics
curl http://localhost:8090/metrics

# Check status
curl http://localhost:8090/status
```
