package store_test

import (
	"path/filepath"
	"testing"

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
	total, msgs, err := s.Query(2, 1)
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
	total, msgs, err := store.QueryFile(path, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(msgs) != 1 || msgs[0].Body != "hello" {
		t.Fatalf("unexpected read: total=%d msgs=%+v", total, msgs)
	}
}
