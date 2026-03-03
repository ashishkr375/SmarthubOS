import pytest
import subprocess
import time
from pathlib import Path


@pytest.fixture(scope="session")
def docker_compose_file():
    """Return path to docker-compose.yml."""
    return Path(__file__).parent.parent.parent / "docker-compose.yml"


@pytest.fixture(scope="session")
def docker_compose_project():
    """Start docker compose stack for integration tests."""
    # Note: Integration tests require Docker and Docker Compose to be installed
    # and the .env file to be configured with valid values
    yield "cloud-infra"


def wait_for_healthy(service_name, timeout=120):
    """Wait for a service to become healthy."""
    start_time = time.time()
    while time.time() - start_time < timeout:
        try:
            result = subprocess.run(
                ["docker", "compose", "ps", "--format", "json", service_name],
                capture_output=True,
                text=True,
                check=True
            )
            if "healthy" in result.stdout.lower():
                return True
        except subprocess.CalledProcessError:
            pass
        time.sleep(2)
    return False
