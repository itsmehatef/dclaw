// Package sandbox tests the container-posture shape applied by CreateAgent.
//
// These tests do NOT contact a real Docker daemon. Instead they inject a
// recording captureClient into DockerClient.cli via the package-internal
// dockerAPI interface, call CreateAgent, and assert the captured Config +
// HostConfig carry the beta.2-sandbox-hardening posture fields (CapDrop,
// SecurityOpt, Resources.PidsLimit, ReadonlyRootfs, Tmpfs). The test is
// scoped tightly: no assertions on unrelated fields — a future PR
// (PR-C / PR-D) will extend the table as new posture fields ship.
//
// Design note: the assertions reference the same package-level constants
// (DefaultCapDrop, DefaultSecurityOpt, DefaultPidsLimit, DefaultTmpfs)
// that the implementation uses, so a regression that renames or reshapes
// those constants trips the compilation, not a string-match mismatch.
// This is deliberate — we want the posture shape pinned at one source
// of truth.
package sandbox

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// captureClient is a minimal dockerAPI implementation that records the
// arguments passed to ContainerCreate. All other methods are no-ops
// returning zero values; they exist only so captureClient satisfies the
// dockerAPI interface at compile time. CreateAgent only touches
// ContainerCreate so nothing else needs real behavior.
type captureClient struct {
	lastCfg     *container.Config
	lastHostCfg *container.HostConfig
	createErr   error
}

func (c *captureClient) ContainerCreate(
	_ context.Context,
	config *container.Config,
	hostConfig *container.HostConfig,
	_ *network.NetworkingConfig,
	_ *ocispec.Platform,
	_ string,
) (container.CreateResponse, error) {
	c.lastCfg = config
	c.lastHostCfg = hostConfig
	if c.createErr != nil {
		return container.CreateResponse{}, c.createErr
	}
	return container.CreateResponse{ID: "stub-container-id"}, nil
}

func (c *captureClient) ContainerStart(_ context.Context, _ string, _ container.StartOptions) error {
	return nil
}

func (c *captureClient) ContainerStop(_ context.Context, _ string, _ container.StopOptions) error {
	return nil
}

func (c *captureClient) ContainerRemove(_ context.Context, _ string, _ container.RemoveOptions) error {
	return nil
}

func (c *captureClient) ContainerInspect(_ context.Context, _ string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}

func (c *captureClient) ContainerLogs(_ context.Context, _ string, _ container.LogsOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (c *captureClient) ContainerExecCreate(_ context.Context, _ string, _ types.ExecConfig) (types.IDResponse, error) {
	return types.IDResponse{}, nil
}

func (c *captureClient) ContainerExecAttach(_ context.Context, _ string, _ types.ExecStartCheck) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, nil
}

func (c *captureClient) ContainerExecInspect(_ context.Context, _ string) (types.ContainerExecInspect, error) {
	return types.ContainerExecInspect{}, nil
}

func (c *captureClient) Close() error { return nil }

// Compile-time proof captureClient satisfies dockerAPI. If the interface
// grows a method and we forget to stub it here, this line fails to build.
var _ dockerAPI = (*captureClient)(nil)

// TestCreateAgentAppliesBeta2HardeningPosture is the posture
// regression test. It exercises CreateAgent across three shape
// variants — with workspace, without workspace, and with populated
// env+labels — and in every case asserts the HostConfig carries the
// beta.2 hardening posture. Workspace, env, and labels are orthogonal
// to posture: changing any of them must not affect CapDrop,
// SecurityOpt, or PidsLimit.
func TestCreateAgentAppliesBeta2HardeningPosture(t *testing.T) {
	tests := []struct {
		name string
		spec CreateSpec
	}{
		{
			name: "happy path with workspace",
			spec: CreateSpec{
				Name:      "test-a",
				Image:     "x:v0.1",
				Workspace: "/tmp/foo",
			},
		},
		{
			name: "no workspace",
			spec: CreateSpec{
				Name:      "test-b",
				Image:     "x:v0.1",
				Workspace: "",
			},
		},
		{
			name: "env and labels populated",
			spec: CreateSpec{
				Name:   "test-c",
				Image:  "x:v0.1",
				Env:    map[string]string{"A": "1", "B": "2"},
				Labels: map[string]string{"L": "v"},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fake := &captureClient{}
			d := &DockerClient{cli: fake}

			id, err := d.CreateAgent(context.Background(), tc.spec)
			if err != nil {
				t.Fatalf("CreateAgent returned error: %v", err)
			}
			if id == "" {
				t.Fatalf("CreateAgent returned empty id")
			}
			if fake.lastHostCfg == nil {
				t.Fatalf("captureClient did not record HostConfig")
			}
			if fake.lastCfg == nil {
				t.Fatalf("captureClient did not record Config")
			}
			got := fake.lastHostCfg
			gotCfg := fake.lastCfg

			// container.Config.User must be "1000:1000" — the
			// DefaultContainerUser constant. The field lives on
			// container.Config (not HostConfig) in Docker SDK v26;
			// daemon-side enforcement overrides any image USER
			// directive so a future image regression to root is
			// caught at container-start time. See
			// docs/phase-3-beta2-sandbox-hardening-plan.md §4.3.
			if gotCfg.User != DefaultContainerUser {
				t.Errorf("Config.User = %q, want %q", gotCfg.User, DefaultContainerUser)
			}
			if gotCfg.User != "1000:1000" {
				t.Errorf("Config.User literal = %q, want \"1000:1000\"", gotCfg.User)
			}

			// CapDrop: ["ALL"] — all Linux capabilities dropped. The
			// HostConfig field is strslice.StrSlice (a []string named
			// type) so cast to []string before DeepEqual-ing against
			// DefaultCapDrop, which is declared as []string.
			gotCapDrop := []string(got.CapDrop)
			if !reflect.DeepEqual(gotCapDrop, DefaultCapDrop) {
				t.Errorf("CapDrop = %v, want %v", gotCapDrop, DefaultCapDrop)
			}
			if !reflect.DeepEqual(gotCapDrop, []string{"ALL"}) {
				t.Errorf("CapDrop literal = %v, want [ALL]", gotCapDrop)
			}

			// SecurityOpt must contain no-new-privileges:true. Use
			// Contains-style (membership) instead of DeepEqual to stay
			// robust if a future PR appends another option — the
			// posture floor is all that matters here.
			//
			// Regression guard: `seccomp=default` must NOT appear.
			// Docker rejects that literal token (the value after
			// `seccomp=` is parsed as `unconfined` or JSON profile
			// content, never a profile-name selector). Docker's
			// built-in default seccomp profile is applied
			// automatically by the daemon when no `seccomp=`
			// SecurityOpt is set, so the threat-model coverage
			// (keyctl/add_key/ptrace denial, §6.3) is unchanged.
			// See the hotfix on top of v0.3.0-beta.2-sandbox-hardening
			// for context.
			if !containsString(got.SecurityOpt, "no-new-privileges:true") {
				t.Errorf("SecurityOpt missing no-new-privileges:true; got %v", got.SecurityOpt)
			}
			if containsString(got.SecurityOpt, "seccomp=default") {
				t.Errorf("SecurityOpt must not contain seccomp=default (Docker rejects it as invalid JSON); got %v", got.SecurityOpt)
			}

			// Resources.PidsLimit must be a non-nil *int64 pointing at
			// DefaultPidsLimit (256). The Docker SDK uses a pointer here
			// so absent/zero semantics differ from explicit 0.
			if got.Resources.PidsLimit == nil {
				t.Fatalf("Resources.PidsLimit is nil; want *int64 at %d", DefaultPidsLimit)
			}
			if *got.Resources.PidsLimit != DefaultPidsLimit {
				t.Errorf("*Resources.PidsLimit = %d, want %d", *got.Resources.PidsLimit, DefaultPidsLimit)
			}
			if *got.Resources.PidsLimit != int64(256) {
				t.Errorf("*Resources.PidsLimit literal = %d, want 256", *got.Resources.PidsLimit)
			}

			// ReadonlyRootfs must be true so /etc, /usr, /opt, /app all
			// become immutable from inside the container. Combined with
			// the Tmpfs overlays below this mirrors the posture assertion
			// in docs/phase-3-beta2-sandbox-hardening-plan.md §4.2.
			if got.ReadonlyRootfs != true {
				t.Errorf("ReadonlyRootfs = %v, want true", got.ReadonlyRootfs)
			}

			// Tmpfs must carry exactly the two entries DefaultTmpfs
			// declares: /tmp sized 64m, /run sized 8m, both
			// rw,noexec,nosuid,nodev. Exact-equal on the map catches
			// any accidental divergence from the package constant;
			// the per-entry checks below document the load-bearing
			// options in case the Docker SDK string-formats them
			// differently in a future version.
			if got.Tmpfs == nil {
				t.Fatalf("Tmpfs is nil; want map with /tmp and /run entries")
			}
			if !reflect.DeepEqual(got.Tmpfs, DefaultTmpfs) {
				t.Errorf("Tmpfs = %v, want %v", got.Tmpfs, DefaultTmpfs)
			}
			if got.Tmpfs["/tmp"] != "rw,noexec,nosuid,nodev,size=64m" {
				t.Errorf(`Tmpfs["/tmp"] = %q, want "rw,noexec,nosuid,nodev,size=64m"`, got.Tmpfs["/tmp"])
			}
			if got.Tmpfs["/run"] != "rw,noexec,nosuid,nodev,size=8m" {
				t.Errorf(`Tmpfs["/run"] = %q, want "rw,noexec,nosuid,nodev,size=8m"`, got.Tmpfs["/run"])
			}
			// Substring checks: robust against Docker SDK reordering
			// the mount-option tokens in a future release. If the
			// exact-equal above starts failing but these still pass,
			// the semantic posture is preserved.
			for _, path := range []string{"/tmp", "/run"} {
				opts := got.Tmpfs[path]
				for _, want := range []string{"noexec", "nosuid", "nodev"} {
					if !strings.Contains(opts, want) {
						t.Errorf("Tmpfs[%q]=%q missing option %q", path, opts, want)
					}
				}
			}
		})
	}
}

// containsString reports whether s appears in haystack. Trivial helper;
// stdlib slices.Contains exists in 1.21+, but keep a local copy so the
// test does not add an import unrelated to its subject.
func containsString(haystack []string, s string) bool {
	for _, h := range haystack {
		if h == s {
			return true
		}
	}
	return false
}
