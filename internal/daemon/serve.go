package daemon

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	failed "github.com/ghokun/coyote/error"
	"github.com/ghokun/coyote/internal/ipc"
	"github.com/ghokun/coyote/internal/paths"
)

// Serve runs the daemon in the current process: it opens the supervisor,
// binds the unix socket, serves the IPC API, and blocks until SIGINT/SIGTERM
// or POST /v1/shutdown. It cleans up the socket and pidfile on exit.
func Serve() error {
	if err := paths.EnsureConfigDir(); err != nil {
		return failed.Because("failed to create config dir:", err)
	}
	// Drop a stale socket from a crashed daemon.
	if _, err := os.Stat(paths.SocketPath()); err == nil {
		_ = os.Remove(paths.SocketPath())
	}
	sup, err := Open()
	if err != nil {
		return err
	}
	defer sup.Close()

	shutdownCh := make(chan struct{})
	var shutdownOnce = make(chan struct{}, 1)
	srv := &http.Server{Handler: ipc.NewHandler(sup, func() {
		select {
		case shutdownOnce <- struct{}{}:
		default:
		}
	})}

	ln, err := net.Listen("unix", paths.SocketPath())
	if err != nil {
		return failed.Because("failed to bind daemon socket:", err)
	}
	_ = os.Chmod(paths.SocketPath(), 0o600)
	if err := os.WriteFile(paths.PidPath(), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		_ = ln.Close()
		return failed.Because("failed to write pidfile:", err)
	}
	cleanup := func() {
		_ = ln.Close()
		_ = os.Remove(paths.SocketPath())
		_ = os.Remove(paths.PidPath())
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("coyote daemon listening on %s", paths.SocketPath())
		serveErr <- srv.Serve(ln)
	}()

	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down...", sig)
	case <-shutdownOnce:
		log.Printf("shutdown requested via API")
	case err := <-serveErr:
		cleanup()
		if err != nil && err != http.ErrServerClosed {
			return failed.Because("daemon serve error:", err)
		}
		return nil
	}
	// Graceful drain: stop accepting, then stop tasks.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = ln.Close()
	sup.ShutdownAll()
	cleanup()
	close(shutdownCh)
	return nil
}
