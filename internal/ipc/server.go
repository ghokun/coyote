package ipc

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ghokun/coyote/internal/api"
)

// TaskService is the daemon surface exposed over HTTP. It is satisfied by
// daemon.Supervisor; defined here so ipc never imports daemon (daemon imports
// ipc for the serve loop — the reverse would be an import cycle).
type TaskService interface {
	Create(api.CreateTaskRequest) (api.Task, error)
	Get(string) (api.Task, bool)
	List() []api.Task
	Transition(id, action string) (api.Task, error)
	Delete(id string, deleteQueue, purge bool) (api.Task, error)
	Messages(id string, q api.MessagesQuery) (int, []api.Message, error)
	Logs(id string, tail int) ([]string, error)
}

// NewHandler wires a TaskService to HTTP-over-Unix-socket routes.
// onShutdown is invoked (in a goroutine, after the response is written) when
// POST /v1/shutdown arrives; the serve loop performs graceful teardown.
func NewHandler(s TaskService, onShutdown func()) http.Handler {
	mux := http.NewServeMux()
	writeErr := func(w http.ResponseWriter, err error) {
		env := api.ErrorEnvelope{Error: err.Error()}
		switch {
		case api.IsNotFound(err):
			w.WriteHeader(http.StatusNotFound)
		case api.IsConflict(err):
			w.WriteHeader(http.StatusConflict)
		case api.IsBadRequest(err) || strings.Contains(strings.ToLower(err.Error()), "required"):
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
		_ = json.NewEncoder(w).Encode(env)
	}
	writeJSON := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}

	mux.HandleFunc("POST /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		var req api.CreateTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, api.ErrBadRequest("invalid request body: "+err.Error()))
			return
		}
		task, err := s.Create(req)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, task)
	})
	mux.HandleFunc("GET /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, api.TasksResponse{Tasks: withEmpty(s.List())})
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tasks": len(s.List())})
	})
	mux.HandleFunc("GET /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		task, ok := s.Get(r.PathValue("id"))
		if !ok {
			writeErr(w, api.ErrNotFound(r.PathValue("id")))
			return
		}
		writeJSON(w, http.StatusOK, task)
	})
	mux.HandleFunc("POST /v1/tasks/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		task, err := s.Transition(r.PathValue("id"), r.PathValue("action"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, task)
	})
	mux.HandleFunc("DELETE /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		task, err := s.Delete(r.PathValue("id"), q.Get("deleteQueue") == "true", q.Get("purge") == "true")
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, task)
	})
	mux.HandleFunc("GET /v1/tasks/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		mq := api.MessagesQuery{
			Limit:  limit,
			Offset: offset,
			Filter: api.MessageFilter{
				Exchange:      q.Get("exchange"),
				RoutingKey:    q.Get("routing_key"),
				CorrelationID: q.Get("correlation_id"),
				ReplyTo:       q.Get("reply_to"),
				Headers:       q.Get("headers"),
				Body:          q.Get("body"),
			},
		}
		total, msgs, err := s.Messages(r.PathValue("id"), mq)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, api.MessagesResponse{Total: total, Messages: msgs})
	})
	mux.HandleFunc("GET /v1/tasks/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		lines, err := s.Logs(r.PathValue("id"), tail)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
	})
	mux.HandleFunc("POST /v1/shutdown", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		if onShutdown != nil {
			go onShutdown()
		}
	})
	return mux
}

func withEmpty(tasks []api.Task) []api.Task {
	if tasks == nil {
		return []api.Task{}
	}
	return tasks
}
