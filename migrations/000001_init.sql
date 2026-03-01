-- Initial opcAPI schema for MVP.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(120) NOT NULL,
    plan_id VARCHAR(64) NOT NULL DEFAULT 'free',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    phone VARCHAR(32),
    wechat_openid VARCHAR(128),
    feishu_open_id VARCHAR(128),
    email VARCHAR(255),
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_identity_check CHECK (
        phone IS NOT NULL OR wechat_openid IS NOT NULL OR feishu_open_id IS NOT NULL OR email IS NOT NULL
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS users_tenant_phone_uk
    ON users (tenant_id, phone)
    WHERE phone IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_tenant_wechat_uk
    ON users (tenant_id, wechat_openid)
    WHERE wechat_openid IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_tenant_feishu_uk
    ON users (tenant_id, feishu_open_id)
    WHERE feishu_open_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_tenant_email_uk
    ON users (tenant_id, email)
    WHERE email IS NOT NULL;

CREATE TABLE IF NOT EXISTS memberships (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(32) NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id)
);

CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    key_hash VARCHAR(128) NOT NULL,
    prefix VARCHAR(32) NOT NULL,
    parent_key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, key_hash)
);

CREATE INDEX IF NOT EXISTS api_keys_tenant_idx ON api_keys (tenant_id);
CREATE INDEX IF NOT EXISTS api_keys_parent_idx ON api_keys (parent_key_id);

CREATE TABLE IF NOT EXISTS key_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id UUID NOT NULL UNIQUE REFERENCES api_keys(id) ON DELETE CASCADE,
    rpm_limit INTEGER NOT NULL DEFAULT 60,
    tpm_limit INTEGER NOT NULL DEFAULT 60000,
    daily_token_limit BIGINT NOT NULL DEFAULT 1000000,
    model_allowlist JSONB NOT NULL DEFAULT '[]'::jsonb,
    ip_allowlist JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    type VARCHAR(32) NOT NULL,
    endpoint TEXT NOT NULL,
    credential_ref VARCHAR(255) NOT NULL,
    weight INTEGER NOT NULL DEFAULT 100,
    priority INTEGER NOT NULL DEFAULT 100,
    health_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS providers_tenant_type_idx ON providers (tenant_id, type);

CREATE TABLE IF NOT EXISTS models (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id UUID NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    model_name VARCHAR(120) NOT NULL,
    input_price NUMERIC(18, 8) NOT NULL DEFAULT 0,
    output_price NUMERIC(18, 8) NOT NULL DEFAULT 0,
    max_context INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider_id, model_name)
);

CREATE INDEX IF NOT EXISTS models_provider_idx ON models (provider_id);

CREATE TABLE IF NOT EXISTS routes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    scene VARCHAR(64) NOT NULL,
    strategy VARCHAR(32) NOT NULL,
    fallback_chain JSONB NOT NULL DEFAULT '[]'::jsonb,
    timeout_ms INTEGER NOT NULL DEFAULT 30000,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, scene)
);

CREATE TABLE IF NOT EXISTS prompt_apis (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    slug VARCHAR(120) NOT NULL,
    input_schema JSONB NOT NULL,
    output_schema JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS prompt_api_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    prompt_api_id UUID NOT NULL REFERENCES prompt_apis(id) ON DELETE CASCADE,
    version VARCHAR(32) NOT NULL,
    prompt_template TEXT NOT NULL,
    model_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    eval_score NUMERIC(8, 4) NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (prompt_api_id, version)
);

CREATE TABLE IF NOT EXISTS requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    prompt_api_version_id UUID REFERENCES prompt_api_versions(id) ON DELETE SET NULL,
    provider_type VARCHAR(32) NOT NULL,
    model VARCHAR(120) NOT NULL,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost NUMERIC(18, 8) NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL,
    trace_id VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS requests_tenant_created_idx ON requests (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS requests_trace_idx ON requests (trace_id);

CREATE TABLE IF NOT EXISTS balances (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    available_amount NUMERIC(18, 4) NOT NULL DEFAULT 0,
    frozen_amount NUMERIC(18, 4) NOT NULL DEFAULT 0,
    currency VARCHAR(8) NOT NULL DEFAULT 'CNY',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS billing_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    type VARCHAR(32) NOT NULL,
    amount NUMERIC(18, 4) NOT NULL,
    request_id UUID REFERENCES requests(id) ON DELETE SET NULL,
    order_id UUID,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS billing_events_tenant_created_idx ON billing_events (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel VARCHAR(32) NOT NULL,
    product_type VARCHAR(32) NOT NULL,
    amount NUMERIC(18, 4) NOT NULL,
    currency VARCHAR(8) NOT NULL DEFAULT 'CNY',
    status VARCHAR(32) NOT NULL,
    external_order_id VARCHAR(128),
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS orders_tenant_created_idx ON orders (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    plan_id VARCHAR(64) NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    renew_status VARCHAR(32) NOT NULL DEFAULT 'auto',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS subscriptions_tenant_status_idx ON subscriptions (tenant_id, status);

CREATE TABLE IF NOT EXISTS webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel VARCHAR(32) NOT NULL,
    event_id VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (channel, event_id)
);
