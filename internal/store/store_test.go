package store_test

import (
	"path/filepath"
	"testing"

	"github.com/ghokun/coyote/internal/api"
	"github.com/ghokun/coyote/internal/store"
)

func TestOpenInsertQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.sqlite")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for i := 0; i < 5; i++ {
		if err := s.Insert("ex", "rk", "cid", "rt", "{}", `{"n":1}`); err != nil {
			t.Fatal(err)
		}
	}
	total, msgs, err := s.Query(api.MessagesQuery{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(msgs) != 2 || msgs[0].ID != 2 || msgs[1].ID != 3 {
		t.Fatalf("unexpected page: %+v", msgs)
	}
	if msgs[0].Exchange != "ex" || msgs[0].Body != `{"n":1}` {
		t.Fatalf("unexpected row: %+v", msgs[0])
	}
}

func TestQueryFileReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.sqlite")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Insert("ex", "#", "", "", "", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	total, msgs, err := store.QueryFile(path, api.MessagesQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(msgs) != 1 || msgs[0].Body != "hello" {
		t.Fatalf("unexpected read: total=%d msgs=%+v", total, msgs)
	}
}

func seedFilterStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "events.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	rows := []struct{ ex, rk, cid, rt, hd, body string }{
		{"orders", "orders.created", "c1", "svc-a", "k=v", "order 100 arrives"},
		{"orders", "orders.shipped", "c2", "svc-b", "k=v", "order 100 shipped"},
		{"payments", "payments.done", "c3", "svc-a", "k=w", "payment for order 100"},
		{"ORDERS", "misc", "", "", "", "uppercase exchange"},
	}
	for _, r := range rows {
		if err := s.Insert(r.ex, r.rk, r.cid, r.rt, r.hd, r.body); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestQueryFilterSubstring(t *testing.T) {
	s := seedFilterStore(t)
	total, msgs, err := s.Query(api.MessagesQuery{Limit: 10, Filter: api.MessageFilter{Exchange: "order"}})
	if err != nil {
		t.Fatal(err)
	}
	// Case-sensitive: "order" matches orders/orders but not ORDERS.
	if total != 2 || len(msgs) != 2 {
		t.Fatalf("total = %d, len = %d, want 2/2", total, len(msgs))
	}
}

func TestQueryFilterCaseSensitive(t *testing.T) {
	s := seedFilterStore(t)
	total, _, err := s.Query(api.MessagesQuery{Limit: 10, Filter: api.MessageFilter{Exchange: "ORDER"}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1 (uppercase row only)", total)
	}
}

func TestQueryFilterCombinedAND(t *testing.T) {
	s := seedFilterStore(t)
	q := api.MessagesQuery{Limit: 10, Filter: api.MessageFilter{Exchange: "orders", Body: "shipped"}}
	total, msgs, err := s.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(msgs) != 1 || msgs[0].RoutingKey != "orders.shipped" {
		t.Fatalf("unexpected combined result: total=%d msgs=%+v", total, msgs)
	}
}

func TestQueryFilterBodyAndPagination(t *testing.T) {
	s := seedFilterStore(t)
	q := api.MessagesQuery{Limit: 1, Offset: 1, Filter: api.MessageFilter{Body: "order 100"}}
	total, msgs, err := s.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	// Three rows contain "order 100"; page 2 of size 1.
	if total != 3 {
		t.Fatalf("total = %d, want filtered total 3", total)
	}
	if len(msgs) != 1 || msgs[0].ID != 2 {
		t.Fatalf("unexpected page: %+v", msgs)
	}
}

func TestQueryFilterNoMatch(t *testing.T) {
	s := seedFilterStore(t)
	total, msgs, err := s.Query(api.MessagesQuery{Limit: 10, Filter: api.MessageFilter{ReplyTo: "nobody"}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(msgs) != 0 {
		t.Fatalf("expected empty result, got total=%d msgs=%+v", total, msgs)
	}
}

func TestQueryFilterLiteralWildcards(t *testing.T) {
	s := seedFilterStore(t)
	// % and _ must be treated literally, not as wildcards.
	total, _, err := s.Query(api.MessagesQuery{Limit: 10, Filter: api.MessageFilter{Body: "100%"}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0 (no literal match)", total)
	}
}
