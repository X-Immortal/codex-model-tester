<div align="center">

# Codex Backend Model Tester

*Verify which backend model a selected Codex account actually serves.*

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Deploy-Compose-2496ED?style=flat&logo=docker&logoColor=white)](https://docs.docker.com/compose/)
[![API](https://img.shields.io/badge/API-OpenAI%20%2B%20Anthropic-412991?style=flat&logo=openai&logoColor=white)]()
[![License](https://img.shields.io/badge/License-MIT-yellow?style=flat)](LICENSE)

</div>

---

```bash
go run ./cmd/api  # opens the local model tester automatically
```

For Docker or external API clients, copy `.env.example` to `.env` and set a
stable `PROXY_API_KEY` before starting the service.

Exposes OpenAI and Anthropic endpoints, translates each request into the private
`chatgpt.com/backend-api/codex/*` format, and routes it across your logged-in
Codex accounts.

The upstream API is private and undocumented, so it can change without notice.
Built for local and small-scale use.

## Project lineage

This repository is based on [10/chatgpt-codex-proxy](https://github.com/10/chatgpt-codex-proxy),
an MIT-licensed OpenAI- and Anthropic-compatible proxy. It retains the original
proxy APIs and multi-account routing, while this fork adds a localhost-only
model-testing UI, account-scoped model discovery, raw upstream model comparison,
quota and heartbeat monitoring, OAuth logout, automatic browser startup, and
a single-file Windows tray launcher for the local Web UI.

## Features

- **Two API surfaces, one backend** — OpenAI Chat Completions, Responses, and Images plus Anthropic Messages.
- **Streaming everywhere** — SSE, a persistent WebSocket for Responses, or plain JSON.
- **Multi-account rotation** — least-used, round-robin, or sticky, with cooldowns and quota awareness.
- **Device login** — add an account by opening a URL. No cookie scraping, no pasted tokens.
- **Local model tester** — the embedded Web UI opens automatically and uses a localhost-only session, so it never asks for the proxy API key.
- **Model heartbeat monitoring** — pin an account and model, choose a randomized interval from 1–10 minutes up to 24–48 hours, and receive a desktop notification on failure, mismatch, or recovery.
- **Tools and structured output** — custom tools, legacy `functions`, `json_schema`, `json_object`.

## Quick Start

The local model tester needs only Go `1.26.8` or later. No API key setup is
required: when `PROXY_API_KEY` is absent, the process generates a private random
key for its own localhost session and opens the browser automatically.

Run it directly:

```bash
go run ./cmd/api
```

Only set a stable key when another OpenAI- or Anthropic-compatible client needs
to call the proxy API:

```bash
export PROXY_URL=http://localhost:8080
export PROXY_API_KEY=change-me-to-a-long-random-string
```

The model tester opens automatically at
`http://127.0.0.1:8080/`. The browser UI does not receive or store
`PROXY_API_KEY`; the backend issues it a process-local, HttpOnly session cookie.
Set `OPEN_BROWSER=false` for a server, container, or headless environment.

Add an account — start a device login, open the returned `auth_url`, then poll
until `status` is `ready`:

```bash
curl -sS -X POST "${PROXY_URL}/admin/accounts/device-login/start" \
  -H "Authorization: Bearer ${PROXY_API_KEY}"

curl -sS "${PROXY_URL}/admin/accounts/device-login/<login_id>" \
  -H "Authorization: Bearer ${PROXY_API_KEY}"
```

Then:

```bash
curl -sS "${PROXY_URL}/v1/chat/completions" \
  -H "Authorization: Bearer ${PROXY_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-6-astra","messages":[{"role":"user","content":"hello"}]}'
```

## Clients

Point any OpenAI client at `http://localhost:8080/v1` using `PROXY_API_KEY` as
the API key. For Claude Code or another Anthropic client:

```bash
export ANTHROPIC_BASE_URL=http://localhost:8080
export ANTHROPIC_API_KEY="${PROXY_API_KEY}"
export ANTHROPIC_MODEL=gpt-6-astra
```

Use a model ID from `GET /v1/models`; the default is `gpt-6-astra`.

Public API routes and external admin API calls need the key, as either
`Authorization: Bearer <key>` or `X-API-Key: <key>`. The embedded admin UI is
available only from localhost and authenticates its own same-origin requests
with an HttpOnly session cookie.

## API

```
POST /v1/completions              POST /v1/messages
POST /v1/chat/completions         POST /v1/messages/count_tokens
POST /v1/responses                GET  /v1/models
GET  /v1/responses   (WebSocket)  GET  /v1/models/:model_id
POST /v1/responses/compact        GET  /health
POST /v1/images/generations       GET  /health/live
POST /v1/images/edits
```

Text, image, and file inputs, reasoning, hosted web search, streaming and
non-streaming. The model catalog comes from upstream for each account at runtime.

Gotchas:

- `GET /v1/responses` is a WebSocket, not SSE. First message must be `response.create`; later turns use `response.create` or `response.append`. No `[DONE]` marker.
- `/v1/chat/completions` also accepts a Responses-shaped body when `messages` is omitted.
- Images default to `gpt-image-2`. If the native endpoint returns `404`, `405`, or `501`, it falls back to the Responses image tool — one image, data URL.
- Anthropic requests need `Anthropic-Version: 2023-06-01`. Sampling controls, stop sequences, `max_tokens` truncation, and thinking budgets are accepted but advisory; Codex has no equivalent.
- Audio input is rejected. Request bodies must be identity or zstd.

Exact rules: [docs/TRANSLATION.md](docs/TRANSLATION.md),
[docs/ANTHROPIC.md](docs/ANTHROPIC.md).

## Accounts

```
GET    /admin/accounts
POST   /admin/accounts/device-login/start
GET    /admin/accounts/device-login/:login_id
DELETE /admin/accounts/:account_id
POST   /admin/accounts/:account_id/logout
PATCH  /admin/accounts/:account_id
GET    /admin/accounts/:account_id/usage
POST   /admin/accounts/:account_id/refresh
GET    /admin/accounts/:account_id/models
POST   /admin/model-test
GET    /admin/heartbeats
POST   /admin/heartbeats
POST   /admin/heartbeats/:heartbeat_id/run
DELETE /admin/heartbeats/:heartbeat_id
POST   /admin/notifications/test
GET    /admin/rotation
PUT    /admin/rotation
```

POST /admin/accounts/:account_id/logout revokes the account's upstream OAuth
refresh and access tokens before removing its local credentials. If revocation
fails, local credentials are kept so the operation can be retried.
DELETE /admin/accounts/:account_id only removes local credentials.

Rotation is `least_used`, `round_robin`, or `sticky`. An account is skipped when
its status is `disabled`, `expired`, or `banned`, a cooldown is active, its
token is missing, or its quota is spent. `code_review_rate_limit` is tracked but
does not affect routing.

A failed OAuth refresh only expires an account on `invalid_grant`. Anything else
keeps it active behind a 60-second cooldown.

Details: [docs/MULTI_ACCOUNT_ROTATION_STRATEGY.md](docs/MULTI_ACCOUNT_ROTATION_STRATEGY.md).

## Deployment

`compose.yaml` persists state in the `chatgpt-codex-proxy-data` volume and runs
with basic hardening. `Dockerfile` builds the API server alone.

```bash
docker compose up -d --build
docker compose logs -f
```

Config is environment-only: `PROXY_API_KEY` (optional for direct local runs;
Docker Compose intentionally requires a stable value), `PORT` (`8080`),
`DATA_DIR` (`data`, or `/app/data` in Docker), `DEBUG_LOG_PAYLOADS` (`false`),
`OPEN_BROWSER` (`true` for direct runs, disabled by the Docker config), and
`UPSTREAM_PROXY` (optional explicit HTTP/HTTPS proxy for upstream requests).
When `UPSTREAM_PROXY` is unset, the app uses `HTTPS_PROXY`/`HTTP_PROXY` and, on
macOS, falls back to the HTTP/HTTPS proxy configured in System Settings. The
same proxy is used for OAuth account login, Codex HTTP/SSE requests, and
upstream WebSocket connections.

## Windows application

The Windows release is one portable executable containing the Go backend and
embedded Web UI. Double-click it to start the local service in the system tray
and open the tester in the default browser. Use the tray menu to reopen the
page or choose `退出` to stop the backend. It does not require Node.js,
Electron, an installer, or a manually configured proxy API key.

By default, runtime data is stored in
`%AppData%\Codex Backend Model Tester\data`, not beside the executable. An
explicit `DATA_DIR` still takes precedence.

Build the Windows x64 executable from any Go-supported host:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -buildvcs=false -ldflags="-s -w -H=windowsgui" \
  -o dist/Codex-Model-Tester-windows-x64.exe ./cmd/api
```

Unsigned builds can trigger Windows SmartScreen. The
`.github/workflows/windows-release.yml` workflow produces the same portable
executable for version tags and manual runs.

## macOS application

The macOS release is a `.app` bundle containing the same Go backend and
embedded Web UI, distributed inside a `.dmg`. Drag it to `Applications` and
double-click it: the local service starts, the tester opens in the default
browser, and the app keeps running in both the menu bar and the Dock. The Dock
icon shows the normal running indicator (a small dot); click it to reopen the
Web UI. You can also click the menu bar icon and choose `打开网页`, or stop the
backend with `退出`. Like the Windows build it needs no Node.js, no Electron,
and no manually configured proxy API key. Launching it again while it is
already running reuses the running instance: the second launch opens the
existing Web UI and exits without starting a second backend.

By default, runtime data is stored in
`~/Library/Application Support/Codex Backend Model Tester/data`, not beside the
bundle — a `.app` launched from Finder starts with `/` as its working
directory. An explicit `DATA_DIR` still takes precedence.

Build the bundle on macOS:

```bash
scripts/build-macos-app.sh
```

That produces a universal (arm64 + x86_64) `dist/Codex Model Tester.app` and
`dist/Codex-Model-Tester-macos-universal.dmg`, using only the Go toolchain and
tools shipped with macOS. `VERSION`, `ARCHS` (e.g. `ARCHS=arm64`), and
`SKIP_DMG=1` override the defaults.

The bundle is ad-hoc signed, so a copy downloaded from the internet is
quarantined and Gatekeeper may block the first launch. If double-clicking shows
the malware-verification warning, close it, open **System Settings → Privacy &
Security**, scroll to the **Security** section, and click **Open Anyway** next
to the Codex Model Tester message. Confirm **Open** in the next dialog, then
launch the app again. The button only appears after macOS has blocked one
launch attempt. As a terminal fallback, clear the quarantine flag:

```bash
xattr -dr com.apple.quarantine "/Applications/Codex Model Tester.app"
```

The `.github/workflows/macos-release.yml` workflow produces the same `.dmg`
for version tags and manual runs.

`${DATA_DIR}` holds `accounts.json` — accounts, OAuth tokens, labels, status,
quota, cooldowns — `models-cache.json`, and `heartbeats.json`. Heartbeats choose
from 40 lightweight prompts and randomly schedule within the range selected in the UI.
They never rotate to another account and only notify when health changes. Continuation state and in-flight
device logins are memory-only and do not survive a restart.

## How It Works

1. Gin accepts an OpenAI- or Anthropic-shaped request.
2. Adapters normalize it into one internal turn model.
3. The proxy picks a ready account, translates, and calls Codex over SSE or WebSocket.
4. Upstream events convert back to whichever protocol the client asked for.

<img width="4599" height="2073" alt="chatgpt-codex-proxy architecture flowchart" src="https://github.com/user-attachments/assets/05cd8446-dd4b-43bc-a3fc-eb370ad917e6" />

```
POST https://chatgpt.com/backend-api/codex/responses
GET  https://chatgpt.com/backend-api/codex/usage
GET  https://chatgpt.com/backend-api/codex/models
WSS  https://chatgpt.com/backend-api/codex/responses
     https://auth.openai.com/api/accounts/deviceauth/*
     https://auth.openai.com/oauth/token
```

## Layout

```
chatgpt-codex-proxy/
├── cmd/api/                  # server entrypoint
├── internal/
│   ├── server/               # Gin routing and handlers
│   ├── openai/ anthropic/    # public protocol adapters
│   ├── turn/                 # internal turn model and accumulation
│   ├── codex/ codexauth/     # private upstream client and OAuth
│   ├── accounts/             # account store
│   ├── accountmanager/       # rotation, cooldowns, quota routing
│   ├── devicelogin/          # device-auth onboarding
│   ├── conversation/         # continuation state and affinity
│   ├── models/               # runtime model catalog
│   ├── middleware/           # authentication and request logging
│   └── config/               # environment configuration
├── scripts/                  # macOS .app and .dmg packaging
├── test/integration/         # live compatibility suite
└── docs/                     # upstream and translation references
```

## Development

```bash
go test ./...
```

Live tests, against a proxy you already have running:

```bash
OPENAI_API_KEY=change-me-to-a-long-random-string \
OPENAI_MODEL=gpt-6-astra \
OPENAI_BASE_URL="${PROXY_URL}/v1" \
go test -tags=live ./test/integration -v -count=1
```

## Docs

- [docs/TRANSLATION.md](docs/TRANSLATION.md) — OpenAI-to-Codex translation and compatibility rules
- [docs/ANTHROPIC.md](docs/ANTHROPIC.md) — Anthropic mapping, streaming, token counting, limits
- [docs/MULTI_ACCOUNT_ROTATION_STRATEGY.md](docs/MULTI_ACCOUNT_ROTATION_STRATEGY.md) — account selection and quota routing
- [docs/CODEX_API_DOCS.md](docs/CODEX_API_DOCS.md) — private upstream behavior, inferred from this codebase

## Limitations

- Upstream is private and may change without notice.
- Device auth is the only onboarding flow.
- Continuation state is in memory and expires with its TTL.
- Deliberately small; it does not chase every edge of the public OpenAI platform.
