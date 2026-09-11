package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	failed "github.com/ghokun/coyote/error"
	"github.com/ghokun/coyote/internal/api"
	"github.com/ghokun/coyote/internal/paths"
)

// Client talks to the daemon over the unix socket.
type Client struct {
	http *http.Client
}

// NewClient dials paths.SocketPath() with a short timeout.
func NewClient() *Client {
	return &Client{http: &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 2 * time.Second}
				return d.DialContext(ctx, "unix", paths.SocketPath())
			},
		},
	}}
}

func (c *Client) do(method, path string, query url.Values, body any, out any, okCodes ...int) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return failed.Because("failed to encode request:", err)
		}
		rdr = bytes.NewReader(buf)
	}
	u := "http://coyote" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, rdr)
	if err != nil {
		return failed.Because("failed to build request:", err)
	}
	req.Method = method
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return failed.Because("daemon is not reachable:", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return failed.Because("failed to read daemon response:", err)
	}
	ok := false
	if len(okCodes) == 0 {
		ok = resp.StatusCode >= 200 && resp.StatusCode < 300
	} else {
		for _, code := range okCodes {
			if resp.StatusCode == code {
				ok = true
			}
		}
	}
	if !ok {
		var env api.ErrorEnvelope
		if json.Unmarshal(data, &env) == nil && env.Error != "" {
			return fmt.Errorf("daemon error (HTTP %d): %s", resp.StatusCode, env.Error)
		}
		return fmt.Errorf("daemon error: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return failed.Because("failed to decode daemon response:", err)
		}
	}
	return nil
}

func (c *Client) CreateTask(req api.CreateTaskRequest) (api.Task, error) {
	var t api.Task
	return t, c.do(http.MethodPost, "/v1/tasks", nil, req, &t, http.StatusCreated)
}

func (c *Client) List() ([]api.Task, error) {
	var r api.TasksResponse
	if err := c.do(http.MethodGet, "/v1/tasks", nil, nil, &r); err != nil {
		return nil, err
	}
	return r.Tasks, nil
}

func (c *Client) Get(id string) (api.Task, error) {
	var t api.Task
	return t, c.do(http.MethodGet, "/v1/tasks/"+id, nil, nil, &t)
}

func (c *Client) Action(id, action string) (api.Task, error) {
	var t api.Task
	return t, c.do(http.MethodPost, "/v1/tasks/"+id+"/"+action, nil, nil, &t)
}

func (c *Client) Delete(id string, deleteQueue, purge bool) (api.Task, error) {
	var t api.Task
	q := url.Values{}
	if deleteQueue {
		q.Set("deleteQueue", "true")
	}
	if purge {
		q.Set("purge", "true")
	}
	return t, c.do(http.MethodDelete, "/v1/tasks/"+id, q, nil, &t)
}

func (c *Client) Messages(id string, limit, offset int) (api.MessagesResponse, error) {
	var r api.MessagesResponse
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	return r, c.do(http.MethodGet, "/v1/tasks/"+id+"/messages", q, nil, &r)
}

type LogsResponse struct {
	Lines []string `json:"lines"`
}

func (c *Client) Logs(id string, tail int) ([]string, error) {
	var r LogsResponse
	q := url.Values{}
	if tail > 0 {
		q.Set("tail", strconv.Itoa(tail))
	}
	if err := c.do(http.MethodGet, "/v1/tasks/"+id+"/logs", q, nil, &r); err != nil {
		return nil, err
	}
	return r.Lines, nil
}

func (c *Client) Shutdown() error {
	var r map[string]any
	return c.do(http.MethodPost, "/v1/shutdown", nil, nil, &r)
}

// Probe reports whether a daemon answers on the socket.
func Probe() bool {
	c := NewClient()
	c.http.Timeout = 2 * time.Second
	var r map[string]any
	return c.do(http.MethodGet, "/v1/status", nil, nil, &r) == nil
}
