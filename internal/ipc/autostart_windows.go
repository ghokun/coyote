//go:build windows

package ipc

import failed "github.com/ghokun/coyote/error"

// EnsureRunning is unsupported on Windows in the MVP (unix socket + setsid).
func EnsureRunning() error {
	return failed.Because("daemon mode requires a unix platform (darwin/linux)", nil)
}
