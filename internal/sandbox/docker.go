// Package sandbox wraps the official Docker Engine API client with a
// dclaw-shaped surface. This is the only place in the codebase that imports
// github.com/docker/docker; daemon code talks to DockerClient methods, not to
// docker types directly.
package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerClient is a thin wrapper around the docker SDK's client.Client.
type DockerClient struct {
	cli *client.Client
}

// CreateSpec captures everything the daemon needs to create a new agent
// container.
type CreateSpec struct {
	Name      string
	Image     string
	Env       map[string]string
	Labels    map[string]string
	Workspace string
}

// NewDockerClient connects to the docker daemon using default env resolution.
// Returns an error if the socket is unreachable.
func NewDockerClient() (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	// Round-trip a Ping so a startup-time error surfaces before anything else.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("docker ping: %w", err)
	}
	return &DockerClient{cli: cli}, nil
}

// Close shuts down the underlying client.
func (d *DockerClient) Close() error {
	if d == nil || d.cli == nil {
		return nil
	}
	return d.cli.Close()
}

// CreateAgent creates (but does not start) a container for the given spec.
// Returns the docker container ID.
func (d *DockerClient) CreateAgent(ctx context.Context, spec CreateSpec) (string, error) {
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	labels := make(map[string]string, len(spec.Labels)+1)
	for k, v := range spec.Labels {
		labels[k] = v
	}
	labels["dclaw.managed"] = "true"
	labels["dclaw.name"] = spec.Name

	var mounts []mount.Mount
	if spec.Workspace != "" {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: spec.Workspace,
			Target: "/workspace",
		})
	}

	cfg := &container.Config{
		Image:  spec.Image,
		Env:    env,
		Labels: labels,
		Tty:    false,
	}
	hostCfg := &container.HostConfig{
		Mounts:        mounts,
		RestartPolicy: container.RestartPolicy{Name: "no"},
	}

	resp, err := d.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("ContainerCreate: %w", err)
	}
	return resp.ID, nil
}

// StartAgent starts a created container.
func (d *DockerClient) StartAgent(ctx context.Context, id string) error {
	return d.cli.ContainerStart(ctx, id, container.StartOptions{})
}

// StopAgent stops the container with a graceful SIGTERM grace period.
func (d *DockerClient) StopAgent(ctx context.Context, id string, grace time.Duration) error {
	secs := int(grace.Seconds())
	return d.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &secs})
}

// DeleteAgent removes a container (stopping it first if running).
func (d *DockerClient) DeleteAgent(ctx context.Context, id string) error {
	return d.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: false})
}

// InspectStatus returns the container's current status string, or an error
// if it no longer exists.
func (d *DockerClient) InspectStatus(ctx context.Context, id string) (string, error) {
	info, err := d.cli.ContainerInspect(ctx, id)
	if err != nil {
		return "", err
	}
	if info.State == nil {
		return "unknown", nil
	}
	switch {
	case info.State.Running:
		return "running", nil
	case info.State.Paused:
		return "paused", nil
	case info.State.Restarting:
		return "restarting", nil
	case info.State.Dead:
		return "dead", nil
	case info.State.OOMKilled:
		return "oomkilled", nil
	case info.State.ExitCode != 0:
		return "exited", nil
	case info.State.Status == "created":
		return "created", nil
	default:
		return info.State.Status, nil
	}
}

// LogsTail returns the last N combined stdout+stderr lines, newest-last.
func (d *DockerClient) LogsTail(ctx context.Context, id string, tail int) ([]string, error) {
	rc, err := d.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", tail),
		Timestamps: true,
	})
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var buf bytes.Buffer
	// Demux the multiplexed stdout/stderr stream.
	if _, err := stdcopy.StdCopy(&buf, &buf, rc); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	return lines, nil
}

// LogsFollow returns a channel that streams log lines until ctx is cancelled
// or the container exits. The second channel yields a terminal error (or is
// closed cleanly on EOF).
func (d *DockerClient) LogsFollow(ctx context.Context, id string) (<-chan string, <-chan error) {
	lines := make(chan string, 128)
	errs := make(chan error, 1)
	go func() {
		defer close(lines)
		defer close(errs)
		rc, err := d.cli.ContainerLogs(ctx, id, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
			Timestamps: true,
			Tail:       "all",
		})
		if err != nil {
			errs <- err
			return
		}
		defer rc.Close()

		// Demux asynchronously via a pipe + scanner.
		pr, pw := io.Pipe()
		go func() {
			_, _ = stdcopy.StdCopy(pw, pw, rc)
			pw.Close()
		}()
		scan := bufio.NewScanner(pr)
		for scan.Scan() {
			select {
			case <-ctx.Done():
				return
			case lines <- scan.Text():
			}
		}
		if err := scan.Err(); err != nil {
			errs <- err
		}
	}()
	return lines, errs
}

// ExecIn runs argv inside the container and returns buffered stdout, stderr,
// and exit code.
func (d *DockerClient) ExecIn(ctx context.Context, id string, argv []string) (string, string, int, error) {
	ex, err := d.cli.ContainerExecCreate(ctx, id, types.ExecConfig{
		Cmd:          argv,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", "", 1, err
	}
	att, err := d.cli.ContainerExecAttach(ctx, ex.ID, types.ExecStartCheck{})
	if err != nil {
		return "", "", 1, err
	}
	defer att.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, att.Reader); err != nil && !errors.Is(err, io.EOF) {
		return stdout.String(), stderr.String(), 1, err
	}

	ins, err := d.cli.ContainerExecInspect(ctx, ex.ID)
	if err != nil {
		return stdout.String(), stderr.String(), 1, err
	}
	return stdout.String(), stderr.String(), ins.ExitCode, nil
}
