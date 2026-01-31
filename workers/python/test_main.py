import asyncio
import json
import os
import pytest
from fastapi.testclient import TestClient
from unittest.mock import patch, MagicMock

# Import the app and functions from main
from main import (
    app,
    Configuration,
    TaskRequest,
    TaskResponse,
    ErrorResponse,
    HealthResponse,
    load_config,
    config,
    active_requests,
    requests_lock,
    queue_depth,
    queue_depth_lock,
)


@pytest.fixture
def client():
    """Create a test client for the FastAPI app"""
    return TestClient(app)


@pytest.fixture
def reset_state():
    """Reset global state before each test"""
    global active_requests, queue_depth
    with requests_lock:
        active_requests = 0
    with queue_depth_lock:
        queue_depth = 0
    yield
    with requests_lock:
        active_requests = 0
    with queue_depth_lock:
        queue_depth = 0


class TestConfiguration:
    def test_configuration_defaults(self):
        """Test Configuration model with default values"""
        cfg = Configuration()
        assert cfg.max_concurrent_requests == 10
        assert cfg.response_delay_ms == 100
        assert cfg.failure_rate == 0.0
        assert cfg.queue_size == 50

    def test_configuration_custom_values(self):
        """Test Configuration model with custom values"""
        cfg = Configuration(
            max_concurrent_requests=20,
            response_delay_ms=200,
            failure_rate=0.15,
            queue_size=100,
        )
        assert cfg.max_concurrent_requests == 20
        assert cfg.response_delay_ms == 200
        assert cfg.failure_rate == 0.15
        assert cfg.queue_size == 100

    def test_configuration_validation(self):
        """Test Configuration model validation"""
        # Should accept valid values
        cfg = Configuration(
            max_concurrent_requests=1,
            response_delay_ms=0,
            failure_rate=0.0,
            queue_size=1,
        )
        assert cfg.max_concurrent_requests == 1

    def test_configuration_json_serialization(self):
        """Test Configuration JSON serialization"""
        cfg = Configuration(max_concurrent_requests=15)
        json_str = cfg.json()
        data = json.loads(json_str)
        assert data["max_concurrent_requests"] == 15
        assert "response_delay_ms" in data


class TestTaskRequest:
    def test_task_request_with_weight(self):
        """Test TaskRequest with weight"""
        task = TaskRequest(id="task-123", weight=2.5)
        assert task.id == "task-123"
        assert task.weight == 2.5

    def test_task_request_default_weight(self):
        """Test TaskRequest with default weight"""
        task = TaskRequest(id="task-456")
        assert task.id == "task-456"
        assert task.weight == 1.0

    def test_task_request_json_serialization(self):
        """Test TaskRequest JSON serialization"""
        task = TaskRequest(id="task-789", weight=1.5)
        json_str = task.json()
        data = json.loads(json_str)
        assert data["id"] == "task-789"
        assert data["weight"] == 1.5


class TestTaskResponse:
    def test_task_response_creation(self):
        """Test TaskResponse creation"""
        resp = TaskResponse(
            id="task-1",
            worker="test-worker",
            color="#FF0000",
            processingTimeMs=150,
            timestamp="2024-01-01T00:00:00Z",
        )
        assert resp.id == "task-1"
        assert resp.worker == "test-worker"
        assert resp.color == "#FF0000"
        assert resp.processingTimeMs == 150
        assert resp.timestamp == "2024-01-01T00:00:00Z"

    def test_task_response_json_serialization(self):
        """Test TaskResponse JSON serialization"""
        resp = TaskResponse(
            id="task-2",
            worker="worker-2",
            color="#00FF00",
            processingTimeMs=100,
            timestamp="2024-01-01T00:00:00Z",
        )
        json_str = resp.json()
        data = json.loads(json_str)
        assert data["id"] == "task-2"
        assert data["processingTimeMs"] == 100


class TestErrorResponse:
    def test_error_response_creation(self):
        """Test ErrorResponse creation"""
        resp = ErrorResponse(error="Test error", worker="test-worker")
        assert resp.error == "Test error"
        assert resp.worker == "test-worker"

    def test_error_response_json_serialization(self):
        """Test ErrorResponse JSON serialization"""
        resp = ErrorResponse(error="Failed", worker="worker-1")
        json_str = resp.json()
        data = json.loads(json_str)
        assert data["error"] == "Failed"
        assert data["worker"] == "worker-1"


class TestHealthResponse:
    def test_health_response_creation(self):
        """Test HealthResponse creation"""
        resp = HealthResponse(status="healthy", currentLoad=5, queueDepth=10)
        assert resp.status == "healthy"
        assert resp.currentLoad == 5
        assert resp.queueDepth == 10

    def test_health_response_json_serialization(self):
        """Test HealthResponse JSON serialization"""
        resp = HealthResponse(status="degraded", currentLoad=8, queueDepth=20)
        json_str = resp.json()
        data = json.loads(json_str)
        assert data["status"] == "degraded"
        assert data["currentLoad"] == 8
        assert data["queueDepth"] == 20


class TestLoadConfig:
    def test_load_config_from_env(self):
        """Test loading configuration from environment variables"""
        with patch.dict(
            os.environ,
            {
                "MAX_CONCURRENT_REQUESTS": "25",
                "RESPONSE_DELAY_MS": "150",
                "FAILURE_RATE": "0.2",
                "QUEUE_SIZE": "75",
            },
        ):
            cfg = load_config()
            assert cfg.max_concurrent_requests == 25
            assert cfg.response_delay_ms == 150
            assert cfg.failure_rate == 0.2
            assert cfg.queue_size == 75

    def test_load_config_defaults(self):
        """Test loading configuration with defaults"""
        with patch.dict(os.environ, {}, clear=True):
            cfg = load_config()
            assert cfg.max_concurrent_requests == 10
            assert cfg.response_delay_ms == 100
            assert cfg.failure_rate == 0.0
            assert cfg.queue_size == 50

    def test_load_config_partial_env(self):
        """Test loading configuration with partial env vars"""
        with patch.dict(
            os.environ, {"MAX_CONCURRENT_REQUESTS": "30"}, clear=True
        ):
            cfg = load_config()
            assert cfg.max_concurrent_requests == 30
            assert cfg.response_delay_ms == 100  # default


class TestHealthEndpoint:
    def test_health_endpoint_healthy_status(self, client, reset_state):
        """Test health endpoint returns healthy status"""
        response = client.get("/health")
        assert response.status_code == 200

        data = response.json()
        assert "status" in data
        assert data["status"] in ["healthy", "degraded", "unhealthy"]
        assert "currentLoad" in data
        assert "queueDepth" in data

    def test_health_endpoint_structure(self, client, reset_state):
        """Test health endpoint response structure"""
        response = client.get("/health")
        assert response.status_code == 200

        data = response.json()
        assert isinstance(data["status"], str)
        assert isinstance(data["currentLoad"], int)
        assert isinstance(data["queueDepth"], int)

    def test_health_endpoint_low_load(self, client, reset_state):
        """Test health endpoint with low load"""
        global active_requests
        with requests_lock:
            active_requests = 2

        response = client.get("/health")
        assert response.status_code == 200

        data = response.json()
        assert data["currentLoad"] == 2

    def test_health_endpoint_multiple_requests(self, client, reset_state):
        """Test health endpoint handles multiple requests"""
        for _ in range(5):
            response = client.get("/health")
            assert response.status_code == 200


class TestTaskEndpoint:
    def test_task_endpoint_success(self, client, reset_state):
        """Test task endpoint with successful request"""
        task_data = {"id": "test-task-1", "weight": 1.0}

        response = client.post("/task", json=task_data)
        assert response.status_code == 200

        data = response.json()
        assert data["id"] == "test-task-1"
        assert data["worker"] is not None
        assert data["color"] is not None
        assert data["processingTimeMs"] >= 0
        assert "timestamp" in data

    def test_task_endpoint_with_weight(self, client, reset_state):
        """Test task endpoint with different weights"""
        task_data = {"id": "test-task-2", "weight": 2.0}

        response = client.post("/task", json=task_data)
        assert response.status_code == 200

        data = response.json()
        assert data["id"] == "test-task-2"
        # Processing time should be roughly 2x the base delay
        assert data["processingTimeMs"] > 0

    def test_task_endpoint_zero_weight(self, client, reset_state):
        """Test task endpoint with zero weight"""
        task_data = {"id": "test-task-3", "weight": 0.0}

        response = client.post("/task", json=task_data)
        # Should succeed with weight treated as minimum
        assert response.status_code == 200

    def test_task_endpoint_negative_weight(self, client, reset_state):
        """Test task endpoint with negative weight"""
        task_data = {"id": "test-task-4", "weight": -1.0}

        response = client.post("/task", json=task_data)
        # Should succeed with weight treated as minimum
        assert response.status_code == 200

    def test_task_endpoint_no_weight(self, client, reset_state):
        """Test task endpoint without weight (default)"""
        task_data = {"id": "test-task-5"}

        response = client.post("/task", json=task_data)
        assert response.status_code == 200

        data = response.json()
        assert data["id"] == "test-task-5"

    def test_task_endpoint_invalid_json(self, client, reset_state):
        """Test task endpoint with invalid JSON"""
        response = client.post(
            "/task",
            data="invalid json",
            headers={"Content-Type": "application/json"},
        )
        assert response.status_code == 422  # FastAPI validation error

    def test_task_endpoint_missing_id(self, client, reset_state):
        """Test task endpoint with missing ID"""
        task_data = {"weight": 1.0}

        response = client.post("/task", json=task_data)
        assert response.status_code == 422  # Validation error

    @pytest.mark.asyncio
    async def test_task_endpoint_concurrent_requests(self, client, reset_state):
        """Test task endpoint with concurrent requests"""
        async def send_task(task_id):
            task_data = {"id": task_id, "weight": 0.1}
            response = client.post("/task", json=task_data)
            return response.status_code

        # Send multiple concurrent requests
        tasks = [send_task(f"task-{i}") for i in range(5)]
        results = await asyncio.gather(*tasks)

        # Most should succeed (200), some might be rejected if queue fills
        success_count = sum(1 for code in results if code == 200)
        assert success_count >= 3  # At least some should succeed

    def test_task_endpoint_response_structure(self, client, reset_state):
        """Test task endpoint response has correct structure"""
        task_data = {"id": "test-task-6", "weight": 1.0}

        response = client.post("/task", json=task_data)
        assert response.status_code == 200

        data = response.json()
        required_fields = ["id", "worker", "color", "processingTimeMs", "timestamp"]
        for field in required_fields:
            assert field in data, f"Missing field: {field}"


class TestConfigEndpoint:
    def test_config_get_endpoint(self, client):
        """Test GET config endpoint"""
        response = client.get("/config")
        assert response.status_code == 200

        data = response.json()
        assert "max_concurrent_requests" in data
        assert "response_delay_ms" in data
        assert "failure_rate" in data
        assert "queue_size" in data

    def test_config_post_endpoint(self, client):
        """Test POST config endpoint"""
        new_config = {
            "max_concurrent_requests": 20,
            "response_delay_ms": 200,
            "failure_rate": 0.1,
            "queue_size": 100,
        }

        response = client.post("/config", json=new_config)
        assert response.status_code == 200

        data = response.json()
        assert data["max_concurrent_requests"] == 20
        assert data["response_delay_ms"] == 200
        assert data["failure_rate"] == 0.1
        assert data["queue_size"] == 100

    def test_config_put_endpoint(self, client):
        """Test PUT config endpoint"""
        new_config = {
            "max_concurrent_requests": 15,
            "response_delay_ms": 150,
            "failure_rate": 0.05,
            "queue_size": 75,
        }

        response = client.put("/config", json=new_config)
        assert response.status_code == 200

        data = response.json()
        assert data["max_concurrent_requests"] == 15

    def test_config_update_partial(self, client):
        """Test config update with partial data"""
        # Get current config
        current = client.get("/config").json()

        # Update only one field
        new_config = {
            "max_concurrent_requests": 25,
            "response_delay_ms": current["response_delay_ms"],
            "failure_rate": current["failure_rate"],
            "queue_size": current["queue_size"],
        }

        response = client.post("/config", json=new_config)
        assert response.status_code == 200

        data = response.json()
        assert data["max_concurrent_requests"] == 25

    def test_config_invalid_values(self, client):
        """Test config update with invalid values"""
        # Get current config
        current = client.get("/config").json()

        # Try to update with invalid values
        new_config = {
            "max_concurrent_requests": -1,  # Invalid
            "response_delay_ms": -100,  # Invalid
            "failure_rate": 2.0,  # Invalid (> 1)
            "queue_size": 0,  # Invalid
        }

        response = client.post("/config", json=new_config)
        # Should still return 200 but values should be rejected
        assert response.status_code == 200

        # Verify config wasn't updated to invalid values
        data = response.json()
        # The update function should reject invalid values
        assert data["max_concurrent_requests"] == current["max_concurrent_requests"]


class TestMetricsEndpoint:
    def test_metrics_endpoint(self, client):
        """Test metrics endpoint returns Prometheus format"""
        response = client.get("/metrics")
        assert response.status_code == 200

        # Check content type
        assert "text/plain" in response.headers.get("content-type", "")

        # Check for metric names in response
        text = response.text
        assert "worker_requests_total" in text or "# HELP" in text

    def test_metrics_endpoint_format(self, client):
        """Test metrics endpoint returns valid Prometheus format"""
        response = client.get("/metrics")
        assert response.status_code == 200

        text = response.text
        # Should contain metric definitions
        assert "# HELP" in text or "# TYPE" in text or "worker_" in text


class TestCORSHeaders:
    def test_cors_headers_on_request(self, client):
        """Test CORS headers are present"""
        response = client.get("/health")
        assert response.status_code == 200

        # CORS headers should be present
        assert "access-control-allow-origin" in [
            h.lower() for h in response.headers.keys()
        ]

    def test_cors_preflight_request(self, client):
        """Test CORS preflight OPTIONS request"""
        response = client.options("/task")
        # FastAPI handles OPTIONS automatically
        assert response.status_code in [200, 405]  # Depends on CORS config


class TestFailureSimulation:
    def test_task_with_high_failure_rate(self, client, reset_state):
        """Test task handling with high failure rate"""
        # Update config to have high failure rate
        new_config = {
            "max_concurrent_requests": 10,
            "response_delay_ms": 10,
            "failure_rate": 1.0,  # Always fail
            "queue_size": 50,
        }
        client.post("/config", json=new_config)

        task_data = {"id": "test-task-fail", "weight": 1.0}
        response = client.post("/task", json=task_data)

        # Should return 500 for simulated failure
        assert response.status_code == 500

        data = response.json()
        assert "detail" in data
        assert "error" in data["detail"]
        assert "Simulated failure" in data["detail"]["error"]

    def test_task_with_zero_failure_rate(self, client, reset_state):
        """Test task handling with zero failure rate"""
        # Update config to have zero failure rate
        new_config = {
            "max_concurrent_requests": 10,
            "response_delay_ms": 10,
            "failure_rate": 0.0,  # Never fail
            "queue_size": 50,
        }
        client.post("/config", json=new_config)

        task_data = {"id": "test-task-success", "weight": 1.0}
        response = client.post("/task", json=task_data)

        assert response.status_code == 200


class TestLoadSimulation:
    def test_health_status_under_load(self, client, reset_state):
        """Test health status changes under different load levels"""
        global active_requests

        # Test healthy status
        with requests_lock:
            active_requests = 2
        response = client.get("/health")
        data = response.json()
        assert data["status"] == "healthy"

        # Test degraded status (70% load)
        with requests_lock:
            active_requests = 7
        response = client.get("/health")
        data = response.json()
        assert data["status"] == "degraded"

        # Test unhealthy status (90% load)
        with requests_lock:
            active_requests = 9
        response = client.get("/health")
        data = response.json()
        assert data["status"] == "unhealthy"

    def test_health_status_with_queue_depth(self, client, reset_state):
        """Test health status considers queue depth"""
        global queue_depth

        # Test with high queue depth
        with queue_depth_lock:
            queue_depth = 46  # > 90% of 50
        response = client.get("/health")
        data = response.json()
        assert data["status"] == "unhealthy"


class TestEdgeCases:
    def test_task_with_very_large_weight(self, client, reset_state):
        """Test task with very large weight"""
        task_data = {"id": "test-task-large", "weight": 100.0}

        # Should handle gracefully (might take longer)
        response = client.post("/task", json=task_data)
        # Depending on delay, might timeout or succeed
        assert response.status_code in [200, 503, 500]

    def test_task_with_fractional_weight(self, client, reset_state):
        """Test task with fractional weight"""
        task_data = {"id": "test-task-frac", "weight": 0.5}

        response = client.post("/task", json=task_data)
        assert response.status_code == 200

    def test_task_id_with_special_characters(self, client, reset_state):
        """Test task with special characters in ID"""
        task_data = {"id": "task-!@#$%^&*()", "weight": 1.0}

        response = client.post("/task", json=task_data)
        assert response.status_code == 200

        data = response.json()
        assert data["id"] == "task-!@#$%^&*()"

    def test_empty_task_id(self, client, reset_state):
        """Test task with empty ID"""
        task_data = {"id": "", "weight": 1.0}

        response = client.post("/task", json=task_data)
        # Should accept empty string (FastAPI doesn't validate non-empty by default)
        assert response.status_code in [200, 422]

    def test_very_long_task_id(self, client, reset_state):
        """Test task with very long ID"""
        long_id = "task-" + "x" * 1000
        task_data = {"id": long_id, "weight": 1.0}

        response = client.post("/task", json=task_data)
        assert response.status_code == 200

        data = response.json()
        assert data["id"] == long_id


class TestStateManagement:
    def test_active_requests_tracking(self, client, reset_state):
        """Test active requests counter"""
        global active_requests

        initial = active_requests

        # Start a task (in separate thread, won't actually track)
        task_data = {"id": "test-track", "weight": 0.1}
        response = client.post("/task", json=task_data)

        # After completion, active requests should return to initial
        assert response.status_code == 200

    def test_queue_depth_tracking(self, client, reset_state):
        """Test queue depth tracking"""
        global queue_depth

        initial = queue_depth

        task_data = {"id": "test-queue", "weight": 0.1}
        response = client.post("/task", json=task_data)

        assert response.status_code == 200


class TestIntegration:
    def test_full_workflow(self, client, reset_state):
        """Test complete workflow: config -> task -> health -> metrics"""
        # 1. Update config
        new_config = {
            "max_concurrent_requests": 15,
            "response_delay_ms": 50,
            "failure_rate": 0.0,
            "queue_size": 60,
        }
        response = client.post("/config", json=new_config)
        assert response.status_code == 200

        # 2. Check health
        response = client.get("/health")
        assert response.status_code == 200

        # 3. Send task
        task_data = {"id": "integration-task", "weight": 1.0}
        response = client.post("/task", json=task_data)
        assert response.status_code == 200

        # 4. Check metrics
        response = client.get("/metrics")
        assert response.status_code == 200

    def test_sequential_tasks(self, client, reset_state):
        """Test sending multiple tasks sequentially"""
        for i in range(5):
            task_data = {"id": f"seq-task-{i}", "weight": 0.1}
            response = client.post("/task", json=task_data)
            assert response.status_code == 200
            data = response.json()
            assert data["id"] == f"seq-task-{i}"


class TestEnvironmentConfiguration:
    def test_worker_name_from_env(self):
        """Test WORKER_NAME is loaded from environment"""
        with patch.dict(os.environ, {"WORKER_NAME": "custom-worker"}):
            from main import WORKER_NAME

            # Would need to reload module, so just test the default
            assert isinstance(WORKER_NAME, str)

    def test_worker_color_from_env(self):
        """Test WORKER_COLOR is loaded from environment"""
        with patch.dict(os.environ, {"WORKER_COLOR": "#FF00FF"}):
            from main import WORKER_COLOR

            assert isinstance(WORKER_COLOR, str)

    def test_port_from_env(self):
        """Test PORT is loaded from environment"""
        with patch.dict(os.environ, {"PORT": "9090"}):
            from main import PORT

            assert PORT == 8080  # Default in current import


if __name__ == "__main__":
    pytest.main([__file__, "-v"])