# CloudChat

Free temporary chat rooms with no sign-up, and self-destructing secret notes.

- **Chat rooms**: create a room, share the invite link, chat in real time, share files and images (up to 10 MB), see who's online. When everyone leaves, the room and everything in it is deleted.
- **Secret notes**: password-protected notes encrypted in the browser (the server never sees the text or password); readable once.
- **Abuse protection**: per-client rate limits shared across servers, per-connection message throttling, connection caps and a storage guard.

Built with Go (Gin) + Redis (Pub/Sub, so several servers can run side by side) and Vue 3 + Tailwind CSS.

## Requirements

- Go 1.25+
- Node.js 22+ (to build the frontend)
- Redis 6+ (3.0 works, but newer is recommended)

## Build and run

```bash
# 1. Build the chat app (frontend/dist) and the site stylesheet (static/site.css)
cd frontend
npm install
npm run build
cd ..

# 2. Configure (optional; every setting has a default)
cp .env.example .env

# 3. Run
go run ./cmd/server
```

Open http://localhost:8080. Run the server from the repository root: it serves `templates/`, `static/` and `frontend/dist/` from relative paths.

For frontend development with hot reload, run `npm run dev` in `frontend/` while the Go server is running; Vite proxies API and WebSocket calls to it.

## Configuration

Settings come from environment variables or a `.env` file (real environment variables win). See [`.env.example`](.env.example) for every option. The ones to check before going live:

| Variable | Why |
|---|---|
| `PUBLIC_URL` | Canonical URLs, social previews and the sitemap, e.g. `https://chat.example.com` |
| `TRUSTED_PROXIES` | Set to your reverse proxy (e.g. `127.0.0.1`) so rate limits see real client IPs |
| `REDIS_ADDR` / `REDIS_PASSWORD` | Where Redis lives |
| `STORAGE_LIMIT_MB` | Stop new rooms/files/secrets before Redis runs out of memory |

To deploy on a Linux server with systemd and HTTPS, follow [deploy/README.md](deploy/README.md).

Secret notes need HTTPS (browsers only expose the Web Crypto API on secure origins, plus `localhost`).

## Layout

```
cmd/server/          entry point, routes, graceful shutdown
internal/config/     configuration (.env + environment)
internal/ratelimit/  rate limits, throttling, connection caps, storage guard
internal/service/    secret notes and file storage
internal/transport/  HTTP handlers, server-rendered pages, WebSocket hub
templates/           homepage and changelog (Go templates)
frontend/            Vue chat app (served at /chat/) and site.css source
static/              icons, fonts, images
```

## Tests

```bash
# Unit tests
go test ./...

# End-to-end and browser tests (needs Redis, redis-cli and a built frontend)
cd tests/e2e && npm ci && npx playwright install chromium webkit && cd ../..
REDIS_ADDR=localhost:6379 node tests/e2e/run.mjs          # everything (~4 min)
REDIS_ADDR=localhost:6379 node tests/e2e/run.mjs chat ui  # only suites whose name matches
```

The e2e runner starts its own servers on ports 18200+ and uses Redis database 15
(`E2E_REDIS_DB`), which it flushes — it refuses to run if that database isn't empty.
Set `E2E_SKIP_BROWSER=1` to skip the browser suites. Logs and screenshots of failed
suites land in `tests/e2e/artifacts/`.

GitHub Actions ([.github/workflows/ci.yml](.github/workflows/ci.yml)) runs all of the above on every push to `main` and on pull requests.
