package dockerctx

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ApplyCurrentContextHost exports DOCKER_HOST from the current Docker CLI
// context when DOCKER_HOST is unset. The Docker Go SDK honors DOCKER_HOST but
// does not read Docker CLI contexts, which matters for rootless Docker.
func ApplyCurrentContextHost(parent context.Context) string {
	if os.Getenv("DOCKER_HOST") != "" {
		return ""
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	host := strings.TrimSpace(string(out))
	if host == "" || host == "<no value>" {
		return ""
	}
	if strings.Contains(host, "\n") {
		host = strings.TrimSpace(strings.Split(host, "\n")[0])
	}
	if host == "" {
		return ""
	}

	_ = os.Setenv("DOCKER_HOST", host)
	return host
}
