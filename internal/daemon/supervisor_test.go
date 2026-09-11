package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ghokun/coyote/internal/api"
	"github.com/ghokun/coyote/internal/consume"
)

func testEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// blockRunner simulates a healthy task: runs until cancelled.
func blockRunner(ctx context.Context, _ consume.Spec, _ consume.Hooks) error {
	<-ctx.Done()
	return nil
}

func openWithRunner(t *testing.T, fn runFuncT) *Supervisor {
	t.Helper()
	testEnv(t)
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.mu.Lock()
	s.runFunc = fn
	s.mu.Unlock()
	return s
}

func createReq() api.CreateTaskRequest {
	return api.CreateTaskRequest{
		URL:       "amqps://user:s3cret@host/vh",
		Exchanges: map[string]string{"myex": "#"},
	}
}

func TestCreateRedactsSecret(t *testing.T) {
	s := openWithRunner(t, blockRunner)
	task, err := s.Create(createReq())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(task.URL, "s3cret") {
		t.Fatalf("secret leaked in task URL: %s", task.URL)
	}
	if task.Status != api.StatusRunning {
		t.Fatalf("status = %s, want running", task.Status)
	}
	if task.Queue == "" {
		t.Fatal("expected generated queue name")
	}
}

func TestLifecycleTransitions(t *testing.T) {
	s := openWithRunner(t, blockRunner)
	task, err := s.Create(createReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action string
		want   api.TaskStatus
	}{
		{"pause", api.StatusPaused},
		{"resume", api.StatusRunning},
		{"stop", api.StatusStopped},
	} {
		got, err := s.Transition(task.ID, tc.action)
		if err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if got.Status != tc.want {
			t.Fatalf("%s: status = %s, want %s", tc.action, got.Status, tc.want)
		}
	}
	if _, err := s.Transition(task.ID, "pause"); err == nil {
		t.Fatal("expected conflict pausing a stopped task")
	} else if !IsConflict(err) {
		t.Fatalf("expected conflict error, got %v", err)
	}
	got, err := s.Transition(task.ID, "start")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if got.Status != api.StatusRunning {
		t.Fatalf("start: status = %s, want running", got.Status)
	}
	if _, err := s.Transition(task.ID, "bogus"); err == nil {
		t.Fatal("expected error for unknown action")
	}
	if _, err := s.Transition("nope", "stop"); !IsNotFound(err) {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestFailingTaskDoesNotKillSiblings(t *testing.T) {
	// Regression test for the review finding: a task whose runner returns an
	// error must be marked failed without affecting other tasks (no log.Fatal).
	calls := 0
	s := openWithRunner(t, func(ctx context.Context, spec consume.Spec, hooks consume.Hooks) error {
		calls++
		if strings.Contains(spec.Queue, "bad") {
			return errConflictForTest("boom: storage unavailable")
		}
		return blockRunner(ctx, spec, hooks)
	})
	good, err := s.Create(api.CreateTaskRequest{URL: "amqps://u:p@h", Exchanges: map[string]string{"e": "#"}, Queue: "good"})
	if err != nil {
		t.Fatal(err)
	}
	badReq := createReq()
	badReq.Queue = "bad-queue"
	bad, err := s.Create(badReq)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the failing goroutine to report.
	deadline := time.Now().Add(5 * time.Second)
	for {
		g, ok := s.Get(bad.ID)
		if ok && g.Status == api.StatusFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failing task never reached failed status")
		}
		time.Sleep(10 * time.Millisecond)
	}
	g, _ := s.Get(good.ID)
	if g.Status != api.StatusRunning {
		t.Fatalf("sibling status = %s, want running", g.Status)
	}
	if calls < 2 {
		t.Fatalf("expected both runners invoked, calls=%d", calls)
	}
}

func TestSecretsNotPersistedAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.runFunc = blockRunner
	s.mu.Unlock()
	task, err := s.Create(createReq())
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash: close the db without ShutdownAll so the row still
	// says running (a graceful Close would persist stopped instead).
	_ = s.db.Close()

	s2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, ok := s2.Get(task.ID)
	if !ok {
		t.Fatal("task missing after restart")
	}
	if got.Status != api.StatusFailed {
		t.Fatalf("status after restart = %s, want failed", got.Status)
	}
	if _, err := s2.Transition(task.ID, "start"); !IsConflict(err) {
		t.Fatalf("expected conflict restarting without credentials, got %v", err)
	}
}

func TestMessagesRequiresStore(t *testing.T) {
	s := openWithRunner(t, blockRunner)
	task, err := s.Create(createReq())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Messages(task.ID, api.MessagesQuery{Limit: 10}); err == nil {
		t.Fatal("expected error for task without store")
	}
}

func TestLogsTail(t *testing.T) {
	s := openWithRunner(t, blockRunner)
	task, err := s.Create(createReq())
	if err != nil {
		t.Fatal(err)
	}
	lines, err := s.Logs(task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("expected at least the 'task started' line")
	}
}

func errConflictForTest(msg string) error { return api.ErrConflict(msg) }
