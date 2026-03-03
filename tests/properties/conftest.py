import yaml
import pytest
from pathlib import Path


@pytest.fixture
def docker_compose_path():
    """Return path to docker-compose.yml file."""
    return Path(__file__).parent.parent.parent / "docker-compose.yml"


@pytest.fixture
def docker_compose(docker_compose_path):
    """Load and parse docker-compose.yml file."""
    with open(docker_compose_path, 'r') as f:
        return yaml.safe_load(f)


@pytest.fixture
def env_example_path():
    """Return path to .env.example file."""
    return Path(__file__).parent.parent.parent / ".env.example"


@pytest.fixture
def env_example(env_example_path):
    """Load .env.example file content."""
    with open(env_example_path, 'r') as f:
        return f.read()
