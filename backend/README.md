# cc-web backend

The Go API uses SQLite on disk and includes a single-admin password login for the web workbench. It does not start a Codex/Claude CLI or claim that planned Team members have run. Passwords are hashed with Argon2id, session tokens are stored only as hashes, cookies are HttpOnly/Secure/SameSite, mutating API calls require Origin and CSRF validation, and the initial password must be changed before other API routes are available.

## Run locally

```sh
CC_WEB_DB_PATH=./data/cc-web.db go -C backend run ./cmd/cc-web
```

`CC_WEB_DB_PATH` defaults to `/data/cc-web.db`. The parent directory is created on startup. Schema migrations run automatically; seed project/session data is inserted only when the project table is empty. Back up the database file while the service is stopped, or use SQLite's online backup mechanism when running with WAL mode (the `-wal`/`-shm` files are part of the live database state and must not be copied independently).

The service binds to `127.0.0.1:8080` by default. Set `CC_WEB_LISTEN` only to a loopback address for local operation. Docker Compose listens inside the container and publishes on host loopback at `127.0.0.1:18089` (host port 8080 is occupied on the target server). Production is intended to be reached via HTTPS at `https://103.11.77.113/cc-web/` through the existing aaPanel Nginx IP virtual host; never expose a plaintext HTTP login endpoint. The public IP certificate is short-lived, so verify renewal automation and alert on failed renewal/low remaining validity. Authentication does not mitigate the risk of a compromised application process.

## Docker

From the repository root, build the images, initialize the single administrator once from an interactive SSH terminal, then start the services:

```sh
docker compose build
docker compose run --rm --no-deps cc-web admin init --username admin
docker compose up -d
docker compose ps
```

The init command requires a TTY, reads the password and confirmation without echo, stores only an Argon2id hash in SQLite, and does not print the password. Ordinary service startup fails closed until the administrator is initialized. Never pass the password in command-line arguments, shell history, environment variables, or logs.

Compose mounts the named volume `cc-web-data` at `/data`, which persists the SQLite database across container recreation. At the user's explicit request, it runs as UID/GID 0 and bind-mounts the entire host root (`/`) read-write at `/host`; `CC_WEB_HOST_ROOT=/host` identifies this path inside the container. This allows modifying or deleting host files wherever the Docker daemon permits and exposes secrets/system files. It is not privileged mode, but is still effectively a host-administration capability: application compromise can become server compromise even when login is enabled. No untrusted users or agent/tool execution should be allowed on this instance. The current API can register existing directories under `/host`; complete file browsing/editing APIs are not implemented. The API port remains loopback-only at `127.0.0.1:18089`; Nginx terminates HTTPS at `/cc-web/`.

On rootless Docker, host permission limits still apply and some system paths may not be accessible. On SELinux systems, bind-mount labeling may need host-specific configuration; do not add `:z`/`:Z` blindly to the host root. `docker compose down -v` deletes the SQLite named volume and all stored application data; do not use it unless you intend to erase the database.

Back up the named volume before upgrades. One portable approach is to stop the service, then archive the volume contents with a temporary helper container; restore the archive into the same volume before starting the service again. Verify `/healthz` and `/api/status` after restore.

## Initial administrator

The database is not initialized automatically. With the backend image built and the services stopped or the backend not yet started, connect to the server through an interactive SSH terminal and run `docker compose run --rm --no-deps cc-web admin init --username admin`. Do not add `-T`: the command needs a TTY for two hidden password entries. It creates exactly one administrator, stores an Argon2id password hash, and marks the account as requiring a password change. Re-running initialization is rejected. Ordinary service startup refuses to listen while no administrator exists. After starting the services, sign in through HTTPS and change the initial password before using projects or sessions.

## API

Public endpoints: `GET /healthz`, `GET /api/status`, `POST /api/auth/login`. Authenticated endpoints: `GET /api/auth/me`, `POST /api/auth/password`, `POST /api/auth/logout`, and all workspace/session/team/approval routes. Mutating authenticated routes require the session cookie, a matching `Origin`, and `X-CSRF-Token`.

- `GET /healthz`
- `GET /api/status`
- `POST /api/auth/login`
- `GET /api/auth/me`
- `POST /api/auth/password`
- `POST /api/auth/logout`
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
curl http://127.0.0.1:18089/healthz
curl http://127.0.0.1:18089/api/status
```
