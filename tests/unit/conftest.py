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
def caddyfile_path():
    """Return path to Caddyfile."""
    return Path(__file__).parent.parent.parent / "Caddyfile"


@pytest.fixture
def readme_path():
    """Return path to README.md."""
    return Path(__file__).parent.parent.parent / "README.md"


@pytest.fixture
def gitignore_path():
    """Return path to .gitignore."""
    return Path(__file__).parent.parent.parent / ".gitignore"
