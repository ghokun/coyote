package auth

import (
	"strings"
	"testing"
)

func TestCallbackBindAddrIsLoopbackOnly(t *testing.T) {
	addr := callbackBindAddr("8080")
	if addr != "127.0.0.1:8080" {
		t.Errorf("callbackBindAddr(%q) = %q, want %q", "8080", addr, "127.0.0.1:8080")
	}
	if strings.HasPrefix(addr, ":") {
		t.Errorf("callbackBindAddr(%q) = %q, must not bind all interfaces with leading ':'", "8080", addr)
	}
	if strings.Contains(addr, "0.0.0.0") {
		t.Errorf("callbackBindAddr(%q) = %q, must not bind 0.0.0.0", "8080", addr)
	}
}
