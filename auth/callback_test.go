package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// testDeliver wires a result channel with the same exactly-once semantics as
// serveForCallback, so tests exercise the production delivery pattern.
func testDeliver() (chan callbackResult, func(callbackResult)) {
	ch := make(chan callbackResult, 1)
	var once sync.Once
	return ch, func(res callbackResult) {
		once.Do(func() {
			ch <- res
		})
	}
}

func callbackRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	return req
}

func TestCallbackHandlerDeliversFirstResultOnce(t *testing.T) {
	exchange := func(code string) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "token-for-" + code}, nil
	}
	ch, deliver := testDeliver()
	handler := newCallbackHandler("test-state", exchange, deliver)

	for i, code := range []string{"code-1", "code-2"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, callbackRequest(t, "/callback?state=test-state&code="+code))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	select {
	case res := <-ch:
		if res.token.AccessToken != "token-for-code-1" {
			t.Errorf("delivered token = %q, want %q (first result must win)", res.token.AccessToken, "token-for-code-1")
		}
	default:
		t.Fatal("expected exactly one delivered result, got none")
	}

	select {
	case res := <-ch:
		t.Fatalf("expected exactly one delivered result, got duplicate with token %q", res.token.AccessToken)
	default:
		// Second duplicate correctly dropped.
	}
}

func TestCallbackHandlerConcurrentDuplicatesDoNotBlock(t *testing.T) {
	exchange := func(code string) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "token-for-" + code}, nil
	}
	ch, deliver := testDeliver()
	handler := newCallbackHandler("test-state", exchange, deliver)

	const callers = 20
	var wg sync.WaitGroup
	done := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, callbackRequest(t, fmt.Sprintf("/callback?state=test-state&code=code-%d", i)))
			if rec.Code != http.StatusOK {
				t.Errorf("caller %d: status = %d, want %d", i, rec.Code, http.StatusOK)
			}
		}(i)
	}
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All duplicate callbacks completed without blocking.
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent duplicate callbacks blocked; goroutine leak regression")
	}

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			if count != 1 {
				t.Fatalf("delivered %d results, want exactly 1", count)
			}
			return
		}
	}
}

func TestCallbackHandlerValidationFailuresDoNotDeliver(t *testing.T) {
	exchange := func(code string) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "must-not-deliver"}, nil
	}
	ch, deliver := testDeliver()
	handler := newCallbackHandler("test-state", exchange, deliver)

	targets := map[string]string{
		"state mismatch": "/callback?state=wrong&code=code-1",
		"missing code":   "/callback?state=test-state",
		"provider error": "/callback?state=test-state&error=access_denied&error_description=nope",
	}
	for name, target := range targets {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, callbackRequest(t, target))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, http.StatusBadRequest)
		}
	}

	select {
	case res := <-ch:
		t.Fatalf("validation failure must not deliver, got token %q", res.token.AccessToken)
	default:
	}
}

func TestCallbackHandlerExchangeFailureAllowsRetry(t *testing.T) {
	calls := 0
	exchange := func(code string) (*oauth2.Token, error) {
		calls++
		if code == "bad-code" {
			return nil, fmt.Errorf("exchange failed")
		}
		return &oauth2.Token{AccessToken: "token-for-" + code}, nil
	}
	ch, deliver := testDeliver()
	handler := newCallbackHandler("test-state", exchange, deliver)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, callbackRequest(t, "/callback?state=test-state&code=bad-code"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("exchange failure: status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, callbackRequest(t, "/callback?state=test-state&code=good-code"))
	if rec.Code != http.StatusOK {
		t.Fatalf("retry: status = %d, want %d", rec.Code, http.StatusOK)
	}

	select {
	case res := <-ch:
		if res.token.AccessToken != "token-for-good-code" {
			t.Errorf("delivered token = %q, want %q", res.token.AccessToken, "token-for-good-code")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected retry result to be delivered")
	}
}
