package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// Config holds the daemon's resolved paths and settings. Constructed once at
// startup via LoadConfig; immutable thereafter.
type Config struct {
	SocketPath string // $XDG_RUNTIME_DIR/dclaw.sock or ~/.dclaw/dclaw.sock
	StateDir   string // ~/.dclaw (created with mode 0700)
	DBPath     string // <StateDir>/state.db
	LogDir     string // <StateDir>/logs
	LogPath    string // <LogDir>/daemon.log
	PIDPath    string // <StateDir>/dclawd.pid
	LogLevel   string // debug|info|warn|error
}

// LoadConfig resolves default paths and validates the runtime environment.
// socketOverride / stateDirOverride / logLevel may be empty.
func LoadConfig(socketOverride, stateDirOverride, logLevel string) (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	stateDir := stateDirOverride
	if stateDir == "" {
		stateDir = filepath.Join(home, ".dclaw")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir state dir %q: %w", stateDir, err)
	}

	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir log dir %q: %w", logDir, err)
	}

	socketPath := socketOverride
	if socketPath == "" {
		socketPath = DefaultSocketPath(stateDir)
	}

	if logLevel == "" {
		logLevel = "info"
	}

	return &Config{
		SocketPath: socketPath,
		StateDir:   stateDir,
		DBPath:     filepath.Join(stateDir, "state.db"),
		LogDir:     logDir,
		LogPath:    filepath.Join(logDir, "daemon.log"),
		PIDPath:    filepath.Join(stateDir, "dclawd.pid"),
		LogLevel:   logLevel,
	}, nil
}

// DefaultSocketPath returns the resolved socket path for this host.
//
// On Linux, prefer $XDG_RUNTIME_DIR/dclaw.sock (typically /run/user/<uid>).
// If XDG_RUNTIME_DIR is unset or not writable, fall back to <stateDir>/dclaw.sock.
// On macOS, XDG_RUNTIME_DIR is rarely set; always use <stateDir>/dclaw.sock.
func DefaultSocketPath(stateDir string) string {
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			if fi, err := os.Stat(xdg); err == nil && fi.IsDir() {
				return filepath.Join(xdg, "dclaw.sock")
			}
		}
	}
	return filepath.Join(stateDir, "dclaw.sock")
}

// WritePIDFile atomically writes pid to PIDPath with mode 0600.
func (c *Config) WritePIDFile(pid int) error {
	tmp := c.PIDPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.PIDPath)
}

// RemovePIDFile deletes the pidfile. Safe to call multiple times.
func (c *Config) RemovePIDFile() {
	_ = os.Remove(c.PIDPath)
}

// ReadPIDFile returns the pid from PIDPath, or 0 if no pidfile exists.
func (c *Config) ReadPIDFile() (int, error) {
	b, err := os.ReadFile(c.PIDPath)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(trimSpace(b)))
	if err != nil {
		return 0, fmt.Errorf("invalid pidfile contents: %w", err)
	}
	return pid, nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
