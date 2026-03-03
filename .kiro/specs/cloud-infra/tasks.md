# Implementation Plan: cloud-infra

## Overview

This plan implements a Docker Compose infrastructure stack with PostgreSQL 16 + TimescaleDB, Grafana OSS, and Caddy reverse proxy with automatic TLS. The implementation follows a bottom-up approach: first creating core configuration files, then adding property-based tests to validate universal correctness properties, followed by unit tests for specific configurations, and finally integration tests for runtime behavior.

## Tasks

- [ ] 1. Create Docker Compose configuration and supporting files
  - [x] 1.1 Create docker-compose.yml with all three services
    - Define postgres service with timescale/timescaledb:latest-pg16 image
    - Define grafana service with grafana/grafana-oss:latest image
    - Define caddy service with caddy:latest image
    - Configure environment variables using ${VAR_NAME} references
    - Set up named volumes (postgres-data, grafana-data, caddy-data, caddy-config)
    - Create smarthub-network bridge network
    - Configure health checks for all services (30s interval, 5s timeout, 3 retries)
    - Set restart policy to unless-stopped for all services
    - Configure service dependencies with health check conditions
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 2.3, 2.4, 2.5, 3.1, 3.2, 3.4, 4.1, 4.2, 4.7, 6.1, 6.2, 6.3, 6.5, 7.1, 7.2, 7.3, 7.4, 7.5, 8.1, 8.2, 8.3, 8.4, 8.5, 9.1, 9.2, 9.3_

  - [x] 1.2 Create Caddyfile for reverse proxy configuration
    - Configure ACME email using environment variable
    - Define reverse proxy rule for Grafana domain
    - Set up automatic HTTPS with Let's Encrypt
    - _Requirements: 4.1, 4.3, 4.4, 4.5, 4.6, 4.9_

  - [x] 1.3 Create .env.example template file
    - Document all required environment variables with descriptions
    - Include PostgreSQL configuration (POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD)
    - Include Grafana configuration (GRAFANA_ADMIN_USER, GRAFANA_ADMIN_PASSWORD, GRAFANA_DOMAIN)
    - Include Caddy configuration (ACME_EMAIL)
    - Add security notes about password generation
    - _Requirements: 5.2, 5.3, 5.4, 5.6, 10.2, 10.3_

  - [x] 1.4 Create .gitignore file
    - Exclude .env file from version control
    - Exclude any local development files
    - _Requirements: 5.5_

- [ ] 2. Implement property-based tests for configuration validation
  - [x] 2.1 Set up Python testing infrastructure
    - Create tests/properties/ directory structure
    - Create requirements.txt with pytest, hypothesis, pyyaml dependencies
    - Create conftest.py with shared fixtures for loading docker-compose.yml
    - _Requirements: All (testing foundation)_

  - [ ]* 2.2 Write property test for volume persistence completeness
    - **Property 1: Volume Persistence Completeness**
    - **Validates: Requirements 2.3, 3.2, 4.7, 6.1, 6.2, 6.3**
    - Verify all data-managing services (postgres, grafana, caddy) have volume mounts

  - [ ]* 2.3 Write property test for network isolation
    - **Property 2: Network Isolation for Internal Services**
    - **Validates: Requirements 2.5, 3.4, 8.2, 8.3**
    - Verify internal services (postgres, grafana) have no host port mappings

  - [ ]* 2.4 Write property test for volume naming convention
    - **Property 3: Volume Naming Convention**
    - **Validates: Requirements 6.5**
    - Verify all volume names contain service identifiers

  - [ ]* 2.5 Write property test for health check interval consistency
    - **Property 4: Health Check Interval Consistency**
    - **Validates: Requirements 7.5**
    - Verify all health checks use 30-second intervals

  - [ ]* 2.6 Write property test for health check retry consistency
    - **Property 5: Health Check Retry Consistency**
    - **Validates: Requirements 7.4**
    - Verify all health checks use exactly 3 retries

  - [ ]* 2.7 Write property test for no hardcoded secrets
    - **Property 6: No Hardcoded Secrets**
    - **Validates: Requirements 5.1**
    - Verify credential-related environment variables use ${VAR_NAME} references

  - [ ]* 2.8 Write property test for environment variable documentation
    - **Property 7: Required Environment Variables Documented**
    - **Validates: Requirements 10.3**
    - Verify all ${VAR_NAME} references in docker-compose.yml appear in .env.example

  - [ ]* 2.9 Write property test for service dependency validity
    - **Property 8: Service Dependency Chain Validity**
    - **Validates: Requirements 9.1, 9.2, 9.3, 9.4**
    - Verify all depends_on references point to existing services

- [x] 3. Checkpoint - Run property tests
  - Run pytest on property tests to validate configuration correctness
  - Ensure all property tests pass, ask the user if questions arise

- [ ] 4. Implement unit tests for specific configurations
  - [x] 4.1 Create tests/unit/ directory and test files
    - Create test_docker_compose.py for service-specific tests
    - Create test_files.py for file existence tests
    - Create test_documentation.py for README validation
    - _Requirements: All (testing foundation)_

  - [ ]* 4.2 Write unit tests for PostgreSQL service configuration
    - Test postgres service uses timescale/timescaledb:latest-pg16 image
    - Test postgres health check command is pg_isready
    - Test postgres has postgres-data volume mounted to /var/lib/postgresql/data
    - Test postgres has no port mappings to host
    - _Requirements: 2.1, 2.2, 2.3, 2.5, 2.6, 7.1_

  - [ ]* 4.3 Write unit tests for Grafana service configuration
    - Test grafana service uses grafana/grafana-oss:latest image
    - Test grafana health check uses /api/health endpoint
    - Test grafana has grafana-data volume mounted to /var/lib/grafana
    - Test grafana depends on postgres with service_healthy condition
    - Test grafana has no port mappings to host
    - _Requirements: 3.1, 3.2, 3.4, 3.5, 7.2, 9.1, 9.3_

  - [ ]* 4.4 Write unit tests for Caddy service configuration
    - Test caddy service uses caddy:latest image
    - Test caddy exposes ports 80 and 443 to host
    - Test caddy has caddy-data and caddy-config volumes
    - Test caddy mounts Caddyfile from host
    - Test caddy depends on grafana with health condition
    - Test caddy health check uses /metrics endpoint
    - _Requirements: 4.1, 4.2, 4.7, 7.3, 8.4, 9.2, 9.4_

  - [ ]* 4.5 Write unit tests for network configuration
    - Test smarthub-network exists with bridge driver
    - Test all services are connected to smarthub-network
    - _Requirements: 1.2, 8.1, 8.5_

  - [ ]* 4.6 Write unit tests for file existence
    - Test docker-compose.yml exists
    - Test Caddyfile exists
    - Test .env.example exists
    - Test .gitignore exists and excludes .env
    - _Requirements: 5.5, 5.6, 10.2_

- [ ] 5. Create README documentation
  - [x] 5.1 Write README.md with comprehensive documentation
    - Add overview section describing the infrastructure stack
    - Document prerequisites (Docker, Docker Compose, domain with DNS configured)
    - Provide step-by-step deployment instructions
    - Document environment variable configuration process
    - Include verification steps to confirm successful deployment
    - Document backup and restore procedures for persistent volumes
    - Add troubleshooting section for common issues
    - Include security best practices for production deployment
    - _Requirements: 10.1, 10.3, 10.4, 10.5, 10.6_

  - [ ]* 5.2 Write unit tests for README documentation
    - Test README.md exists
    - Test README contains deployment instructions section
    - Test README contains environment variables section
    - Test README contains verification steps section
    - Test README contains backup/restore procedures section
    - _Requirements: 10.1, 10.3, 10.4, 10.5, 10.6_

- [x] 6. Checkpoint - Run all unit tests
  - Run pytest on unit tests to validate specific configurations
  - Ensure all unit tests pass, ask the user if questions arise

- [ ] 7. Implement integration tests for runtime behavior
  - [x] 7.1 Create tests/integration/ directory and test infrastructure
    - Create test_deployment.py for deployment tests
    - Create test_connectivity.py for service connectivity tests
    - Create test_persistence.py for data persistence tests
    - Create conftest.py with Docker Compose fixtures
    - _Requirements: All (integration testing foundation)_

  - [ ]* 7.2 Write integration tests for stack deployment
    - Test docker compose up starts all containers successfully
    - Test all containers reach healthy state within timeout
    - Test no containers restart unexpectedly after startup
    - _Requirements: 1.3, 1.5, 7.4_

  - [ ]* 7.3 Write integration tests for service connectivity
    - Test Grafana can connect to PostgreSQL
    - Test Caddy can proxy requests to Grafana
    - Test HTTP requests redirect to HTTPS
    - _Requirements: 2.7, 3.5, 3.6, 4.3, 4.6, 8.5_

  - [ ]* 7.4 Write integration tests for data persistence
    - Test PostgreSQL data persists after container restart
    - Test Grafana dashboards persist after container restart
    - _Requirements: 6.4_

  - [ ]* 7.5 Write integration tests for health check behavior
    - Test health checks report healthy status for running services
    - Test containers restart automatically after health check failures
    - _Requirements: 1.5, 2.6, 7.1, 7.2, 7.3, 7.4, 7.5_

- [ ] 8. Final checkpoint - Run full test suite
  - Run all property tests, unit tests, and integration tests
  - Verify all tests pass and configuration is production-ready
  - Ensure all tests pass, ask the user if questions arise

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Property tests validate universal correctness across all configuration elements
- Unit tests validate specific service configurations and file contents
- Integration tests require Docker and Docker Compose to be installed
- Integration tests will start actual containers and may take several minutes
- Each task references specific requirements for traceability
- The implementation uses Python with pytest and hypothesis for all testing
