package paths

import (
	"os"
	"path/filepath"
)

// ConfigDir returns $XDG_CONFIG_HOME/coyote when XDG_CONFIG_HOME is set,
// otherwise ~/.config/coyote.
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "coyote")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".config", "coyote")
	}
	return filepath.Join(home, ".config", "coyote")
}

func SocketPath() string    { return filepath.Join(ConfigDir(), "coyote.sock") }
func PidPath() string       { return filepath.Join(ConfigDir(), "daemon.pid") }
func DaemonLogPath() string { return filepath.Join(ConfigDir(), "daemon.log") }
func DaemonDBPath() string  { return filepath.Join(ConfigDir(), "daemon.db") }
func TasksDir() string      { return filepath.Join(ConfigDir(), "tasks") }

func TaskDir(id string) string     { return filepath.Join(TasksDir(), id) }
func TaskLogPath(id string) string { return filepath.Join(TaskDir(id), "task.log") }

// EnsureDir creates dir with 0700 (and parents) if missing.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o700)
}

// EnsureConfigDir creates the base config dir and tasks dir.
func EnsureConfigDir() error {
	if err := EnsureDir(ConfigDir()); err != nil {
		return err
	}
	return EnsureDir(TasksDir())
}
