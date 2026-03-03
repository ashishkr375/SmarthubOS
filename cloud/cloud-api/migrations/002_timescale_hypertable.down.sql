-- 002_timescale_hypertable.down.sql
-- Removes the telemetry continuous aggregate and hypertable.

DROP MATERIALIZED VIEW IF EXISTS telemetry_hourly;
DROP TABLE IF EXISTS telemetry;
