//go:build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	failed "github.com/ghokun/coyote/error"
	"github.com/ghokun/coyote/internal/paths"
)

// EnsureRunning probes the socket and, when no daemon answers, spawns a
// detached daemon (setsid, stdio to daemon.log) and waits for it to serve.
func EnsureRunning() error {
	if Probe() {
		return nil
	}
	// Drop a stale socket left by a crashed daemon so bind can succeed.
	if conn, err := net.DialTimeout("unix", paths.SocketPath(), 200*time.Millisecond); err != nil {
		_ = os.Remove(paths.SocketPath())
	} else {
		_ = conn.Close()
	}
	if err := paths.EnsureConfigDir(); err != nil {
		return failed.Because("failed to create config dir:", err)
	}
	logF, err := os.OpenFile(paths.DaemonLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return failed.Because("failed to open daemon log:", err)
	}
	defer logF.Close()
	exe, err := os.Executable()
	if err != nil {
		return failed.Because("failed to locate coyote binary:", err)
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return failed.Because("failed to open /dev/null:", err)
	}
	defer devNull.Close()
	cmd := exec.Command(exe, "daemon", "start", "--foreground")
	cmd.Stdin = devNull
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Env = os.Environ()
	if err := detach(cmd); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return failed.Because("failed to start daemon:", err)
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if Probe() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return failed.Because(fmt.Sprintf("daemon did not become ready; see %s", paths.DaemonLogPath()), nil)
}
