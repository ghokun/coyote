# Coyote v1 Roadmap — Daemon + CLI (docker-like)

## Background

Today `coyote` runs in the foreground: a single `urfave/cli/v3` `Action`
(`coyote.go`) connects to RabbitMQ (`connect.go` + `auth/`), declares a queue,
binds exchanges, consumes, and optionally stores messages into a SQLite `event`
table. `Ctrl-C` ends the process and all consumption.

## Goal

Same binary acts as CLI client and background daemon, just like docker:

- `coyote consume ...` starts the daemon if it is not running and submits a task.
- A second `coyote consume ...` detects the running daemon and adds a new task.
- CLI exposes `list`, `start`, `stop`, `pause`, `resume`, `rm`, `logs`, `fetch`,
  plus `daemon` management and `fetch` of stored messages.

## Architecture

- **Single binary dispatch:** thin `coyote.go` wiring only; subcommands own
  behavior. Hidden `daemon` command for the background process.
- **Daemon detection:** socket probe primary (`~/.config/coyote/coyote.sock`,
  resolved as `$XDG_CONFIG_HOME/coyote` when `XDG_CONFIG_HOME` is set, short
  dial timeout); `daemon.pid` diagnostics-only (pid reuse unsafe).
  Dir `0700`, socket `0600`.
- **Auto-start:** on `ENOENT`/`ECONNREFUSED`, spawn
  `exec.Command(os.Executable(), "daemon")` with `Setsid:true`,
  `Stdin:/dev/null`, output to `~/.config/coyote/daemon.log`, then poll socket.
  Unix-only MVP (darwin/linux); error on Windows.
- **IPC:** Unix domain socket + `net/http` JSON, stdlib only. Rejected:
  raw JSON-over-socket (framing), gRPC (deps/overkill), TCP localhost
  (ports/firewall).
- **Daemon internals:** supervisor `map[taskID]cancelFunc + status`, one
  goroutine per task reusing extracted `consumeTask()` (channel/queue/bind/
  consume + store loop). Per-task AMQP `Connection` (isolation over conn
  sharing). `~/.config/coyote/daemon.db` (existing `modernc.org/sqlite`) persists
  task specs; memory is runtime truth. `NotifyClose` + exponential backoff
  (1s→30s, 5 tries → `failed`). Ephemeral `coyote.<uuid>` queues recreated
  on restart.
- **Signals:** daemon handles `SIGINT`+`SIGTERM` → graceful drain (close
  channels/connections, flush DB). Client `Ctrl-C` only cancels submit/wait.
- **Layout (proposed):**
  - `coyote.go` — CLI wiring only
  - `internal/ipc/` — socket paths, HTTP server/handlers, client
  - `internal/daemon/` — supervisor, reconnect, shutdown
  - `internal/consume/runner.go` — extracted from `coyote.go`
  - `internal/store/` — SQLite open/schema/insert
  - `connect.go` + `auth/` — refactored to `Connect(Config{...})`

## CLI surface

```text
coyote consume --url U --exchange k=v [--oauth --redirect-url R --insecure --queue Q --store F --silent]
coyote list [--json]                         # ps = hidden alias
coyote stop|pause|resume|start <task-id>
coyote rm <task-id> [--purge] [--delete-queue|--keep-queue|--yes]
coyote logs <task-id> [--tail N]             # -f/SSE → Phase 2
coyote fetch <task-id> [--limit 100 --offset 0 --json --store F]  # messages = alias
coyote daemon [start --foreground | stop | status]  # hidden
coyote version
```

Backward compat: root `Action` stays `consume` with a deprecation warning on
bare `coyote --url ...` usage. Keeps existing help/version godog tests green
(after expected-output update). Also fixes ghost `--noprompt` in usage text.

## Daemon API (`/v1`, HTTP-over-Unix-socket)

| Method | Path | Notes |
|---|---|---|
| `POST` | `/v1/tasks` | `CreateTask{url,password/oauthToken,oauth,redirectUrl,insecure,exchanges,queue,store,silent}` → `201 Task` |
| `GET` | `/v1/tasks`, `/v1/tasks/{id}` | `Task{id,url(redacted),exchanges,queue,store,silent,status,createdAt,stats{receivedCount,lastMessageAt,error}}` |
| `POST` | `/v1/tasks/{id}/{start\|stop\|pause\|resume}` | `409` on illegal transition |
| `DELETE` | `/v1/tasks/{id}` | stop + optional purge |
| `GET` | `/v1/tasks/{id}/messages?limit&offset` | paged `Event[]`, served by daemon |
| `GET` | `/v1/tasks/{id}/logs?tail=N` | file tail; follow/SSE deferred |
| `POST` | `/v1/shutdown` | graceful daemon stop |

Errors: `{"error","cause"}` envelope mapped from `error/failed.go`.
Each CLI command is a thin HTTP client over the Unix socket.

## Storage

- **MVP:** keep per-task `--store` SQLite files, daemon is sole writer (avoids
  `_txlock=exclusive` contention). `fetch` reads through the daemon, never by
  opening the DB directly. No schema migration.
- **Phase 2:** central `~/.config/coyote/messages.db` (+ `task_id`) with unified
  `fetch`; `consume --store` remains as legacy export override.
- `fetch` = `GET /messages` pagination (`limit`/`offset`).

## Auth, queue lifecycle, observability

- **Auth (blocker resolved):** `auth/` + `connect()` currently take
  `*cli.Command` and prompt/open a browser — impossible detached. Extract
  `Connect(Config)`; CLI resolves password/OAuth at submit time (foreground
  only in MVP, no token refresh; secret sent once over `0600` socket, held in
  RAM only, never persisted/returned, URL redacted in `GET`).
- **Queue lifecycle:** `promptToDeletePersistentQueue` (interactive) becomes
  non-interactive `--yes/--keep-queue/--delete-queue`. `pause` = cancel
  consumer, keep queue; `resume` = redeclare+rebind; `stop` = cancel (+opt
  `QueueDelete`). Ephemeral vs persistent semantics preserved.
- **Logs:** per-task ring (last ~1000) + `~/.config/coyote/tasks/<id>.log`; `tail`
  in MVP, streaming follow later.

## Phases

**Phase 1 (MVP):** package split, socket + auto-start, supervisor, all CLI
verbs above, auth extraction, signal + queue-lifecycle fixes, per-task store.
**Phase 2 (deferred):** central DB + unified fetch, SSE `logs -f`, OAuth
refresh, log rotation, Windows named-pipe support.

## Tests

- Godog: `daemon.feature` (start→consume→ps→fetch→stop→rm), `compat.feature`
  (bare flags still foreground), `fetch.feature` (pagination, concurrent
  write+read).
- Unit (`httptest` + temp socket dir): handler table tests (400/404/409,
  URL redaction, pause/resume transitions, messages pagination); client
  auto-start on `ECONNREFUSED`.

## Risks

- Stale socket/pid on crash → unlink-on-failed-dial + respawn; consider flock.
- Secret over socket → mitigated by `0700`/`0600` perms.
- OAuth detached flow unsupported in MVP (documented).
- SQLite locking → daemon-only writes in MVP.
