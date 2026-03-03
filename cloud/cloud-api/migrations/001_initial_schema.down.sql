-- 001_initial_schema.down.sql
-- Drops all tables created in 001_initial_schema.up.sql.
-- Order matters: drop dependents before parents.

DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS rules;
DROP TABLE IF EXISTS hubs;
DROP TABLE IF EXISTS tenants;
