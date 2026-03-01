# opcAPI

`opcAPI` is an OPC-focused backend project for multi-model AI API aggregation and productized Prompt APIs.

This repository currently contains the **Week 1 MVP skeleton**:
- Go backend service bootstrap
- OpenAI-compatible base endpoints
- Unified error format and request middleware
- Health check and basic observability
- Provider gateway v1 (`OpenAI-compatible` + `Ollama` + local stub)

## Current Endpoints

- `GET /healthz`
- `GET|POST /admin/login` (server-rendered admin login/signup page)
- `GET|POST /admin/logout`
- `GET /admin` (server-rendered management console)
- `POST /admin/keys/new`
- `POST /admin/keys/delete`
- `POST /admin/prompt-apis/new`
- `POST /admin/billing/topup`
- `POST /admin/users/new` (owner-only, create tenant admin/member)
- `POST /admin/users/role` (owner-only, update tenant user role)
- `POST /admin/users/status` (owner-only, disable/enable tenant user)
- `POST /v1/auth/signup`
- `POST /v1/auth/signin`
- `GET /v1/auth/me`
- `GET /v1/admin/users`
- `GET|POST|DELETE /v1/admin/keys` (tenant-scoped API key policy CRUD, supports `enabled/limit/offset`; `tenant_id` is restricted to current session tenant)
- `GET /v1/admin/audit/logs` (admin audit query: `resource/action/actor_user_id/target_tenant_id/limit/offset`)
- `GET|POST /v1/admin/prompt-apis` (Prompt API definition list/upsert/publish)
- `GET /v1/billing/balance`
- `POST /v1/billing/topup`
- `POST /v1/billing/subscribe`
- `GET /v1/admin/billing/orders` (supports `type/status/channel/limit/offset`)
- `GET /v1/models`
- `POST /v1/chat/completions` (supports non-stream + stream)
- `POST /v1/prompt-apis/{slug}/chat/completions` (published Prompt API invoke)

## Quick Start

```bash
cp .env.example .env
make run
```

Server default address: `:8080`.
Default static development API key: `sk-opcapi-dev` (from `.env.example`).
By default `REQUIRE_TENANT_BOUND_KEY=true`, so chat/prompt inference should use tenant-bound API keys (from signup auto-provisioning).
Rate limit and quota policy are persisted and hot-reloaded by `POLICY_REFRESH_INTERVAL`.

Admin web console:
- open `http://localhost:8080/admin/login`
- sign in with existing account, or sign up directly on the page
- the console is server-rendered and reuses existing `/v1/admin/*` services internally
- first signup user in a tenant is `owner` and can manage tenant admins in the Users card
- owner can disable/enable users in Users card; disabled users cannot sign in or use existing session routes

Provider selection:
- `model` starts with `openai/` -> route to OpenAI-compatible upstream
- `model` starts with `ollama/` -> route to Ollama upstream
- no prefix -> route by `DEFAULT_PROVIDER` (`stub` by default)

## Example Requests

Get a tenant-bound API key (signup auto-provisions one key):

```bash
curl -s http://localhost:8080/v1/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{
    "channel":"phone",
    "identifier":"13800138000",
    "verification_code":"000000",
    "tenant_name":"demo"
  }'
```

Then use returned `api_key`:

Non-stream chat completion:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer <tenant-api-key>' \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"stub/moonshot-v1",
    "messages":[{"role":"user","content":"hello opcAPI"}]
  }'
```

Stream chat completion:

```bash
curl -N http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer <tenant-api-key>' \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"stub/moonshot-v1",
    "stream":true,
    "messages":[{"role":"user","content":"hello opcAPI"}]
  }'
```

OpenAI-compatible upstream:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer <tenant-api-key>' \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"openai/gpt-4o-mini",
    "messages":[{"role":"user","content":"write a title"}]
  }'
```

Ollama upstream:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer <tenant-api-key>' \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"ollama/qwen2.5:7b",
    "messages":[{"role":"user","content":"say hello"}]
  }'
```

## Project Structure

```txt
cmd/api                  # API entrypoint
internal/app             # app lifecycle and graceful shutdown
internal/config          # env-based config loader
internal/observability   # logger
internal/transport/http  # HTTP router, middleware, OpenAI-compatible handlers
internal/provider        # provider gateway and adapters
internal/auth            # API key authentication
internal/quota           # request rate and token quota limiter
internal/keymgmt         # persisted policy sync (file/redis)
internal/store/redis     # minimal Redis client (no external deps)
```

## Policy Persistence

`POLICY_BACKEND`:
- `file` (default): load/save policy JSON from `POLICY_STORE_PATH` (`data/policies.json`)
- `redis`: load/save policy JSON from `REDIS_POLICY_KEY`

Quota counter behavior:
- `file` backend: request/token counters are in-memory (reset on restart)
- `redis` backend: request/token counters are stored in Redis (`REDIS_QUOTA_PREFIX`) and shared across instances

Example:

```json
{
  "version": 1,
  "keys": [
    {
      "key_hash": "<sha256-of-api-key>",
      "enabled": true,
      "rpm_limit": 120,
      "daily_token_limit": 1000000
    }
  ]
}
```

Fields:
- `key_hash`: sha256 hash of API key (recommended and default)
- `tenant_id`: optional tenant binding for key-level tenant isolation
- `enabled`: disable key without deleting policy
- `rpm_limit`: per-key requests/minute
- `daily_token_limit`: per-key daily token quota

Compatibility:
- existing policy payloads containing `api_key` are still readable for backward compatibility, but new writes persist `key_hash` only.

## User Management

Auth channels:
- `phone` (requires `verification_code`, default test code from `PHONE_TEST_CODE`)
- `wechat`
- `feishu`

User storage:
- persisted in `USER_STORE_PATH` (`data/users.json` by default)

Session:
- `Authorization: Bearer <session-token>` for `/v1/auth/me` and `/v1/admin/*`
- `POST /v1/auth/signup` now auto-provisions one tenant-bound API key and returns `api_key` once in response
- `REQUIRE_TENANT_BOUND_KEY=true` (default) requires tenant-bound API keys for `/v1/chat/completions`
- set `REQUIRE_TENANT_BOUND_KEY=false` only for local compatibility testing with static keys

For tenant-bound API keys, successful inference calls with token usage will charge tenant balance automatically.
When balance is insufficient, inference returns `402` with `insufficient_balance`.

## Audit Log

Key policy changes are written to audit log (`resource=api_key`), including:
- upsert
- delete

## Prompt API

Admin creates/updates prompt API definitions:
- `POST /v1/admin/prompt-apis`
- supports `publish=true` to publish current version

Invoke published prompt API with API key:
- `POST /v1/prompt-apis/{slug}/chat/completions`
- body includes `input` only; tenant is resolved from API key binding

## Next Milestones

1. More provider adapters (`Ali DashScope`, `Kimi/Moonshot`, `vLLM`, `SGLang`, `LM Studio`)
2. API key management UI and audit/event log
3. Real payment webhooks (`Stripe` / `易支付`) + idempotent order lifecycle
4. Prompt API definition/versioning/publishing
5. Intelligent routing and failover
