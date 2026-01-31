"""
Integration tests for Docker Compose configuration
Tests validate the docker-compose.yml structure and service definitions
"""

import yaml
import pytest
from pathlib import Path


class TestDockerComposeConfiguration:
    """Tests for docker-compose.yml configuration"""

    @pytest.fixture
    def compose_config(self):
        """Load docker-compose.yml configuration"""
        compose_file = Path(__file__).parent.parent / "docker-compose.yml"
        with open(compose_file, "r") as f:
            return yaml.safe_load(f)

    def test_compose_file_exists(self):
        """Test that docker-compose.yml exists"""
        compose_file = Path(__file__).parent.parent / "docker-compose.yml"
        assert compose_file.exists(), "docker-compose.yml not found"

    def test_all_services_defined(self, compose_config):
        """Test that all required services are defined"""
        services = compose_config.get("services", {})

        required_services = [
            "load-balancer",
            "worker-go-1",
            "worker-go-2",
            "worker-rust-1",
            "worker-rust-2",
            "worker-python-1",
            "worker-python-2",
            "client",
            "redis",
            "prometheus",
            "grafana",
        ]

        for service in required_services:
            assert service in services, f"Service {service} not found in compose file"

    def test_load_balancer_configuration(self, compose_config):
        """Test load balancer service configuration"""
        lb = compose_config["services"]["load-balancer"]

        assert "build" in lb, "Load balancer should have build configuration"
        assert lb["build"] == "./load-balancer"

        assert "ports" in lb, "Load balancer should expose ports"
        assert "8000:8000" in lb["ports"]

        assert "environment" in lb, "Load balancer should have environment variables"
        env = lb["environment"]
        assert any("PORT=8000" in str(e) for e in env)
        assert any("LB_ALGORITHM" in str(e) for e in env)

        assert "networks" in lb, "Load balancer should be on a network"
        assert "sandbox-network" in lb["networks"]

    def test_worker_go_configuration(self, compose_config):
        """Test Go worker service configuration"""
        worker = compose_config["services"]["worker-go-1"]

        assert worker["build"] == "./workers/go"

        env = worker["environment"]
        assert any("PORT=8080" in str(e) for e in env)
        assert any("WORKER_NAME=go-worker-1" in str(e) for e in env)
        assert any("WORKER_COLOR" in str(e) for e in env)
        assert any("MAX_CONCURRENT_REQUESTS" in str(e) for e in env)

        assert "sandbox-network" in worker["networks"]

    def test_worker_python_configuration(self, compose_config):
        """Test Python worker service configuration"""
        worker = compose_config["services"]["worker-python-1"]

        assert worker["build"] == "./workers/python"

        env = worker["environment"]
        assert any("PORT=8080" in str(e) for e in env)
        assert any("WORKER_NAME=python-worker-1" in str(e) for e in env)
        assert any("WORKER_COLOR" in str(e) for e in env)

    def test_client_configuration(self, compose_config):
        """Test client service configuration"""
        client = compose_config["services"]["client"]

        assert client["build"] == "./client"
        assert "3000:80" in client["ports"]
        assert "depends_on" in client
        assert "load-balancer" in client["depends_on"]

    def test_prometheus_configuration(self, compose_config):
        """Test Prometheus service configuration"""
        prometheus = compose_config["services"]["prometheus"]

        assert "image" in prometheus
        assert "prom/prometheus" in prometheus["image"]

        assert "volumes" in prometheus
        volumes = prometheus["volumes"]
        assert any("prometheus.yml" in str(v) for v in volumes)

        assert "9090:9090" in prometheus["ports"]

    def test_grafana_configuration(self, compose_config):
        """Test Grafana service configuration"""
        grafana = compose_config["services"]["grafana"]

        assert "image" in grafana
        assert "grafana/grafana" in grafana["image"]

        assert "volumes" in grafana
        assert "3001:3000" in grafana["ports"]

        env = grafana["environment"]
        assert any("GF_SECURITY_ADMIN_USER" in str(e) for e in env)
        assert any("GF_SECURITY_ADMIN_PASSWORD" in str(e) for e in env)

    def test_redis_configuration(self, compose_config):
        """Test Redis service configuration"""
        redis = compose_config["services"]["redis"]

        assert "image" in redis
        assert "redis" in redis["image"]
        assert "6379:6379" in redis["ports"]

    def test_network_configuration(self, compose_config):
        """Test network configuration"""
        assert "networks" in compose_config
        networks = compose_config["networks"]

        assert "sandbox-network" in networks
        assert networks["sandbox-network"]["driver"] == "bridge"

    def test_volume_configuration(self, compose_config):
        """Test volume configuration"""
        assert "volumes" in compose_config
        volumes = compose_config["volumes"]

        assert "prometheus-data" in volumes
        assert "grafana-data" in volumes

    def test_restart_policies(self, compose_config):
        """Test that services have restart policies"""
        services = compose_config["services"]

        critical_services = [
            "load-balancer",
            "worker-go-1",
            "worker-python-1",
            "client",
        ]

        for service_name in critical_services:
            service = services[service_name]
            assert "restart" in service, f"{service_name} should have restart policy"
            assert service["restart"] == "unless-stopped"

    def test_worker_environment_variables(self, compose_config):
        """Test that all workers have required environment variables"""
        worker_services = [
            "worker-go-1",
            "worker-go-2",
            "worker-rust-1",
            "worker-rust-2",
            "worker-python-1",
            "worker-python-2",
        ]

        required_env_vars = [
            "PORT",
            "WORKER_NAME",
            "WORKER_COLOR",
            "MAX_CONCURRENT_REQUESTS",
        ]

        for worker_name in worker_services:
            worker = compose_config["services"][worker_name]
            env = worker.get("environment", [])

            for var in required_env_vars:
                assert any(
                    var in str(e) for e in env
                ), f"{worker_name} missing {var} environment variable"

    def test_load_balancer_dependencies(self, compose_config):
        """Test load balancer depends on all workers"""
        lb = compose_config["services"]["load-balancer"]

        assert "depends_on" in lb
        depends = lb["depends_on"]

        expected_dependencies = [
            "worker-go-1",
            "worker-go-2",
            "worker-rust-1",
            "worker-rust-2",
            "worker-python-1",
            "worker-python-2",
        ]

        for dep in expected_dependencies:
            assert dep in depends, f"Load balancer should depend on {dep}"

    def test_worker_colors_are_unique(self, compose_config):
        """Test that each worker has a unique color"""
        colors = []
        worker_services = [k for k in compose_config["services"] if "worker" in k]

        for worker_name in worker_services:
            worker = compose_config["services"][worker_name]
            env = worker.get("environment", [])

            for e in env:
                if "WORKER_COLOR" in str(e):
                    color = str(e).split("=")[1]
                    colors.append(color)

        # Check for uniqueness
        assert len(colors) == len(set(colors)), "Worker colors should be unique"

    def test_port_mapping_no_conflicts(self, compose_config):
        """Test that port mappings don't conflict"""
        exposed_ports = []

        for service_name, service in compose_config["services"].items():
            if "ports" in service:
                for port_mapping in service["ports"]:
                    # Extract host port (before the colon)
                    host_port = port_mapping.split(":")[0]
                    exposed_ports.append((service_name, host_port))

        # Check for duplicate host ports
        host_ports = [p[1] for p in exposed_ports]
        duplicates = [p for p in host_ports if host_ports.count(p) > 1]

        assert len(duplicates) == 0, f"Duplicate port mappings found: {duplicates}"


class TestDockerComposeOverrideExample:
    """Tests for docker-compose.override.yml.example"""

    @pytest.fixture
    def override_example(self):
        """Load docker-compose.override.yml.example"""
        override_file = (
            Path(__file__).parent.parent / "docker-compose.override.yml.example"
        )
        with open(override_file, "r") as f:
            content = f.read()
        return content

    def test_override_example_exists(self):
        """Test that override example file exists"""
        override_file = (
            Path(__file__).parent.parent / "docker-compose.override.yml.example"
        )
        assert override_file.exists(), "docker-compose.override.yml.example not found"

    def test_override_example_has_documentation(self, override_example):
        """Test that override example has usage documentation"""
        assert "Usage:" in override_example
        assert "docker-compose up" in override_example

    def test_override_example_shows_customization(self, override_example):
        """Test that override example shows various customization options"""
        # Should show how to replace services
        assert "load-balancer:" in override_example or "Load Balancer" in override_example

        # Should show environment variable customization
        assert "environment:" in override_example

        # Should show examples of adding new services
        assert "custom" in override_example.lower() or "example" in override_example.lower()


class TestEnvironmentExample:
    """Tests for .env.example file"""

    @pytest.fixture
    def env_example(self):
        """Load .env.example file"""
        env_file = Path(__file__).parent.parent / ".env.example"
        with open(env_file, "r") as f:
            return f.read()

    def test_env_example_exists(self):
        """Test that .env.example file exists"""
        env_file = Path(__file__).parent.parent / ".env.example"
        assert env_file.exists(), ".env.example file not found"

    def test_env_example_has_lb_config(self, env_example):
        """Test that .env.example has load balancer configuration"""
        assert "LB_IMAGE" in env_example
        assert "LB_ALGORITHM" in env_example
        assert "LB_HEALTH_CHECK_SEC" in env_example

    def test_env_example_has_worker_config(self, env_example):
        """Test that .env.example has worker configuration"""
        assert "WORKER_GO_IMAGE" in env_example
        assert "WORKER_PYTHON_IMAGE" in env_example
        assert "ENABLE_GO_WORKERS" in env_example
        assert "GO_WORKER_REPLICAS" in env_example

    def test_env_example_has_monitoring_config(self, env_example):
        """Test that .env.example has monitoring configuration"""
        assert "PROMETHEUS_PORT" in env_example
        assert "GRAFANA_PORT" in env_example
        assert "GRAFANA_ADMIN_PASSWORD" in env_example

    def test_env_example_has_documentation(self, env_example):
        """Test that .env.example has documentation comments"""
        assert "#" in env_example
        assert "Configuration" in env_example


class TestPrometheusConfiguration:
    """Tests for Prometheus configuration"""

    @pytest.fixture
    def prometheus_config(self):
        """Load prometheus.yml configuration"""
        prometheus_file = Path(__file__).parent.parent / "prometheus" / "prometheus.yml"
        with open(prometheus_file, "r") as f:
            return yaml.safe_load(f)

    def test_prometheus_config_exists(self):
        """Test that prometheus.yml exists"""
        prometheus_file = Path(__file__).parent.parent / "prometheus" / "prometheus.yml"
        assert prometheus_file.exists(), "prometheus.yml not found"

    def test_prometheus_scrape_config(self, prometheus_config):
        """Test Prometheus scrape configurations"""
        assert "scrape_configs" in prometheus_config

        scrape_configs = prometheus_config["scrape_configs"]
        job_names = [job["job_name"] for job in scrape_configs]

        assert "prometheus" in job_names
        assert "load-balancer" in job_names
        assert "workers" in job_names

    def test_prometheus_worker_targets(self, prometheus_config):
        """Test that all worker targets are configured"""
        scrape_configs = prometheus_config["scrape_configs"]

        workers_job = next(
            (job for job in scrape_configs if job["job_name"] == "workers"), None
        )
        assert workers_job is not None, "Workers job not found"

        targets = workers_job["static_configs"][0]["targets"]

        expected_workers = [
            "worker-go-1:8080",
            "worker-go-2:8080",
            "worker-rust-1:8080",
            "worker-rust-2:8080",
            "worker-python-1:8080",
            "worker-python-2:8080",
        ]

        for worker in expected_workers:
            assert worker in targets, f"Worker {worker} not in Prometheus targets"

    def test_prometheus_scrape_interval(self, prometheus_config):
        """Test Prometheus scrape interval is configured"""
        assert "global" in prometheus_config
        assert "scrape_interval" in prometheus_config["global"]

        interval = prometheus_config["global"]["scrape_interval"]
        # Should be in seconds format like "5s"
        assert "s" in interval


class TestGrafanaConfiguration:
    """Tests for Grafana configuration"""

    def test_grafana_datasource_exists(self):
        """Test Grafana datasource configuration exists"""
        datasource_file = (
            Path(__file__).parent.parent
            / "grafana"
            / "provisioning"
            / "datasources"
            / "prometheus.yml"
        )
        assert datasource_file.exists(), "Grafana datasource config not found"

    def test_grafana_dashboard_config_exists(self):
        """Test Grafana dashboard configuration exists"""
        dashboard_config = (
            Path(__file__).parent.parent
            / "grafana"
            / "provisioning"
            / "dashboards"
            / "default.yml"
        )
        assert dashboard_config.exists(), "Grafana dashboard config not found"

    def test_grafana_dashboard_json_exists(self):
        """Test Grafana dashboard JSON exists"""
        dashboard_json = (
            Path(__file__).parent.parent
            / "grafana"
            / "provisioning"
            / "dashboards"
            / "network-sandbox.json"
        )
        assert dashboard_json.exists(), "Grafana dashboard JSON not found"

    def test_grafana_datasource_config(self):
        """Test Grafana datasource configuration"""
        datasource_file = (
            Path(__file__).parent.parent
            / "grafana"
            / "provisioning"
            / "datasources"
            / "prometheus.yml"
        )

        with open(datasource_file, "r") as f:
            config = yaml.safe_load(f)

        assert "datasources" in config
        datasources = config["datasources"]

        prometheus_ds = next(
            (ds for ds in datasources if ds["type"] == "prometheus"), None
        )
        assert prometheus_ds is not None, "Prometheus datasource not configured"

        assert prometheus_ds["name"] == "Prometheus"
        assert "prometheus:9090" in prometheus_ds["url"]
        assert prometheus_ds["isDefault"] is True

    def test_grafana_dashboard_structure(self):
        """Test Grafana dashboard JSON structure"""
        dashboard_json = (
            Path(__file__).parent.parent
            / "grafana"
            / "provisioning"
            / "dashboards"
            / "network-sandbox.json"
        )

        with open(dashboard_json, "r") as f:
            dashboard = yaml.safe_load(f)

        assert "title" in dashboard
        assert "panels" in dashboard
        assert len(dashboard["panels"]) > 0, "Dashboard should have panels"

        # Check for expected panels
        panel_titles = [p.get("title", "") for p in dashboard["panels"]]
        assert any("Request" in title for title in panel_titles)
        assert any("Worker" in title for title in panel_titles)


class TestDockerfileConfigurations:
    """Tests for Dockerfile configurations"""

    def test_load_balancer_dockerfile_exists(self):
        """Test load balancer Dockerfile exists"""
        dockerfile = Path(__file__).parent.parent / "load-balancer" / "Dockerfile"
        assert dockerfile.exists(), "Load balancer Dockerfile not found"

    def test_worker_go_dockerfile_exists(self):
        """Test Go worker Dockerfile exists"""
        dockerfile = Path(__file__).parent.parent / "workers" / "go" / "Dockerfile"
        assert dockerfile.exists(), "Go worker Dockerfile not found"

    def test_worker_python_dockerfile_exists(self):
        """Test Python worker Dockerfile exists"""
        dockerfile = Path(__file__).parent.parent / "workers" / "python" / "Dockerfile"
        assert dockerfile.exists(), "Python worker Dockerfile not found"

    def test_client_dockerfile_exists(self):
        """Test client Dockerfile exists"""
        dockerfile = Path(__file__).parent.parent / "client" / "Dockerfile"
        assert dockerfile.exists(), "Client Dockerfile not found"

    def test_dockerfiles_use_multistage_build(self):
        """Test that Dockerfiles use multi-stage builds where appropriate"""
        go_dockerfile = Path(__file__).parent.parent / "load-balancer" / "Dockerfile"

        with open(go_dockerfile, "r") as f:
            content = f.read()

        # Go services should use multi-stage builds
        assert "AS builder" in content, "Go Dockerfile should use multi-stage build"
        assert "FROM alpine" in content or "FROM scratch" in content


class TestNginxConfiguration:
    """Tests for Nginx configuration"""

    def test_nginx_config_exists(self):
        """Test nginx.conf exists"""
        nginx_conf = Path(__file__).parent.parent / "client" / "nginx.conf"
        assert nginx_conf.exists(), "nginx.conf not found"

    def test_nginx_config_proxies_api(self):
        """Test nginx configuration proxies API requests"""
        nginx_conf = Path(__file__).parent.parent / "client" / "nginx.conf"

        with open(nginx_conf, "r") as f:
            content = f.read()

        assert "location /api/" in content
        assert "proxy_pass" in content
        assert "load-balancer:8000" in content

    def test_nginx_config_handles_websocket(self):
        """Test nginx configuration handles WebSocket"""
        nginx_conf = Path(__file__).parent.parent / "client" / "nginx.conf"

        with open(nginx_conf, "r") as f:
            content = f.read()

        assert "location /ws" in content
        assert "Upgrade" in content or "upgrade" in content
        assert "Connection" in content or "connection" in content


if __name__ == "__main__":
    pytest.main([__file__, "-v"])