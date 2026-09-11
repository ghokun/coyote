package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	failed "github.com/ghokun/coyote/error"
	"github.com/ghokun/coyote/internal/api"
	"github.com/ghokun/coyote/internal/consume"
	"github.com/ghokun/coyote/internal/paths"
	"github.com/ghokun/coyote/internal/store"
	amqp "github.com/rabbitmq/amqp091-go"
)

const maxRingLines = 1000

// persistedTask is the daemon.db row shape. Secrets are never stored.
type persistedTask struct {
	ID            string            `json:"id"`
	URL           string            `json:"url"` // redacted
	OAuth         bool              `json:"oauth"`
	RedirectURL   string            `json:"redirectUrl,omitempty"`
	Insecure      bool              `json:"insecure"`
	Exchanges     map[string]string `json:"exchanges"`
	Queue         string            `json:"queue"`
	QueueExplicit bool              `json:"queueExplicit"`
	Store         string            `json:"store,omitempty"`
	Silent        bool              `json:"silent"`
	Status        api.TaskStatus    `json:"status"`
	CreatedAt     time.Time         `json:"createdAt"`
	Stats         api.TaskStats     `json:"stats"`
}

type managedTask struct {
	spec    persistedTask
	secret  string // RAM only, never persisted
	cancel  context.CancelFunc
	logFile *os.File
	ring    []string
}

type runFuncT func(ctx context.Context, spec consume.Spec, hooks consume.Hooks) error

// Supervisor owns all daemon tasks. It is safe for concurrent use by HTTP
// handlers. stats/ring/log writes happen under mu; steady-state message
// throughput holds the lock only briefly.
type Supervisor struct {
	mu      sync.Mutex
	tasks   map[string]*managedTask
	db      *sql.DB
	runFunc runFuncT
}

// Open loads daemon.db (creating it) and returns a supervisor with no
// running tasks. Tasks that were running when the daemon last exited are
// marked failed: secrets live in RAM only, so they cannot be resumed.
func Open() (*Supervisor, error) {
	if err := paths.EnsureConfigDir(); err != nil {
		return nil, failed.Because("failed to create config dir:", err)
	}
	db, err := sql.Open("sqlite", paths.DaemonDBPath()+"?_txlock=exclusive&mode=rwc")
	if err != nil {
		return nil, failed.Because("failed to open daemon db:", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, data TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, failed.Because("failed to create tasks table:", err)
	}
	s := &Supervisor{tasks: map[string]*managedTask{}, db: db, runFunc: consume.Runner{}.Run}
	rows, err := db.Query(`SELECT data FROM tasks`)
	if err != nil {
		_ = db.Close()
		return nil, failed.Because("failed to load tasks:", err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			_ = db.Close()
			return nil, failed.Because("failed to scan task:", err)
		}
		var p persistedTask
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			continue // skip corrupt rows, keep daemon bootable
		}
		if p.Status == api.StatusRunning || p.Status == api.StatusPaused {
			p.Status = api.StatusFailed
			p.Stats.Error = "daemon restarted; credentials are not persisted — re-submit with consume"
		}
		s.tasks[p.ID] = &managedTask{spec: p}
		if err := rows.Err(); err != nil {
			break
		}
	}
	// Persist any restart transitions so a second boot is stable.
	for _, t := range s.tasks {
		_ = s.saveLocked(t)
	}
	return s, nil
}

func (s *Supervisor) saveLocked(t *managedTask) error {
	data, err := json.Marshal(t.spec)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO tasks(id, data) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, t.spec.ID, string(data))
	return err
}

// Create validates req, persists a redacted spec, and starts the task.
func (s *Supervisor) Create(req api.CreateTaskRequest) (api.Task, error) {
	if strings.TrimSpace(req.URL) == "" {
		return api.Task{}, failed.Because("url is required", nil)
	}
	if len(req.Exchanges) == 0 {
		return api.Task{}, failed.Because("at least one exchange is required", nil)
	}
	redacted, secret := api.SplitSecret(req.URL)
	if req.Password != "" {
		secret = req.Password
		redacted = api.RedactURL(req.URL)
	}
	id := uuid.NewString()
	queue := req.Queue
	queueExplicit := req.Queue != ""
	if queue == "" {
		queue = "coyote." + id
	}
	storePath := req.Store
	if storePath != "" && !filepath.IsAbs(storePath) {
		if abs, err := filepath.Abs(storePath); err == nil {
			storePath = abs
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := paths.EnsureDir(paths.TaskDir(id)); err != nil {
		return api.Task{}, failed.Because("failed to create task dir:", err)
	}
	if storePath != "" {
		// Create the store (and its schema) up front so fetch works even
		// before the first successful broker connection, and so a bad
		// --store path fails fast at submit time instead of in the runner.
		st, err := store.Open(storePath)
		if err != nil {
			return api.Task{}, err
		}
		if err := st.Close(); err != nil {
			return api.Task{}, failed.Because("failed to close store:", err)
		}
	}
	lf, err := os.OpenFile(paths.TaskLogPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return api.Task{}, failed.Because("failed to open task log:", err)
	}
	t := &managedTask{
		spec: persistedTask{
			ID: id, URL: redacted, OAuth: req.OAuth, RedirectURL: req.RedirectURL,
			Insecure: req.Insecure, Exchanges: req.Exchanges, Queue: queue,
			QueueExplicit: queueExplicit,
			Store:         storePath, Silent: req.Silent,
			Status: api.StatusRunning, CreatedAt: time.Now().UTC(),
		},
		secret:  secret,
		logFile: lf,
	}
	if err := s.saveLocked(t); err != nil {
		_ = lf.Close()
		return api.Task{}, failed.Because("failed to persist task:", err)
	}
	s.tasks[id] = t
	s.startLocked(t)
	return toAPI(t), nil
}

func (s *Supervisor) startLocked(t *managedTask) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.spec.Status = api.StatusRunning
	t.spec.Stats.Error = ""
	spec := consume.Spec{
		URL: api.RebuildURL(t.spec.URL, t.secret), Insecure: t.spec.Insecure,
		Exchanges: t.spec.Exchanges, Queue: t.spec.Queue,
		Store: t.spec.Store, Silent: t.spec.Silent,
	}
	id := t.spec.ID
	go func() {
		err := s.runFunc(ctx, spec, consume.Hooks{
			OnMessage: func(d amqp.Delivery) { s.onMessage(id, d) },
			OnLog:     func(f string, a ...any) { s.onLog(id, f, a...) },
		})
		if err != nil {
			s.mu.Lock()
			defer s.mu.Unlock()
			if cur, ok := s.tasks[id]; ok && cur == t {
				// Only record failure if the task wasn't cleanly paused/stopped
				// after this run started (status still running).
				if t.spec.Status == api.StatusRunning {
					t.spec.Status = api.StatusFailed
					t.spec.Stats.Error = err.Error()
					_ = s.saveLocked(t)
					s.appendLocked(t, "task failed: "+err.Error())
				}
			}
		}
	}()
	_ = s.saveLocked(t)
	s.appendLocked(t, "task started")
}

func (s *Supervisor) onMessage(id string, d amqp.Delivery) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return
	}
	t.spec.Stats.ReceivedCount++
	now := time.Now().UTC()
	t.spec.Stats.LastMessageAt = &now
	line := fmt.Sprintf("%s exchange=%s routing-key=%s correlation-id=%s reply-to=%s headers=%v body=%s",
		now.Format(time.RFC3339), d.Exchange, d.RoutingKey, d.CorrelationId, d.ReplyTo, d.Headers, string(d.Body))
	s.appendLocked(t, line)
}

func (s *Supervisor) onLog(id, format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		s.appendLocked(t, fmt.Sprintf(format, args...))
	}
}

func (s *Supervisor) appendLocked(t *managedTask, line string) {
	t.ring = append(t.ring, line)
	if len(t.ring) > maxRingLines {
		t.ring = t.ring[len(t.ring)-maxRingLines:]
	}
	if t.logFile != nil {
		_, _ = fmt.Fprintln(t.logFile, line)
	}
}

// Get returns a snapshot of one task.
func (s *Supervisor) Get(id string) (api.Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return api.Task{}, false
	}
	return toAPI(t), true
}

// List returns snapshots of all tasks, oldest first.
func (s *Supervisor) List() []api.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]api.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, toAPI(t))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func toAPI(t *managedTask) api.Task {
	return api.Task{
		ID: t.spec.ID, URL: t.spec.URL, Exchanges: t.spec.Exchanges,
		Queue: t.spec.Queue, QueueExplicit: t.spec.QueueExplicit,
		Store: t.spec.Store, Silent: t.spec.Silent,
		Status: t.spec.Status, CreatedAt: t.spec.CreatedAt, Stats: t.spec.Stats,
	}
}

// Transition moves a task between lifecycle states:
// pause (running->paused), stop (running/paused->stopped),
// resume (paused->running), start (stopped/failed->running).
func (s *Supervisor) Transition(id string, action string) (api.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return api.Task{}, errNotFound(id)
	}
	switch action {
	case "pause":
		if t.spec.Status != api.StatusRunning {
			return api.Task{}, errConflict(fmt.Sprintf("cannot pause task in status %q", t.spec.Status))
		}
		if t.cancel != nil {
			t.cancel()
		}
		t.spec.Status = api.StatusPaused
		s.appendLocked(t, "task paused")
	case "stop":
		if t.spec.Status != api.StatusRunning && t.spec.Status != api.StatusPaused {
			return api.Task{}, errConflict(fmt.Sprintf("cannot stop task in status %q", t.spec.Status))
		}
		if t.cancel != nil {
			t.cancel()
		}
		t.spec.Status = api.StatusStopped
		s.appendLocked(t, "task stopped")
	case "resume":
		if t.spec.Status != api.StatusPaused {
			return api.Task{}, errConflict(fmt.Sprintf("cannot resume task in status %q", t.spec.Status))
		}
		if t.secret == "" {
			return api.Task{}, errConflict("credentials are not available after daemon restart — re-submit with consume")
		}
		s.startLocked(t)
		s.appendLocked(t, "task resumed")
		return toAPI(t), s.saveLocked(t)
	case "start":
		if t.spec.Status != api.StatusStopped && t.spec.Status != api.StatusFailed {
			return api.Task{}, errConflict(fmt.Sprintf("cannot start task in status %q", t.spec.Status))
		}
		if t.secret == "" {
			return api.Task{}, errConflict("credentials are not available after daemon restart — re-submit with consume")
		}
		s.startLocked(t)
		return toAPI(t), s.saveLocked(t)
	default:
		return api.Task{}, failed.Because("unknown action "+action, nil)
	}
	return toAPI(t), s.saveLocked(t)
}

// Delete stops the task, optionally deletes the AMQP queue (best-effort when
// credentials are in RAM) and the store file, then removes the record.
func (s *Supervisor) Delete(id string, deleteQueue, purge bool) (api.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return api.Task{}, errNotFound(id)
	}
	if t.cancel != nil {
		t.cancel()
	}
	var warnings []string
	if deleteQueue {
		if t.secret == "" {
			return api.Task{}, errConflict("cannot delete queue: credentials are not available after daemon restart")
		}
		if err := consume.DeleteQueue(api.RebuildURL(t.spec.URL, t.secret), t.spec.Insecure, t.spec.Queue); err != nil {
			warnings = append(warnings, "queue delete failed: "+err.Error())
		}
	}
	if purge && t.spec.Store != "" {
		if err := os.Remove(t.spec.Store); err != nil && !os.IsNotExist(err) {
			warnings = append(warnings, "purge failed: "+err.Error())
		}
	}
	snap := toAPI(t)
	snap.Status = api.StatusStopped
	if len(warnings) > 0 {
		snap.Stats.Error = strings.Join(warnings, "; ")
	}
	if _, err := s.db.Exec(`DELETE FROM tasks WHERE id=?`, id); err != nil {
		return api.Task{}, failed.Because("failed to delete task:", err)
	}
	if t.logFile != nil {
		_ = t.logFile.Close()
	}
	delete(s.tasks, id)
	return snap, nil
}

// Messages pages the task's SQLite store through the daemon.
func (s *Supervisor) Messages(id string, q api.MessagesQuery) (int, []api.Message, error) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return 0, nil, errNotFound(id)
	}
	storePath := t.spec.Store
	s.mu.Unlock()
	if storePath == "" {
		return 0, nil, api.ErrBadRequest("task has no store configured")
	}
	return store.QueryFile(storePath, q)
}

// Logs returns the last tail lines of the task log.
func (s *Supervisor) Logs(id string, tail int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errNotFound(id)
	}
	if tail <= 0 {
		tail = 100
	}
	if data, err := os.ReadFile(paths.TaskLogPath(id)); err == nil {
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			return []string{}, nil
		}
		if len(lines) > tail {
			lines = lines[len(lines)-tail:]
		}
		return lines, nil
	}
	// Fall back to the in-memory ring (e.g. task created but nothing flushed).
	ring := t.ring
	if len(ring) > tail {
		ring = ring[len(ring)-tail:]
	}
	out := make([]string, len(ring))
	copy(out, ring)
	return out, nil
}

// ShutdownAll cancels every running task and persists stopped state.
func (s *Supervisor) ShutdownAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		if t.spec.Status == api.StatusRunning || t.spec.Status == api.StatusPaused {
			t.spec.Status = api.StatusStopped
			s.appendLocked(t, "daemon shutting down")
			_ = s.saveLocked(t)
		}
		if t.logFile != nil {
			_ = t.logFile.Close()
			t.logFile = nil
		}
	}
}

// Close shuts down tasks and the daemon db.
func (s *Supervisor) Close() error {
	s.ShutdownAll()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

type notFoundError = api.NotFoundError

func errNotFound(id string) error { return api.ErrNotFound(id) }

// ErrNotFound builds a missing-task error (maps to 404 over HTTP).
func ErrNotFound(id string) error { return errNotFound(id) }

type conflictError = api.ConflictError

func errConflict(msg string) error { return api.ErrConflict(msg) }

// IsNotFound reports whether err is a missing-task error (maps to 404).
func IsNotFound(err error) bool { return api.IsNotFound(err) }

// IsConflict reports whether err is an illegal-transition error (maps to 409).
func IsConflict(err error) bool { return api.IsConflict(err) }
