import asyncio
import json
import os
import pytest
from fastapi.testclient import TestClient
from unittest.mock import patch, MagicMock

# Import the application
from main import (
    app,
    Configuration,
    TaskRequest,
    load_config,
    config,
    active_requests,
    requests_lock,
    config_lock,
    queue_semaphore,
    queue_depth,
    queue_depth_lock,
    WORKER_NAME,
    WORKER_COLOR,
)

# Test client
client = TestClient(app)


class TestConfiguration:
    """Tests for Configuration model"""

    def test_configuration_default_values(self):
        """Test default configuration values"""
        cfg = Configuration()
        assert cfg.max_concurrent_requests == 10
        assert cfg.response_delay_ms == 100
        assert cfg.failure_rate == 0.0
        assert cfg.queue_size == 50

    def test_configuration_custom_values(self):
        """Test custom configuration values"""
        cfg = Configuration(
            max_concurrent_requests=20,
            response_delay_ms=200,
            failure_rate=0.1,
            queue_size=100,
        )
        assert cfg.max_concurrent_requests == 20
        assert cfg.response_delay_ms == 200
        assert cfg.failure_rate == 0.1
        assert cfg.queue_size == 100

    def test_configuration_validation(self):
        """Test configuration with various values"""
        cfg = Configuration(
            max_concurrent_requests=1,
            response_delay_ms=0,
            failure_rate=1.0,
            queue_size=1,
        )
        assert cfg.max_concurrent_requests == 1
        assert cfg.response_delay_ms == 0
        assert cfg.failure_rate == 1.0
        assert cfg.queue_size == 1


class TestLoadConfig:
    """Tests for load_config function"""

    def test_load_config_defaults(self):
        """Test loading default configuration"""
        with patch.dict(
            os.environ,
            {
                "MAX_CONCURRENT_REQUESTS": "",
                "RESPONSE_DELAY_MS": "",
                "FAILURE_RATE": "",
                "QUEUE_SIZE": "",
            },
            clear=False,
        ):
            cfg = load_config()
            assert cfg.max_concurrent_requests == 10
            assert cfg.response_delay_ms == 100
            assert cfg.failure_rate == 0.0
            assert cfg.queue_size == 50

    def test_load_config_from_env(self):
        """Test loading configuration from environment variables"""
        with patch.dict(
            os.environ,
            {
                "MAX_CONCURRENT_REQUESTS": "25",
                "RESPONSE_DELAY_MS": "250",
                "FAILURE_RATE": "0.15",
                "QUEUE_SIZE": "75",
            },
        ):
            cfg = load_config()
            assert cfg.max_concurrent_requests == 25
            assert cfg.response_delay_ms == 250
            assert cfg.failure_rate == 0.15
            assert cfg.queue_size == 75

    def test_load_config_partial_env(self):
        """Test loading with some env vars set"""
        with patch.dict(
            os.environ, {"MAX_CONCURRENT_REQUESTS": "15", "FAILURE_RATE": "0.05"}
        ):
            cfg = load_config()
            assert cfg.max_concurrent_requests == 15
            assert cfg.response_delay_ms == 100  # default
            assert cfg.failure_rate == 0.05
            assert cfg.queue_size == 50  # default


class TestHealthEndpoint:
    """Tests for /health endpoint"""

    def test_health_endpoint_healthy_status(self):
        """Test health endpoint returns healthy status"""
        response = client.get("/health")
        assert response.status_code == 200

        data = response.json()
        assert "status" in data
        assert "currentLoad" in data
        assert "queueDepth" in data
        assert data["status"] in ["healthy", "degraded", "unhealthy"]

    def test_health_endpoint_structure(self):
        """Test health endpoint response structure"""
        response = client.get("/health")
        data = response.json()

        assert isinstance(data["status"], str)
        assert isinstance(data["currentLoad"], int)
        assert isinstance(data["queueDepth"], int)
        assert data["currentLoad"] >= 0
        assert data["queueDepth"] >= 0


class TestConfigEndpoint:
    """Tests for /config endpoint"""

    def test_get_config(self):
        """Test getting current configuration"""
        response = client.get("/config")
        assert response.status_code == 200

        data = response.json()
        assert "max_concurrent_requests" in data
        assert "response_delay_ms" in data
        assert "failure_rate" in data
        assert "queue_size" in data

    def test_update_config_put(self):
        """Test updating configuration with PUT"""
        new_config = {
            "max_concurrent_requests": 15,
            "response_delay_ms": 150,
            "failure_rate": 0.05,
            "queue_size": 60,
        }

        response = client.put("/config", json=new_config)
        assert response.status_code == 200

        data = response.json()
        assert data["max_concurrent_requests"] == 15
        assert data["response_delay_ms"] == 150
        assert data["failure_rate"] == 0.05
        assert data["queue_size"] == 60

    def test_update_config_post(self):
        """Test updating configuration with POST"""
        new_config = {
            "max_concurrent_requests": 20,
            "response_delay_ms": 200,
            "failure_rate": 0.1,
            "queue_size": 80,
        }

        response = client.post("/config", json=new_config)
        assert response.status_code == 200

        data = response.json()
        assert data["max_concurrent_requests"] == 20

    def test_update_config_partial(self):
        """Test partial configuration update"""
        # First get current config
        response = client.get("/config")
        original = response.json()

        # Update only one field
        partial_update = {
            "max_concurrent_requests": original["max_concurrent_requests"],
            "response_delay_ms": 300,  # Only change this
            "failure_rate": original["failure_rate"],
            "queue_size": original["queue_size"],
        }

        response = client.put("/config", json=partial_update)
        assert response.status_code == 200

        data = response.json()
        assert data["response_delay_ms"] == 300


class TestTaskEndpoint:
    """Tests for /task endpoint"""

    def test_task_success(self):
        """Test successful task processing"""
        task = {"id": "test-task-1", "weight": 1.0}

        response = client.post("/task", json=task)
        assert response.status_code == 200

        data = response.json()
        assert data["id"] == "test-task-1"
        assert data["worker"] == WORKER_NAME
        assert data["color"] == WORKER_COLOR
        assert "processingTimeMs" in data
        assert "timestamp" in data
        assert data["processingTimeMs"] >= 0

    def test_task_with_different_weights(self):
        """Test tasks with different weight values"""
        weights = [0.5, 1.0, 2.0, 3.0]

        for weight in weights:
            task = {"id": f"task-weight-{weight}", "weight": weight}
            response = client.post("/task", json=task)

            assert response.status_code == 200
            data = response.json()
            assert data["id"] == f"task-weight-{weight}"

    def test_task_without_weight(self):
        """Test task with default weight"""
        task = {"id": "test-task-no-weight"}

        response = client.post("/task", json=task)
        # Should use default weight of 1.0
        assert response.status_code == 200

    def test_task_with_zero_weight(self):
        """Test task with zero weight"""
        task = {"id": "test-task-zero", "weight": 0.0}

        response = client.post("/task", json=task)
        assert response.status_code == 200

    def test_task_invalid_json(self):
        """Test task endpoint with invalid JSON"""
        response = client.post(
            "/task",
            data="invalid json",
            headers={"Content-Type": "application/json"},
        )
        assert response.status_code == 422  # FastAPI validation error

    def test_task_missing_id(self):
        """Test task without required id field"""
        task = {"weight": 1.0}

        response = client.post("/task", json=task)
        assert response.status_code == 422  # Validation error


class TestMetricsEndpoint:
    """Tests for /metrics endpoint"""

    def test_metrics_endpoint(self):
        """Test metrics endpoint returns Prometheus format"""
        response = client.get("/metrics")
        assert response.status_code == 200

        # Check content type
        content_type = response.headers.get("content-type")
        assert content_type is not None

        # Check that response contains metric data
        content = response.text
        assert len(content) > 0

        # Should contain worker metrics
        assert "worker_requests_total" in content or "# TYPE" in content


class TestCORSMiddleware:
    """Tests for CORS middleware"""

    def test_cors_headers_present(self):
        """Test CORS headers are present in response"""
        response = client.get("/health")

        # Check CORS headers
        assert response.headers.get("access-control-allow-origin") == "*"

    def test_cors_options_request(self):
        """Test OPTIONS request handling"""
        response = client.options("/health")
        assert response.status_code == 200


class TestConcurrency:
    """Tests for concurrent request handling"""

    def test_sequential_tasks(self):
        """Test processing multiple tasks sequentially"""
        task_count = 5
        success_count = 0

        for i in range(task_count):
            task = {"id": f"sequential-task-{i}", "weight": 0.1}
            response = client.post("/task", json=task)

            if response.status_code == 200:
                success_count += 1

        assert success_count == task_count

    def test_task_response_fields(self):
        """Test all required fields in task response"""
        task = {"id": "field-test", "weight": 1.0}
        response = client.post("/task", json=task)

        assert response.status_code == 200
        data = response.json()

        # Check all required fields
        required_fields = ["id", "worker", "color", "processingTimeMs", "timestamp"]
        for field in required_fields:
            assert field in data, f"Missing field: {field}"

        # Validate field types
        assert isinstance(data["id"], str)
        assert isinstance(data["worker"], str)
        assert isinstance(data["color"], str)
        assert isinstance(data["processingTimeMs"], int)
        assert isinstance(data["timestamp"], str)


class TestErrorHandling:
    """Tests for error handling"""

    def test_invalid_endpoint(self):
        """Test accessing non-existent endpoint"""
        response = client.get("/nonexistent")
        assert response.status_code == 404

    def test_method_not_allowed(self):
        """Test using wrong HTTP method"""
        # GET on task endpoint (should be POST)
        response = client.get("/task")
        assert response.status_code == 405


class TestTaskProcessing:
    """Tests for task processing logic"""

    def test_processing_time_increases_with_weight(self):
        """Test that processing time correlates with weight"""
        task_light = {"id": "light-task", "weight": 0.1}
        task_heavy = {"id": "heavy-task", "weight": 2.0}

        response_light = client.post("/task", json=task_light)
        response_heavy = client.post("/task", json=task_heavy)

        assert response_light.status_code == 200
        assert response_heavy.status_code == 200

        time_light = response_light.json()["processingTimeMs"]
        time_heavy = response_heavy.json()["processingTimeMs"]

        # Heavy task should generally take longer
        # Note: Due to timing variations, this is a loose check
        assert time_heavy >= time_light * 0.5

    def test_timestamp_format(self):
        """Test timestamp is in ISO format"""
        task = {"id": "timestamp-test", "weight": 1.0}
        response = client.post("/task", json=task)

        assert response.status_code == 200
        data = response.json()

        # Should be ISO format timestamp
        timestamp = data["timestamp"]
        assert "T" in timestamp
        assert len(timestamp) > 0


class TestWorkerIdentity:
    """Tests for worker identity"""

    def test_worker_name_in_response(self):
        """Test worker name is included in responses"""
        task = {"id": "identity-test", "weight": 1.0}
        response = client.post("/task", json=task)

        assert response.status_code == 200
        data = response.json()
        assert data["worker"] == WORKER_NAME

    def test_worker_color_in_response(self):
        """Test worker color is included in responses"""
        task = {"id": "color-test", "weight": 1.0}
        response = client.post("/task", json=task)

        assert response.status_code == 200
        data = response.json()
        assert data["color"] == WORKER_COLOR
        # Check color format (should be hex)
        assert data["color"].startswith("#")


class TestEdgeCases:
    """Tests for edge cases"""

    def test_task_with_very_large_weight(self):
        """Test task with very large weight"""
        task = {"id": "large-weight", "weight": 100.0}
        response = client.post("/task", json=task)

        # Should still process (might be slow)
        assert response.status_code in [200, 503]

    def test_task_with_negative_weight(self):
        """Test task with negative weight (should handle gracefully)"""
        task = {"id": "negative-weight", "weight": -1.0}
        response = client.post("/task", json=task)

        # Should either process or return error
        assert response.status_code in [200, 400, 422]

    def test_task_with_empty_id(self):
        """Test task with empty string ID"""
        task = {"id": "", "weight": 1.0}
        response = client.post("/task", json=task)

        # Should either accept or reject
        assert response.status_code in [200, 400, 422]

    def test_task_with_very_long_id(self):
        """Test task with very long ID"""
        long_id = "a" * 1000
        task = {"id": long_id, "weight": 1.0}
        response = client.post("/task", json=task)

        assert response.status_code == 200
        data = response.json()
        assert data["id"] == long_id


class TestConfigurationBounds:
    """Tests for configuration boundary values"""

    def test_config_with_zero_values(self):
        """Test configuration with zero values"""
        config_zero = {
            "max_concurrent_requests": 1,  # Can't be 0
            "response_delay_ms": 0,
            "failure_rate": 0.0,
            "queue_size": 1,  # Can't be 0
        }

        response = client.put("/config", json=config_zero)
        assert response.status_code == 200

    def test_config_with_boundary_failure_rate(self):
        """Test failure rate at boundaries"""
        # Test minimum (0.0)
        config_min = {
            "max_concurrent_requests": 10,
            "response_delay_ms": 100,
            "failure_rate": 0.0,
            "queue_size": 50,
        }
        response = client.put("/config", json=config_min)
        assert response.status_code == 200

        # Test maximum (1.0)
        config_max = {
            "max_concurrent_requests": 10,
            "response_delay_ms": 100,
            "failure_rate": 1.0,
            "queue_size": 50,
        }
        response = client.put("/config", json=config_max)
        assert response.status_code == 200


class TestHealthStatusCalculation:
    """Tests for health status calculation"""

    def test_health_status_values(self):
        """Test that health status is one of expected values"""
        response = client.get("/health")
        data = response.json()

        valid_statuses = ["healthy", "degraded", "unhealthy"]
        assert data["status"] in valid_statuses


class TestResponseHeaders:
    """Tests for response headers"""

    def test_content_type_json(self):
        """Test JSON endpoints return correct content type"""
        endpoints = ["/health", "/config"]

        for endpoint in endpoints:
            response = client.get(endpoint)
            content_type = response.headers.get("content-type")
            assert "application/json" in content_type

    def test_task_response_content_type(self):
        """Test task endpoint returns JSON"""
        task = {"id": "content-type-test", "weight": 1.0}
        response = client.post("/task", json=task)

        content_type = response.headers.get("content-type")
        assert "application/json" in content_type


class TestSimulatedFailures:
    """Tests for simulated failure handling"""

    def test_task_with_high_failure_rate(self):
        """Test task processing with high failure rate"""
        # Update config to have high failure rate
        high_fail_config = {
            "max_concurrent_requests": 10,
            "response_delay_ms": 10,
            "failure_rate": 0.99,  # Very high failure rate
            "queue_size": 50,
        }

        client.put("/config", json=high_fail_config)

        # Try multiple tasks, most should fail
        failure_count = 0
        attempts = 10

        for i in range(attempts):
            task = {"id": f"fail-test-{i}", "weight": 0.1}
            response = client.post("/task", json=task)

            if response.status_code == 500:
                failure_count += 1

        # Most should fail due to high failure rate
        assert failure_count > 0

        # Reset config
        reset_config = {
            "max_concurrent_requests": 10,
            "response_delay_ms": 100,
            "failure_rate": 0.0,
            "queue_size": 50,
        }
        client.put("/config", json=reset_config)


# Additional regression test
class TestRegression:
    """Regression tests for specific issues"""

    def test_multiple_rapid_requests(self):
        """Test handling multiple rapid requests"""
        results = []
        for i in range(10):
            task = {"id": f"rapid-{i}", "weight": 0.1}
            response = client.post("/task", json=task)
            results.append(response.status_code)

        # All should succeed or gracefully reject
        for code in results:
            assert code in [200, 503]

    def test_config_persistence(self):
        """Test configuration changes persist"""
        # Set a specific config
        test_config = {
            "max_concurrent_requests": 33,
            "response_delay_ms": 333,
            "failure_rate": 0.33,
            "queue_size": 33,
        }

        client.put("/config", json=test_config)

        # Read it back
        response = client.get("/config")
        data = response.json()

        assert data["max_concurrent_requests"] == 33
        assert data["response_delay_ms"] == 333
        assert abs(data["failure_rate"] - 0.33) < 0.01
        assert data["queue_size"] == 33


if __name__ == "__main__":
    pytest.main([__file__, "-v"])