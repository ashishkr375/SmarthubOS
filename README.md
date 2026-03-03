# SmartHub Cloud Infrastructure

Docker Compose infrastructure stack for the SmartHub IoT platform, providing PostgreSQL 16 with TimescaleDB, Grafana OSS for monitoring, and Caddy reverse proxy with automatic TLS certificate management.

## Overview

This infrastructure provides the foundational services for the SmartHub IoT platform:

- **PostgreSQL 16 + TimescaleDB**: Relational database with time-series optimization for telemetry data
- **Grafana OSS**: Monitoring and visualization dashboards
- **Caddy**: Reverse proxy with automatic HTTPS certificate management via Let's Encrypt

All services run in Docker containers, communicate over a private network, and persist data across restarts.

## Prerequisites

Before deploying, ensure you have:

- **Docker** (version 20.10 or later)
- **Docker Compose** (version 2.0 or later)
- **Domain name** with DNS configured to point to your server's IP address
- **Ports 80 and 443** open on your firewall (required for Let's Encrypt ACME challenge and HTTPS)
- **Sufficient disk space** for database and monitoring data (recommended: 20GB minimum)

## Quick Start

1. **Clone the repository** (or copy the files to your server)

2. **Create environment configuration**:
   ```bash
   cp .env.example .env
   ```

3. **Edit `.env` file** with your configuration:
   ```bash
   nano .env
   ```
   
   Update the following variables:
   - `POSTGRES_PASSWORD`: Generate a secure password (minimum 16 characters)
   - `GRAFANA_ADMIN_PASSWORD`: Generate a secure password (minimum 16 characters)
   - `GRAFANA_DOMAIN`: Your domain name (e.g., grafana.yourdomain.com)
   - `ACME_EMAIL`: Your email for Let's Encrypt notifications

   Generate secure passwords using:
   ```bash
   openssl rand -base64 24
   ```

4. **Set file permissions** (security best practice):
   ```bash
   chmod 600 .env
   ```

5. **Deploy the stack**:
   ```bash
   docker compose up -d
   ```

6. **Monitor startup**:
   ```bash
   docker compose logs -f
   ```

   Wait for all services to become healthy (typically 30-60 seconds).

## Environment Variables

All configuration is managed through the `.env` file. Never commit this file to version control.

### Required Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `POSTGRES_DB` | PostgreSQL database name | `smarthub` |
| `POSTGRES_USER` | PostgreSQL superuser username | `smarthub_admin` |
| `POSTGRES_PASSWORD` | PostgreSQL superuser password | `<secure-random-password>` |
| `GRAFANA_ADMIN_USER` | Grafana admin username | `admin` |
| `GRAFANA_ADMIN_PASSWORD` | Grafana admin password | `<secure-random-password>` |
| `GRAFANA_DOMAIN` | Domain for Grafana (must have DNS configured) | `grafana.example.com` |
| `ACME_EMAIL` | Email for Let's Encrypt certificate notifications | `admin@example.com` |

### Security Notes

- Use cryptographically secure random generators for passwords
- Minimum password length: 16 characters
- Rotate passwords regularly in production
- Set `.env` file permissions to 600 (owner read/write only)

## Deployment Process

### Step-by-Step Deployment

1. **Verify prerequisites**:
   ```bash
   docker --version
   docker compose version
   ```

2. **Configure environment**:
   - Copy `.env.example` to `.env`
   - Update all variables with your values
   - Verify domain DNS points to your server

3. **Start services**:
   ```bash
   docker compose up -d
   ```

4. **Check service health**:
   ```bash
   docker compose ps
   ```
   
   All services should show status as "healthy".

5. **View logs** (if needed):
   ```bash
   # All services
   docker compose logs -f
   
   # Specific service
   docker compose logs -f postgres
   docker compose logs -f grafana
   docker compose logs -f caddy
   ```

### Service Startup Order

Services start in dependency order:
1. PostgreSQL starts first and initializes TimescaleDB
2. Grafana starts after PostgreSQL is healthy
3. Caddy starts after Grafana is healthy and begins TLS certificate acquisition

## Verification Steps

After deployment, verify each component:

### 1. Check Container Status

```bash
docker compose ps
```

Expected output: All services should show "Up" and "healthy" status.

### 2. Verify PostgreSQL

```bash
docker compose exec postgres pg_isready -U smarthub_admin
```

Expected output: `postgres:5432 - accepting connections`

### 3. Verify Grafana

```bash
curl -k http://localhost:3000/api/health
```

Expected output: `{"database":"ok","version":"..."}`

### 4. Verify Caddy and HTTPS

Open your browser and navigate to: `https://grafana.yourdomain.com`

You should see:
- Valid HTTPS certificate (green padlock)
- Grafana login page
- HTTP requests automatically redirect to HTTPS

### 5. Login to Grafana

- URL: `https://grafana.yourdomain.com`
- Username: Value of `GRAFANA_ADMIN_USER` from `.env`
- Password: Value of `GRAFANA_ADMIN_PASSWORD` from `.env`

### 6. Configure PostgreSQL Data Source (First Time Only)

After logging into Grafana:

1. Navigate to **Configuration** → **Data Sources**
2. Click **Add data source**
3. Select **PostgreSQL**
4. Configure:
   - **Host**: `postgres:5432`
   - **Database**: Value of `POSTGRES_DB` from `.env`
   - **User**: Value of `POSTGRES_USER` from `.env`
   - **Password**: Value of `POSTGRES_PASSWORD` from `.env`
   - **TLS/SSL Mode**: `disable` (internal network)
   - **TimescaleDB**: Enable
5. Click **Save & Test**

## Backup and Restore Procedures

### Backup

#### PostgreSQL Database Backup

```bash
# Create backup directory
mkdir -p backups

# Backup database
docker compose exec -T postgres pg_dump -U smarthub_admin smarthub > backups/postgres_$(date +%Y%m%d_%H%M%S).sql
```

#### Grafana Dashboards Backup

```bash
# Backup Grafana data volume
docker run --rm \
  -v cloud-infra_grafana-data:/data \
  -v $(pwd)/backups:/backup \
  alpine tar czf /backup/grafana_$(date +%Y%m%d_%H%M%S).tar.gz -C /data .
```

#### Full Volume Backup

```bash
# Backup all volumes
docker run --rm \
  -v cloud-infra_postgres-data:/postgres \
  -v cloud-infra_grafana-data:/grafana \
  -v cloud-infra_caddy-data:/caddy \
  -v $(pwd)/backups:/backup \
  alpine sh -c "tar czf /backup/volumes_$(date +%Y%m%d_%H%M%S).tar.gz -C / postgres grafana caddy"
```

### Restore

#### PostgreSQL Database Restore

```bash
# Stop services
docker compose down

# Start only PostgreSQL
docker compose up -d postgres

# Wait for PostgreSQL to be ready
sleep 10

# Restore database
cat backups/postgres_YYYYMMDD_HHMMSS.sql | docker compose exec -T postgres psql -U smarthub_admin smarthub

# Start all services
docker compose up -d
```

#### Grafana Dashboards Restore

```bash
# Stop Grafana
docker compose stop grafana

# Restore Grafana data
docker run --rm \
  -v cloud-infra_grafana-data:/data \
  -v $(pwd)/backups:/backup \
  alpine sh -c "rm -rf /data/* && tar xzf /backup/grafana_YYYYMMDD_HHMMSS.tar.gz -C /data"

# Start Grafana
docker compose start grafana
```

### Automated Backup Script

Create a backup script for regular backups:

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="./backups"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR

# Backup PostgreSQL
docker compose exec -T postgres pg_dump -U smarthub_admin smarthub > $BACKUP_DIR/postgres_$DATE.sql

# Backup Grafana
docker run --rm \
  -v cloud-infra_grafana-data:/data \
  -v $(pwd)/$BACKUP_DIR:/backup \
  alpine tar czf /backup/grafana_$DATE.tar.gz -C /data .

# Keep only last 7 days of backups
find $BACKUP_DIR -name "postgres_*.sql" -mtime +7 -delete
find $BACKUP_DIR -name "grafana_*.tar.gz" -mtime +7 -delete

echo "Backup completed: $DATE"
```

Make it executable and run via cron:
```bash
chmod +x backup.sh
# Add to crontab: 0 2 * * * /path/to/backup.sh
```

## Troubleshooting

### TLS Certificate Issues

**Problem**: Caddy fails to obtain Let's Encrypt certificate

**Solutions**:
1. Verify domain DNS points to your server:
   ```bash
   nslookup grafana.yourdomain.com
   ```

2. Ensure port 80 is accessible from the internet (required for ACME HTTP-01 challenge):
   ```bash
   curl http://grafana.yourdomain.com
   ```

3. Check Caddy logs for specific errors:
   ```bash
   docker compose logs caddy
   ```

4. Let's Encrypt rate limits: 5 failures per hour per domain. Wait and retry.

### Database Connection Issues

**Problem**: Grafana cannot connect to PostgreSQL

**Solutions**:
1. Verify PostgreSQL is healthy:
   ```bash
   docker compose ps postgres
   ```

2. Check PostgreSQL logs:
   ```bash
   docker compose logs postgres
   ```

3. Verify credentials in `.env` file match

4. Restart Grafana:
   ```bash
   docker compose restart grafana
   ```

### Container Restart Loops

**Problem**: Container continuously restarts

**Solutions**:
1. Check container logs:
   ```bash
   docker compose logs <service-name>
   ```

2. Verify health check is passing:
   ```bash
   docker compose ps
   ```

3. Check disk space:
   ```bash
   df -h
   ```

4. Verify environment variables are set correctly

### Port Conflicts

**Problem**: Cannot bind to port 80 or 443

**Solutions**:
1. Check what's using the ports:
   ```bash
   # Linux
   sudo netstat -tulpn | grep :80
   sudo netstat -tulpn | grep :443
   
   # Windows
   netstat -ano | findstr :80
   netstat -ano | findstr :443
   ```

2. Stop conflicting services or change port mappings in `docker-compose.yml`

## Security Best Practices

### Production Deployment

1. **Use strong passwords**: Minimum 16 characters, generated with cryptographically secure random generators

2. **Restrict file permissions**:
   ```bash
   chmod 600 .env
   chmod 644 docker-compose.yml
   chmod 644 Caddyfile
   ```

3. **Regular updates**: Keep Docker images updated
   ```bash
   docker compose pull
   docker compose up -d
   ```

4. **Monitor logs**: Set up log aggregation and alerting

5. **Backup regularly**: Automate daily backups with retention policy

6. **Network security**: Use firewall rules to restrict access
   - Allow ports 80, 443 from internet (for Caddy)
   - Restrict SSH access to specific IPs
   - Block all other ports

7. **Resource limits**: Set memory and CPU limits in `docker-compose.yml` if needed

8. **Monitoring**: Configure Grafana alerts for disk space, memory, and service health

## Maintenance

### Updating Services

```bash
# Pull latest images
docker compose pull

# Recreate containers with new images
docker compose up -d

# Remove old images
docker image prune -f
```

### Viewing Resource Usage

```bash
# Container resource usage
docker stats

# Disk usage
docker system df
```

### Cleaning Up

```bash
# Remove stopped containers
docker compose down

# Remove volumes (WARNING: deletes all data)
docker compose down -v

# Prune unused Docker resources
docker system prune -a
```

## Architecture

### Network Topology

- **External Network**: Only Caddy exposes ports 80 and 443
- **Internal Network**: All services communicate over `smarthub-network` bridge
- **Network Isolation**: PostgreSQL and Grafana are not accessible from external networks

### Data Persistence

All data is stored in Docker volumes:
- `postgres-data`: PostgreSQL database files
- `grafana-data`: Grafana dashboards and configuration
- `caddy-data`: TLS certificates and keys
- `caddy-config`: Caddy configuration cache

### Service Dependencies

1. PostgreSQL starts first
2. Grafana starts after PostgreSQL is healthy
3. Caddy starts after Grafana is healthy

## Support

For issues or questions:
- Check the troubleshooting section above
- Review Docker Compose logs: `docker compose logs`
- Verify all prerequisites are met
- Ensure environment variables are correctly configured

## License

[Add your license information here]
