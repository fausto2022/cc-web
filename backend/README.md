# cc-web backend

The Go API uses SQLite on disk. It does not start a Codex/Claude CLI or claim that planned Team members have run.

## Run locally

```sh
CC_WEB_DB_PATH=./data/cc-web.db go -C backend run ./cmd/cc-web
```

`CC_WEB_DB_PATH` defaults to `/data/cc-web.db`. The parent directory is created on startup. Schema migrations run automatically; seed project/session data is inserted only when the project table is empty. Back up the database file while the service is stopped, or use SQLite's online backup mechanism when running with WAL mode (the `-wal`/`-shm` files are part of the live database state and must not be copied independently).

The service binds to `127.0.0.1:8080` by default. Set `CC_WEB_LISTEN` only to a loopback address for local operation. The Docker image listens on the container interface, while Compose publishes it on host loopback only. Do not expose it to the internet: authentication, session protection, CSRF controls, a configured workspace-root allowlist, audit logging, and a trusted HTTPS reverse proxy are not implemented.

## Docker

From the repository root:

```sh
docker compose up -d --build
docker compose logs -f cc-web
docker compose down
```

Compose mounts the named volume `cc-web-data` at `/data`, which persists the SQLite database across container recreation. It also bind-mounts the entire host root (`/`) read-write at `/host`; `CC_WEB_HOST_ROOT=/host` identifies that mount inside the container. This gives the container process permission to modify or delete host files wherever the Docker daemon permits. It exposes secrets and system files, and is intentionally much broader than a normal workspace mount. Do not run this configuration with untrusted users, internet exposure, or agent/tool execution enabled. The current API does not yet implement file browsing/editing endpoints, so the mount is present but is not yet usable through the cc-web UI/API. The published address remains `127.0.0.1:8080`; do not widen it until authentication, authorization, auditing, and HTTPS controls exist.

On rootless Docker, host permission limits still apply and some system paths may not be accessible. On SELinux systems, bind-mount labeling may need host-specific configuration; do not add `:z`/`:Z` blindly to the host root. `docker compose down -v` deletes the SQLite named volume and all stored application data; do not use it unless you intend to erase the database.

Back up the named volume before upgrades. One portable approach is to stop the service, then archive the volume contents with a temporary helper container; restore the archive into the same volume before starting the service again. Verify `/healthz` and `/api/status` after restore.

## API

- `GET /healthz`
- `GET /api/status`
- `GET /api/projects`
- `POST /api/projects` (register an existing directory contained under `CC_WEB_HOST_ROOT`; canonical paths and symlinks are checked)
- `GET /api/sessions?projectId=cc-web`
- `POST /api/sessions`
- `GET /api/sessions/{sessionID}`
- `GET|POST /api/sessions/{sessionID}/messages`
- `GET|POST /api/sessions/{sessionID}/team-runs`
- `GET|POST /api/team-runs/{teamRunID}/messages`
- `POST /api/sessions/{sessionID}/approvals`
- `POST /api/approvals/{approvalID}/approve|reject`

Team-run creation records planned members as `planned` and returns `runtimeConnected: false`. It never launches agents. Approval records decisions but never executes commands. API status reports `persistence: sqlite` and runtime `not_connected`.

## Verify

```sh
go -C backend test ./...
go -C backend vet ./...
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/api/status
```
