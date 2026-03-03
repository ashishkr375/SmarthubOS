-- 002_timescale_hypertable.up.sql
-- Creates the telemetry hypertable, indexes, retention policy, and
-- continuous aggregate for hourly rollups.
-- Requires TimescaleDB extension (timescale/timescaledb:latest-pg16).

-- ─── TELEMETRY HYPERTABLE ────────────────────────────────────────────────────
CREATE TABLE telemetry (
    time         TIMESTAMPTZ  NOT NULL,
    tenant_id    UUID         NOT NULL,
    hub_id       UUID         NOT NULL,
    device_id    UUID         NOT NULL,
    protocol_src VARCHAR(10)  NOT NULL,
    payload      JSONB        NOT NULL
);

-- Convert to hypertable partitioned by time (1-week chunks by default)
SELECT create_hypertable('telemetry', 'time');

-- Composite indexes for fast tenant/device queries
CREATE INDEX idx_telemetry_tenant ON telemetry(tenant_id, time DESC);
CREATE INDEX idx_telemetry_device ON telemetry(device_id, time DESC);

-- Retention: keep raw telemetry 90 days, then drop automatically
SELECT add_retention_policy('telemetry', INTERVAL '90 days');

-- Continuous aggregate: hourly averages (kept indefinitely for Grafana charts)
CREATE MATERIALIZED VIEW telemetry_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time)              AS bucket,
    tenant_id,
    device_id,
    avg((payload->>'temperature')::float)    AS avg_temp,
    avg((payload->>'humidity')::float)       AS avg_humidity
FROM telemetry
GROUP BY bucket, tenant_id, device_id;
