package consume

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	failed "github.com/ghokun/coyote/error"
	amqp "github.com/rabbitmq/amqp091-go"
	_ "modernc.org/sqlite"
)

// Spec describes one consume task. URL must be fully resolved (secret
// embedded); the daemon reconstructs it from redacted URL + RAM-only secret.
type Spec struct {
	URL       string
	Insecure  bool
	Exchanges map[string]string
	Queue     string // requested name; empty => runner assigns coyote.<uuid>
	Store     string // sqlite path; empty => no persistence
	Silent    bool
}

// Hooks lets the supervisor observe progress without the runner logging
// directly. All hooks must be non-blocking and goroutine-safe.
type Hooks struct {
	OnMessage func(d amqp.Delivery)
	OnLog     func(format string, args ...any)
}

// Runner executes Spec until ctx is done. It returns nil on clean
// cancellation and a descriptive error otherwise — never calls log.Fatal or
// os.Exit, so one failing task cannot kill the daemon.
type Runner struct{}

func (Runner) Run(ctx context.Context, spec Spec, hooks Hooks) error {
	if len(spec.Exchanges) == 0 {
		return failed.Because("no exchanges to consume from", nil)
	}
	logf := hooks.OnLog
	if logf == nil {
		logf = func(string, ...any) {}
	}
	onMsg := hooks.OnMessage
	if onMsg == nil {
		onMsg = func(amqp.Delivery) {}
	}

	queueName := spec.Queue
	if queueName == "" {
		queueName = fmt.Sprintf("coyote.ephemeral-%d", time.Now().UnixNano())
	}
	persistent := spec.Queue != ""

	backoff := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if attempt > len(backoff) {
			if lastErr == nil {
				lastErr = failed.Because("too many connection attempts", nil)
			}
			return lastErr
		}
		if attempt > 0 {
			wait := backoff[attempt-1]
			logf("reconnecting in %s (attempt %d)", wait, attempt)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
		}
		lastErr = runOnce(ctx, spec, queueName, persistent, logf, onMsg)
		if lastErr == nil {
			return nil // clean shutdown via ctx
		}
		if ctx.Err() != nil {
			return nil
		}
		logf("connection lost: %v", lastErr)
	}
}

func runOnce(ctx context.Context, spec Spec, queueName string, persistent bool, logf func(string, ...any), onMsg func(amqp.Delivery)) error {
	conn, err := dial(spec.URL, spec.Insecure)
	if err != nil {
		return err
	}
	defer conn.Close()
	closeCh := make(chan *amqp.Error, 1)
	conn.NotifyClose(closeCh)

	ch, err := conn.Channel()
	if err != nil {
		return failed.Because("failed to open a channel:", err)
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(queueName, false, !persistent, !persistent, false, nil)
	if err != nil {
		return failed.Because("failed to declare a queue:", err)
	}
	for exchange, routingKey := range spec.Exchanges {
		if err := ch.ExchangeDeclarePassive(exchange, "topic", true, false, false, false, nil); err != nil {
			return failed.Because("failed to connect to exchange:", err)
		}
		if err := ch.QueueBind(q.Name, routingKey, exchange, false, nil); err != nil {
			return failed.Because("failed to bind to queue:", err)
		}
		logf("listening exchange=%s routing-key=%s queue=%s", exchange, routingKey, q.Name)
	}

	deliveries, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		return failed.Because("failed to register a consumer:", err)
	}

	var db *sql.DB
	var insert *sql.Stmt
	if spec.Store != "" {
		db, err = sql.Open("sqlite", spec.Store+"?_txlock=exclusive&mode=rwc")
		if err != nil {
			return failed.Because("failed to open store:", err)
		}
		if _, err := db.Exec(schemaSQL); err != nil {
			_ = db.Close()
			return failed.Because("failed to create event table:", err)
		}
		insert, err = db.Prepare(insertSQL)
		if err != nil {
			_ = db.Close()
			return failed.Because("failed to prepare insert:", err)
		}
		defer func() {
			_ = insert.Close()
			_ = db.Close()
		}()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case amqpErr, ok := <-closeCh:
			if !ok {
				return failed.Because("amqp connection closed", nil)
			}
			if amqpErr != nil {
				return failed.Because("amqp connection closed:", amqpErr)
			}
			return failed.Because("amqp connection closed", nil)
		case d, ok := <-deliveries:
			if !ok {
				return failed.Because("consumer channel closed", nil)
			}
			if insert != nil {
				if _, err := insert.Exec(d.Exchange, d.RoutingKey, d.CorrelationId, d.ReplyTo, fmt.Sprint(d.Headers), string(d.Body)); err != nil {
					return failed.Because("failed to store event:", err)
				}
			}
			onMsg(d)
		}
	}
}

const schemaSQL = `CREATE TABLE IF NOT EXISTS event
(
  "id"             INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  "timestamp"      TIMESTAMP DEFAULT (DATETIME(CURRENT_TIMESTAMP, 'localtime')),
  "exchange"       TEXT,
  "routing_key"    TEXT,
  "correlation_id" TEXT,
  "reply_to"       TEXT,
  "headers"        TEXT,
  "body"           TEXT
);`

const insertSQL = `INSERT INTO event(exchange, routing_key, correlation_id, reply_to, headers, body)
VALUES (?, ?, ?, ?, ?, ?)`
