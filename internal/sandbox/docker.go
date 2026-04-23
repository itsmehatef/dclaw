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
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Container posture constants (beta.2-sandbox-hardening). Held as
// package-level named constants so tests can assert the exact shape
// without string-matching the implementation.
var (
	DefaultCapDrop     = []string{"ALL"}
	DefaultSecurityOpt = []string{"no-new-privileges:true", "seccomp=default"}
)

// DefaultPidsLimit caps the number of processes an agent container can
// fork. pi-mono's steady-state process count is ~5; 256 leaves ~50×
// headroom while bounding a fork-bomb / PID-DoS primitive.
const DefaultPidsLimit int64 = 256

// DefaultTmpfs is the tmpfs mount table applied to every agent container
// alongside ReadonlyRootfs. The rootfs is read-only post-beta.2, so we
// explicitly carve out the two paths pi-mono writes at runtime:
//
//   - /tmp  — pi-mono scratch space (shell history, working JSON files).
//     Sized 64 MiB; observed pi-mono /tmp usage is < 10 MiB.
//   - /run  — tini runtime sockets and lock files. Sized 8 MiB.
//
// Both mounts are rw + noexec,nosuid,nodev. Because the rootfs is
// read-only, anything dropped in these tmpfses must not be executable
// — an attacker who smuggles a payload into /tmp should not be able
// to chmod+x it into a runnable shellcode stager.
//
// pi-mono write-path audit (agent/run.mjs + agent/Dockerfile, v0.1):
//   - /workspace/*          bind-mount, writable unconditionally.
//   - /tmp/*                covered by this tmpfs.
//   - /root/.pi/agent/*     suppressed by --no-session in agent/run.mjs:29.
//   - /app/node_modules/.cache/*  build-time only (npm ci), not runtime.
//   - /etc/resolv.conf, /etc/hosts  Docker-managed; auto-preserved.
//
// No additional tmpfs overlays are required for the v0.1 agent image.
var DefaultTmpfs = map[string]string{
	"/tmp": "rw,noexec,nosuid,nodev,size=64m",
	"/run": "rw,noexec,nosuid,nodev,size=8m",
}

// pidsLimitPtr lifts an int64 into a *int64 — the Docker SDK's
// container.Resources.PidsLimit field is a pointer so absent/zero
// semantics differ from "set to N".
func pidsLimitPtr(n int64) *int64 { return &n }

// dockerAPI is the minimal subset of the docker SDK's *client.Client
// surface that DockerClient actually calls. Declaring it here lets
// tests inject a recording fake via DockerClient.cli without touching
// a live Docker daemon. The real *client.Client satisfies this
// interface trivially because every method below has the same
// signature on the SDK type.
type dockerAPI interface {
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerExecCreate(ctx context.Context, container string, config types.ExecConfig) (types.IDResponse, error)
	ContainerExecAttach(ctx context.Context, execID string, config types.ExecStartCheck) (types.HijackedResponse, error)
	ContainerExecInspect(ctx context.Context, execID string) (types.ContainerExecInspect, error)
	Close() error
}

// Compile-time proof the real SDK client satisfies dockerAPI. If the
// docker SDK ever changes a signature, this line fails to build in
// lockstep with every call site — the canary we want.
var _ dockerAPI = (*client.Client)(nil)

// ErrDockerFailure is the sentinel for any error arising from a Docker
// API call. Wrapped with fmt.Errorf("%w: %v", ErrDockerFailure, err) so
// callers can dispatch via errors.Is without string matching.
var ErrDockerFailure = errors.New("docker operation failed")

// DockerClient is a thin wrapper around the docker SDK. The cli field
// holds a dockerAPI interface (not *client.Client directly) so tests
// can inject a recording fake; production still supplies the real
// *client.Client via NewDockerClient.
type DockerClient struct {
	cli dockerAPI
}

// DockerExecClient is the subset of DockerClient methods needed by ChatHandler.
// Declaring it in this package (next to the concrete type) keeps sandbox as the
// single source of truth for Docker API shapes. ChatHandler accepts this
// interface so tests can inject a mock without a live Docker daemon.
type DockerExecClient interface {
	InspectStatus(ctx context.Context, id string) (string, error)
	ExecIn(ctx context.Context, id string, argv []string) (string, string, int, error)
}

// Verify DockerClient satisfies DockerExecClient at compile time.
var _ DockerExecClient = (*DockerClient)(nil)

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
//
// Belt-and-suspenders: before building the bind-mount we assert the
// workspace path is already absolute and Clean — policy lives upstream
// in internal/paths, and a sandbox-layer failure here would be a dclaw
// bug (the validator gave us a relative path, or "." / ".." crept in).
// We deliberately do NOT wrap with paths.ErrWorkspaceForbidden because
// this is not a policy rejection; it is an internal invariant violation.
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
		if !filepath.IsAbs(spec.Workspace) {
			return "", fmt.Errorf("sandbox: workspace must be absolute, got %q", spec.Workspace)
		}
		if filepath.Clean(spec.Workspace) != spec.Workspace {
			return "", fmt.Errorf("sandbox: workspace must be clean, got %q", spec.Workspace)
		}
		if strings.Contains(spec.Workspace, "..") {
			return "", fmt.Errorf("sandbox: workspace contains '..': %q", spec.Workspace)
		}
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
		Mounts:         mounts,
		RestartPolicy:  container.RestartPolicy{Name: "no"},
		CapDrop:        DefaultCapDrop,
		SecurityOpt:    DefaultSecurityOpt,
		ReadonlyRootfs: true,
		Tmpfs:          DefaultTmpfs,
		Resources: container.Resources{
			PidsLimit: pidsLimitPtr(DefaultPidsLimit),
		},
	}

	resp, err := d.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("%w: ContainerCreate: %v", ErrDockerFailure, err)
	}
	return resp.ID, nil
}

// StartAgent starts a created container.
func (d *DockerClient) StartAgent(ctx context.Context, id string) error {
	if err := d.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("%w: ContainerStart: %v", ErrDockerFailure, err)
	}
	return nil
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
