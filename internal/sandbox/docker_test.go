// Package sandbox tests the container-posture shape applied by CreateAgent.
//
// These tests do NOT contact a real Docker daemon. Instead they inject a
// recording captureClient into DockerClient.cli via the package-internal
// dockerAPI interface, call CreateAgent, and assert the captured Config +
// HostConfig carry the beta.2-sandbox-hardening posture fields (CapDrop,
// SecurityOpt, Resources.PidsLimit). The test is scoped tightly: no
// assertions on unrelated fields — a future PR (PR-B / PR-C) will extend
// the table as new posture fields ship.
//
// Design note: the assertions reference the same package-level constants
// (DefaultCapDrop, DefaultSecurityOpt, DefaultPidsLimit) that the
// implementation uses, so a regression that renames or reshapes those
// constants trips the compilation, not a string-match mismatch. This is
// deliberate — we want the posture shape pinned at one source of truth.
package sandbox

import (
	"context"
	"io"
	"reflect"
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
			got := fake.lastHostCfg

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

			// SecurityOpt must contain both no-new-privileges:true and
			// seccomp=default. Use Contains-style (membership) instead of
			// DeepEqual to stay robust if a future PR appends a third
			// option — the posture floor is all that matters here.
			if !containsString(got.SecurityOpt, "no-new-privileges:true") {
				t.Errorf("SecurityOpt missing no-new-privileges:true; got %v", got.SecurityOpt)
			}
			if !containsString(got.SecurityOpt, "seccomp=default") {
				t.Errorf("SecurityOpt missing seccomp=default; got %v", got.SecurityOpt)
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
