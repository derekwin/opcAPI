# opcAPI

`opcAPI` is a single-app backend for OPC builders:
- regular app backend APIs (`/api/*`)
- OpenAI-compatible LLM gateway (`/v1/*`)
- admin web console (`/admin`)

## Routing Convention

- `/v1/*`: OpenAI-compatible / LLM related endpoints only
- `/api/*`: app backend endpoints (auth, billing, admin APIs)
- `/admin/*`: server-rendered admin console pages/actions

## Current Endpoints

### Health
- `GET /healthz`

### Auth (`/api`)
- `POST /api/auth/signup` (email + password)
- `POST /api/auth/signin` (email + password)
- `GET /api/auth/me`
- `POST /api/auth/logout` (invalidate current JWT-like session token)

### Admin APIs (`/api/admin`, admin role required)
- `GET /api/admin/users`
- `GET|POST /api/admin/prompt-apis`
- `GET /api/admin/audit/logs`
- `GET /api/admin/billing/orders`
- `GET|POST|DELETE /api/admin/upstreams`
- `POST /api/admin/upstreams/diagnose`
- `GET|POST|DELETE /api/admin/routes`
- `GET /api/admin/routes/runtime-stats`

### Billing (`/api/billing`)
- `GET /api/billing/balance`
- `POST /api/billing/subscribe`

### OpenAI-Compatible (`/v1`)
- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/prompt-apis/{slug}/chat/completions`

Membership quota policy on `/v1/*`:
- 5-hour token window
- monthly token window
- top tier can be unlimited (configured in admin)

### Admin Web
- `GET|POST /admin/login`
- `GET|POST /admin/setup` (first boot only)
- `GET|POST /admin/logout`
- `GET /admin` (redirect to `/admin/users`)
- `GET /admin/users`
- `GET /admin/prompts`
- `GET /admin/upstreams`
- `POST /admin/prompt-apis/new`
- `POST /admin/users/new`
- `POST /admin/users/role`
- `POST /admin/users/status`
- `POST /admin/users/membership`
- `POST /admin/billing/plans`
- `POST /admin/upstreams/new`
- `POST /admin/upstreams/delete`
- `POST /admin/upstreams/diagnose`
- `POST /admin/upstreams/toggle`
- `POST /admin/routes/new`
- `POST /admin/routes/delete`

## Quick Start

```bash
cp .env.example .env
make run
```

Server default: `:8080`

Open admin console:
- `http://localhost:8080/admin/login`

First boot flow:
- if no admin exists, `/admin/login` redirects to `/admin/setup`
- `/admin/setup` can only create the first super admin once
- after first super admin exists, setup is closed and only login is allowed

## User Auth Example (Email + Password)

```bash
curl -s http://localhost:8080/api/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{
    "email":"user@example.com",
    "password":"Password123"
  }'
```

Then use returned `token` (session token) for LLM gateway:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer <session_token>' \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"stub/moonshot-v1",
    "messages":[{"role":"user","content":"hello opcAPI"}]
  }'
```

Logout and revoke current session token:

```bash
curl -s -X POST http://localhost:8080/api/auth/logout \
  -H 'Authorization: Bearer <session_token>'
```

## Upstream Routing (Persistent)

Upstream store path:
- `UPSTREAM_STORE_PATH` (default `data/upstreams.json`)

Supported upstream types:
- `openai_compat` (OpenAI-compatible vendor APIs)
- `ollama` (self-hosted Ollama)

Routing strategies:
- `fallback` (priority chain)
- `weighted` (weighted primary + fallback chain)

Use `/admin/upstreams` to manage:
- upstream provider endpoints + credentials
- model alias routing targets
- runtime routing behavior for `/v1/chat/completions`
- upstream health checks and fuse(toggle enable/disable)
- runtime route stats (success/failure/latency)

## Verification

```bash
go test ./...
go test -race ./...
go build ./cmd/api
```

## App Simulator (for backend testing)

Run a small client that simulates common app traffic:
- `healthz`
- `signup/signin -> me -> membership -> chat -> logout`
- chat model is selected server-side by admin routing; end user does not need model awareness

```bash
go run ./cmd/appsim --base-url http://127.0.0.1:8080
```

Run full backend API suite (covers `/api/*`, `/api/admin/*`, `/v1/*`, and optional `/admin/*` web actions):

```bash
go run ./cmd/appsim \
  --suite full \
  --base-url http://127.0.0.1:8080 \
  --email appsim-user@example.com \
  --password Password123 \
  --admin-email admin@example.com \
  --admin-password Password123
```

Useful options:

```bash
# use fixed account (signup first, then fallback to signin if already exists)
go run ./cmd/appsim \
  --base-url http://127.0.0.1:8080 \
  --email tester@example.com \
  --password Password123

# reuse existing session and send 5 chat calls
go run ./cmd/appsim \
  --base-url http://127.0.0.1:8080 \
  --session-token <session_token> \
  --chat-rounds 5 \
  --message "hello from smoke test"

# buy one membership cycle before chat
go run ./cmd/appsim \
  --base-url http://127.0.0.1:8080 \
  --email tester@example.com \
  --password Password123 \
  --subscribe \
  --subscribe-plan starter

# interactive mode (manual command loop)
go run ./cmd/appsim \
  --base-url http://127.0.0.1:8080 \
  --email tester@example.com \
  --password Password123 \
  --interactive

# full suite without /admin web form smoke
go run ./cmd/appsim \
  --suite full \
  --base-url http://127.0.0.1:8080 \
  --admin-email admin@example.com \
  --admin-password Password123 \
  --admin-web=false
```

Interactive commands:
- `help`
- `status`
- `session`
- `me`
- `signin`
- `signup`
- `balance`
- `subscribe [plan] [channel]`
- `chat [message]`
- `logout`
- `exit`
