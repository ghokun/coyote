package ipc_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghokun/coyote/internal/api"
	"github.com/ghokun/coyote/internal/ipc"
)

type stubService struct {
	tasks    map[string]api.Task
	shutdown bool
}

func newStub() *stubService {
	return &stubService{tasks: map[string]api.Task{}}
}

func (s *stubService) Create(req api.CreateTaskRequest) (api.Task, error) {
	if req.URL == "" {
		return api.Task{}, api.ErrBadRequest("url is required")
	}
	t := api.Task{ID: "task-1", URL: api.RedactURL(req.URL), Exchanges: req.Exchanges, Queue: req.Queue, Status: api.StatusRunning}
	s.tasks[t.ID] = t
	return t, nil
}

func (s *stubService) Get(id string) (api.Task, bool) {
	t, ok := s.tasks[id]
	return t, ok
}

func (s *stubService) List() []api.Task {
	out := make([]api.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

func (s *stubService) Transition(id, action string) (api.Task, error) {
	t, ok := s.tasks[id]
	if !ok {
		return api.Task{}, api.ErrNotFound(id)
	}
	switch action {
	case "pause":
		if t.Status != api.StatusRunning {
			return api.Task{}, api.ErrConflict("cannot pause")
		}
		t.Status = api.StatusPaused
	default:
		return api.Task{}, api.ErrBadRequest("unknown action")
	}
	s.tasks[id] = t
	return t, nil
}

func (s *stubService) Delete(id string, _, _ bool) (api.Task, error) {
	t, ok := s.tasks[id]
	if !ok {
		return api.Task{}, api.ErrNotFound(id)
	}
	delete(s.tasks, id)
	return t, nil
}

func (s *stubService) Messages(id string, _, _ int) (int, []api.Message, error) {
	if _, ok := s.tasks[id]; !ok {
		return 0, nil, api.ErrNotFound(id)
	}
	return 1, []api.Message{{ID: 1, Body: "hi"}}, nil
}

func (s *stubService) Logs(id string, _ int) ([]string, error) {
	if _, ok := s.tasks[id]; !ok {
		return nil, api.ErrNotFound(id)
	}
	return []string{"line1"}, nil
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(buf)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateAndGet(t *testing.T) {
	h := ipc.NewHandler(newStub(), nil)
	rec := do(t, h, http.MethodPost, "/v1/tasks", api.CreateTaskRequest{URL: "amqps://u:p@host", Exchanges: map[string]string{"e": "#"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created api.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.URL != "amqps://u@host" {
		t.Fatalf("URL not redacted: %s", created.URL)
	}
	rec = do(t, h, http.MethodGet, "/v1/tasks/task-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestStatusCodes(t *testing.T) {
	h := ipc.NewHandler(newStub(), nil)
	if rec := do(t, h, http.MethodPost, "/v1/tasks", api.CreateTaskRequest{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad create code = %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/v1/tasks/missing", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing code = %d", rec.Code)
	}
	stub := newStub()
	h2 := ipc.NewHandler(stub, nil)
	do(t, h2, http.MethodPost, "/v1/tasks", api.CreateTaskRequest{URL: "amqps://u@h", Exchanges: map[string]string{"e": "#"}})
	// Pause a freshly created (running) task: need running status; stub creates running.
	if rec := do(t, h2, http.MethodPost, "/v1/tasks/task-1/pause", nil); rec.Code != http.StatusOK {
		t.Fatalf("pause code = %d, body=%s", rec.Code, rec.Body.String())
	}
	// Pausing again is a conflict.
	if rec := do(t, h2, http.MethodPost, "/v1/tasks/task-1/pause", nil); rec.Code != http.StatusConflict {
		t.Fatalf("second pause code = %d", rec.Code)
	}
}

func TestShutdownHook(t *testing.T) {
	called := make(chan struct{}, 1)
	h := ipc.NewHandler(newStub(), func() { called <- struct{}{} })
	if rec := do(t, h, http.MethodPost, "/v1/shutdown", nil); rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case <-called:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown hook not invoked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
