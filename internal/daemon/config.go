package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/itsmehatef/dclaw/internal/config"
)

// Config holds the daemon's resolved paths and settings. Constructed once at
// startup via LoadConfig; immutable thereafter.
type Config struct {
	SocketPath string // resolved via config.DefaultSocketPath
	StateDir   string // ~/.dclaw (created with mode 0700)
	DBPath     string // <StateDir>/state.db
	LogDir     string // <StateDir>/logs
	LogPath    string // <LogDir>/daemon.log
	PIDPath    string // <StateDir>/dclawd.pid
	LogLevel   string // debug|info|warn|error
}

// LoadConfig resolves default paths via internal/config.Resolve, ensures the
// state and log directories exist, and returns the populated Config.
// socketOverride / stateDirOverride / logLevel may be empty.
func LoadConfig(socketOverride, stateDirOverride, logLevel string) (*Config, error) {
	paths, err := config.Resolve(stateDirOverride, socketOverride)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir state dir %q: %w", paths.StateDir, err)
	}

	logDir := filepath.Join(paths.StateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir log dir %q: %w", logDir, err)
	}

	if logLevel == "" {
		logLevel = "info"
	}

	return &Config{
		SocketPath: paths.SocketPath,
		StateDir:   paths.StateDir,
		DBPath:     filepath.Join(paths.StateDir, "state.db"),
		LogDir:     logDir,
		LogPath:    filepath.Join(logDir, "daemon.log"),
		PIDPath:    filepath.Join(paths.StateDir, "dclawd.pid"),
		LogLevel:   logLevel,
	}, nil
}

// DefaultSocketPath is a compatibility alias kept so any caller not in the
// five call sites tracked by PR-A transparently redirects to the canonical
// implementation in internal/config. Prefer config.DefaultSocketPath in new code.
var DefaultSocketPath = config.DefaultSocketPath

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
