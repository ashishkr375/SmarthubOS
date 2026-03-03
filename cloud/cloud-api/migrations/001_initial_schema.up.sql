-- 001_initial_schema.up.sql
-- Creates core tables: tenants, hubs, devices, rules, users, refresh_tokens.
-- Applied by golang-migrate (embedded in binary via migrations/embed.go).

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ─── TENANTS ────────────────────────────────────────────────────────────────
CREATE TABLE tenants (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(255) NOT NULL,
    namespace   VARCHAR(64)  NOT NULL UNIQUE,   -- URL-safe slug, e.g. "lab01"
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE
);
CREATE INDEX idx_tenants_namespace ON tenants(namespace);

-- ─── HUBS ───────────────────────────────────────────────────────────────────
-- One row per Raspberry Pi.
CREATE TABLE hubs (
    id           UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    name         VARCHAR(255) NOT NULL,
    api_key_hash VARCHAR(72)  NOT NULL,   -- bcrypt(hub_api_key, cost=12)
    last_ping    TIMESTAMPTZ,
    is_online    BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─── DEVICES ────────────────────────────────────────────────────────────────
CREATE TABLE devices (
    id           UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id    UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    hub_id       UUID        NOT NULL REFERENCES hubs(id),
    device_name  VARCHAR(255) NOT NULL,
    protocol     VARCHAR(10)  NOT NULL CHECK (protocol IN ('mqtt','http','coap','ws')),
    token_hash   VARCHAR(72)  NOT NULL,   -- bcrypt(token, cost=12). NEVER plaintext.
    status       VARCHAR(16)  NOT NULL DEFAULT 'active'
                              CHECK (status IN ('active','suspended','revoked')),
    last_seen    TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_devices_tenant_id ON devices(tenant_id);
CREATE INDEX idx_devices_hub_id    ON devices(hub_id);

-- ─── RULES ──────────────────────────────────────────────────────────────────
CREATE TABLE rules (
    id          UUID    PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   UUID    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    rule_json   JSONB   NOT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_rules_tenant_id ON rules(tenant_id);

-- ─── USERS ──────────────────────────────────────────────────────────────────
-- MVP: single admin bootstrapped from env vars (ADMIN_EMAIL, ADMIN_PASSWORD).
CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    email         VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(72)  NOT NULL,   -- bcrypt(password, cost=12)
    role          VARCHAR(20)  NOT NULL DEFAULT 'admin'
                               CHECK (role IN ('admin','user')),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─── REFRESH TOKENS ─────────────────────────────────────────────────────────
CREATE TABLE refresh_tokens (
    id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(72) NOT NULL,   -- bcrypt hash of the refresh token
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
