package api

import (
	"net/url"
	"time"
)

// TaskStatus is the lifecycle state of a daemon task.
type TaskStatus string

const (
	StatusRunning TaskStatus = "running"
	StatusPaused  TaskStatus = "paused"
	StatusStopped TaskStatus = "stopped"
	StatusFailed  TaskStatus = "failed"
)

// TaskStats holds runtime counters surfaced by list/get.
type TaskStats struct {
	ReceivedCount int64      `json:"receivedCount"`
	LastMessageAt *time.Time `json:"lastMessageAt,omitempty"`
	Error         string     `json:"error,omitempty"`
}

// CreateTaskRequest is POST /v1/tasks. Secret travels once over the
// 0600 unix socket and is held in daemon RAM only — never persisted.
type CreateTaskRequest struct {
	URL         string            `json:"url"` // may embed secret; daemon redacts before persisting
	Password    string            `json:"password,omitempty"`
	OAuth       bool              `json:"oauth"`
	RedirectURL string            `json:"redirectUrl,omitempty"`
	Insecure    bool              `json:"insecure"`
	Exchanges   map[string]string `json:"exchanges"`
	Queue       string            `json:"queue,omitempty"`
	Store       string            `json:"store,omitempty"`
	Silent      bool              `json:"silent"`
}

// Task is the daemon's external view. Secrets are never included.
type Task struct {
	ID            string            `json:"id"`
	URL           string            `json:"url"` // redacted, no password
	Exchanges     map[string]string `json:"exchanges"`
	Queue         string            `json:"queue"`
	QueueExplicit bool              `json:"queueExplicit,omitempty"`
	Store         string            `json:"store,omitempty"`
	Silent        bool              `json:"silent"`
	Status        TaskStatus        `json:"status"`
	CreatedAt     time.Time         `json:"createdAt"`
	Stats         TaskStats         `json:"stats"`
}

// ErrorEnvelope is the JSON error shape for all daemon endpoints.
type ErrorEnvelope struct {
	Error string `json:"error"`
	Cause string `json:"cause,omitempty"`
}

// Message is one stored event row.
type Message struct {
	ID            int64  `json:"id"`
	Timestamp     string `json:"timestamp"`
	Exchange      string `json:"exchange"`
	RoutingKey    string `json:"routing_key"`
	CorrelationID string `json:"correlation_id"`
	ReplyTo       string `json:"reply_to"`
	Headers       string `json:"headers"`
	Body          string `json:"body"`
}

type MessagesResponse struct {
	Total    int       `json:"total"`
	Messages []Message `json:"messages"`
}

// MessageFilter constrains which stored messages are returned. Every
// non-empty field is a case-sensitive substring match; fields combine with
// AND. An empty filter matches everything.
type MessageFilter struct {
	Exchange      string `json:"exchange,omitempty"`
	RoutingKey    string `json:"routingKey,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
	ReplyTo       string `json:"replyTo,omitempty"`
	Headers       string `json:"headers,omitempty"`
	Body          string `json:"body,omitempty"`
}

// Active returns the names of the fields that constrain the query.
func (f MessageFilter) Active() []string {
	var out []string
	if f.Exchange != "" {
		out = append(out, "exchange")
	}
	if f.RoutingKey != "" {
		out = append(out, "routing-key")
	}
	if f.CorrelationID != "" {
		out = append(out, "correlation-id")
	}
	if f.ReplyTo != "" {
		out = append(out, "reply-to")
	}
	if f.Headers != "" {
		out = append(out, "headers")
	}
	if f.Body != "" {
		out = append(out, "body")
	}
	return out
}

// MessagesQuery is a paged, filtered read of a task's store. Total in the
// response reflects the filtered count so pagination stays coherent.
type MessagesQuery struct {
	Limit  int
	Offset int
	Filter MessageFilter
}

type TasksResponse struct {
	Tasks []Task `json:"tasks"`
}

// RedactURL strips any password from raw, keeping the username.
// e.g. amqps://user:secret@host/vh -> amqps://user@host/vh
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if name := u.User.Username(); name != "" {
		u.User = url.User(name)
	} else {
		u.User = nil
	}
	return u.String()
}

// SplitSecret splits a fully-resolved URL into redacted URL + secret.
// Secret is the password/userinfo token; empty when none embedded.
func SplitSecret(resolvedURL string) (redacted, secret string) {
	u, err := url.Parse(resolvedURL)
	if err != nil {
		return resolvedURL, ""
	}
	if u.User != nil {
		if pw, ok := u.User.Password(); ok {
			secret = pw
		}
	}
	return RedactURL(resolvedURL), secret
}

// RebuildURL injects secret back into a redacted URL.
func RebuildURL(redacted, secret string) string {
	if secret == "" {
		return redacted
	}
	u, err := url.Parse(redacted)
	if err != nil {
		return redacted
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), secret)
	}
	return u.String()
}
