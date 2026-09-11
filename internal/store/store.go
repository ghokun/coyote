package store

import (
	"database/sql"

	failed "github.com/ghokun/coyote/error"
	"github.com/ghokun/coyote/internal/api"
	_ "modernc.org/sqlite"
)

const schema = `CREATE TABLE IF NOT EXISTS event
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

// Store is a thin wrapper around a per-task SQLite event DB.
// All methods return errors — callers (daemon tasks) must propagate them to
// the supervisor instead of calling log.Fatal.
type Store struct {
	db     *sql.DB
	insert *sql.Stmt
}

// Open creates/opens path (mode=rwc) and ensures the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_txlock=exclusive&mode=rwc")
	if err != nil {
		return nil, failed.Because("failed to open store:", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, failed.Because("failed to create event table:", err)
	}
	insert, err := db.Prepare(insertSQL)
	if err != nil {
		_ = db.Close()
		return nil, failed.Because("failed to prepare insert:", err)
	}
	return &Store{db: db, insert: insert}, nil
}

// Insert records one message.
func (s *Store) Insert(exchange, routingKey, correlationID, replyTo, headers, body string) error {
	if s == nil || s.insert == nil {
		return failed.Because("store is not open", nil)
	}
	if _, err := s.insert.Exec(exchange, routingKey, correlationID, replyTo, headers, body); err != nil {
		return failed.Because("failed to insert event:", err)
	}
	return nil
}

// Query returns total count plus one page (id ascending).
func (s *Store) Query(limit, offset int) (total int, msgs []api.Message, err error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&total); err != nil {
		return 0, nil, failed.Because("failed to count events:", err)
	}
	rows, err := s.db.Query(`SELECT id, timestamp, exchange, routing_key, correlation_id, reply_to, headers, body
		FROM event ORDER BY id ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return 0, nil, failed.Because("failed to query events:", err)
	}
	defer rows.Close()
	msgs = []api.Message{}
	for rows.Next() {
		var m api.Message
		var ts sql.NullString
		var ex, rk, cid, rt, hd, body sql.NullString
		if err := rows.Scan(&m.ID, &ts, &ex, &rk, &cid, &rt, &hd, &body); err != nil {
			return 0, nil, failed.Because("failed to scan event:", err)
		}
		m.Timestamp, m.Exchange, m.RoutingKey = ts.String, ex.String, rk.String
		m.CorrelationID, m.ReplyTo, m.Headers, m.Body = cid.String, rt.String, hd.String, body.String
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, failed.Because("failed to iterate events:", err)
	}
	return total, msgs, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	if s.insert != nil {
		_ = s.insert.Close()
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// QueryFile is a convenience for the daemon: open read-only, query, close.
func QueryFile(path string, limit, offset int) (int, []api.Message, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return 0, nil, failed.Because("failed to open store for reading:", err)
	}
	defer db.Close()
	s := &Store{db: db}
	return s.Query(limit, offset)
}
