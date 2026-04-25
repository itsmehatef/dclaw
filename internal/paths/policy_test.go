package paths_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/itsmehatef/dclaw/internal/paths"
)

// macOSDenylist is the built-in list used by the table tests. It strips
// /private/var and /private/tmp from paths.DefaultDenylist because
// t.TempDir on macOS creates paths under /private/var/folders/... —
// without the strip, every tempdir-based row would trip the denylist
// before the specific invariant under test had a chance to fire. The
// denylist entries we strip are still exercised in rows 10 and 11
// (which call Validate on the literal strings "/private/var" and
// "/private/tmp") via the full default denylist.
var macOSDenylist = stripTempDirEntries(paths.DefaultDenylist)

// fullDenylist is paths.DefaultDenylist with /private/var and /private/tmp
// included; used by rows 10/11 that specifically assert those paths are
// rejected.
var fullDenylist = paths.DefaultDenylist

// stripTempDirEntries removes entries that overlap with t.TempDir's
// storage locations on common CI runners. Keeps everything else.
func stripTempDirEntries(in []string) []string {
	stripped := make([]string, 0, len(in))
	strip := map[string]bool{
		"/private/var": true,
		"/private/tmp": true,
		"/var":         true, // /var → /private/var on macOS
	}
	for _, e := range in {
		if strip[e] {
			continue
		}
		stripped = append(stripped, e)
	}
	return stripped
}

// policyTestCase is one row from §6 of the plan. runSetup allows rows that
// need on-disk scaffolding (symlinks, existing dirs) to build it against
// t.TempDir and substitute the dynamic AllowRoot / input into the case.
type policyTestCase struct {
	name        string
	runSetup    func(t *testing.T) (allowRoot, input string, overrideDenylist []string, allowTrust bool, expectedCanonical string)
	allowRoot   string
	input       string
	allowTrust  bool
	wantErr     bool
	wantCanonEQ string // if set, exact equality check; else no canonical assertion
	wantSubstr  string // substring expected in the error message when wantErr
	skipIf      func(t *testing.T) bool
}

// TestPolicyValidate covers ≥30 rows from §6. Rows requiring on-disk
// scaffolding build it inside t.TempDir; rows requiring APFS case-
// insensitivity are skipped on non-darwin runners.
func TestPolicyValidate(t *testing.T) {
	// Resolve temp dir once so every row that needs an "inside allow-root"
	// path can build one under a shared root without each row making its
	// own dir.
	tmpRoot := t.TempDir()
	// The allow-root is a subdir of tmpRoot; we create sibling dirs that
	// demonstrate the prefix-bypass attack (row 6).
	allowRoot := filepath.Join(tmpRoot, "dclaw-agents")
	if err := os.Mkdir(allowRoot, 0o755); err != nil {
		t.Fatalf("mkdir allow-root: %v", err)
	}
	// row-2 happy path: allowRoot/p1
	p1 := filepath.Join(allowRoot, "p1")
	if err := os.Mkdir(p1, 0o755); err != nil {
		t.Fatalf("mkdir p1: %v", err)
	}
	// row-6 bypass attempt: allowRoot+"-evil"
	evilSibling := allowRoot + "-evil"
	if err := os.Mkdir(evilSibling, 0o755); err != nil {
		t.Fatalf("mkdir evil: %v", err)
	}
	// row-23 tab-in-name
	tabDir := filepath.Join(allowRoot, "p\t1")
	if err := os.Mkdir(tabDir, 0o755); err != nil {
		t.Fatalf("mkdir tab: %v", err)
	}
	// row-24 NFC café
	cafeNFC := filepath.Join(allowRoot, "café")
	if err := os.Mkdir(cafeNFC, 0o755); err != nil {
		t.Fatalf("mkdir cafe: %v", err)
	}
	// row-26 symlink out of root
	outside := filepath.Join(tmpRoot, "outside-etc")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	linkOut := filepath.Join(allowRoot, "symlink-out")
	if err := os.Symlink(outside, linkOut); err != nil {
		t.Fatalf("symlink out: %v", err)
	}
	// row-27 intra-root symlink
	linkIn := filepath.Join(allowRoot, "symlink-in")
	if err := os.Symlink(p1, linkIn); err != nil {
		t.Fatalf("symlink in: %v", err)
	}
	// row-32 trailing-space
	trailSpace := filepath.Join(allowRoot, "p1 ")
	if err := os.Mkdir(trailSpace, 0o755); err != nil {
		t.Fatalf("mkdir trail-space: %v", err)
	}

	// Denylist with a test-controlled entry for trust-doesn't-bypass-denylist.
	// Includes both /etc and /private/etc because EvalSymlinks on macOS
	// resolves /etc → /private/etc; without both, the canonical doesn't
	// match the denylist string.
	denyEtcOnly := []string{"/etc", "/private/etc"}

	// Resolve the canonical allowRoot once; tests compare against EvalSymlinks
	// output which may differ from the raw join on macOS (/var ↔ /private/var).
	allowRootCanon, err := filepath.EvalSymlinks(allowRoot)
	if err != nil {
		t.Fatalf("evalsymlinks allowRoot: %v", err)
	}
	p1Canon, _ := filepath.EvalSymlinks(p1)
	tabCanon, _ := filepath.EvalSymlinks(tabDir)
	cafeCanon, _ := filepath.EvalSymlinks(cafeNFC)
	trailCanon, _ := filepath.EvalSymlinks(trailSpace)

	cases := []policyTestCase{
		// 1. empty input
		{
			name:       "01-empty-input",
			allowRoot:  allowRoot,
			input:      "",
			wantErr:    true,
			wantSubstr: "empty",
		},
		// 2. happy path
		{
			name:        "02-happy-path",
			allowRoot:   allowRoot,
			input:       p1,
			wantErr:     false,
			wantCanonEQ: p1Canon,
		},
		// 3. APFS case-insensitive happy — on APFS the lowercased p1
		// resolves to the same inode, Validate accepts, and the returned
		// canonical is whatever casing EvalSymlinks produced. The test
		// does a case-fold comparison instead of byte-equality.
		{
			name:      "03-apfs-case-insensitive",
			allowRoot: allowRoot,
			input:     strings.ToLower(p1),
			wantErr:   false,
			// No wantCanonEQ: special-cased below via runSetup that sets
			// canonOverride to a case-fold match sentinel.
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				return allowRoot, strings.ToLower(p1), macOSDenylist, false, "__APFS_CASEFOLD_MATCH__"
			},
			skipIf: func(t *testing.T) bool {
				// Only meaningful on macOS (APFS default is case-insensitive);
				// on Linux ext4 (case-sensitive) the lowercase version of a
				// mixed-case path does not exist and EvalSymlinks fails, which
				// is different semantics than what this row exercises.
				return runtime.GOOS != "darwin"
			},
		},
		// 4. ../ in path, Clean normalizes
		{
			name:        "04-clean-normalizes",
			allowRoot:   allowRoot,
			input:       allowRoot + "/../dclaw-agents/p1",
			wantErr:     false,
			wantCanonEQ: p1Canon,
		},
		// 5. ../ escaping: path crafted so Clean collapses it up and out
		// of the allow-root into an existing denylisted directory (/etc).
		// The literal input is an absolute path, so Abs is a no-op and
		// Clean handles the ".." sequence.
		{
			name:       "05-clean-escape-to-etc",
			allowRoot:  allowRoot,
			input:      "/etc/..//etc",
			wantErr:    true,
			wantSubstr: "denylist",
		},
		// 6. allow-root-prefix-bypass
		{
			name:       "06-allow-root-prefix-bypass",
			allowRoot:  allowRoot,
			input:      evilSibling,
			wantErr:    true,
			wantSubstr: "not under allow-root",
		},
		// 7. /etc denylist
		{
			name:       "07-etc-denylist",
			allowRoot:  allowRoot,
			input:      "/etc",
			wantErr:    true,
			wantSubstr: "denylist",
		},
		// 8. /ETC denylist via EqualFold
		{
			name:       "08-etc-equalfold",
			allowRoot:  allowRoot,
			input:      "/ETC",
			wantErr:    true,
			wantSubstr: "denylist",
			skipIf: func(t *testing.T) bool {
				// APFS is case-insensitive so /ETC folds to /etc → /private/etc
				// (both denylisted). On Linux ext4 (case-sensitive) /ETC does
				// not exist and EvalSymlinks errors before the denylist check.
				return runtime.GOOS != "darwin"
			},
		},
		// 9. /private/etc denylist
		{
			name:       "09-private-etc-denylist",
			allowRoot:  allowRoot,
			input:      "/private/etc",
			wantErr:    true,
			wantSubstr: "denylist",
			skipIf: func(t *testing.T) bool {
				// /private/etc is a macOS firmlink; absent on Linux CI,
				// where EvalSymlinks errors before the denylist check runs.
				_, err := os.Stat("/private/etc")
				return err != nil
			},
		},
		// 10. /private/var denylist — use the full denylist so /private/var
		// is included even though test tempdirs normally need it stripped.
		{
			name: "10-private-var-denylist",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				return allowRoot, "/private/var", fullDenylist, false, ""
			},
			wantErr:    true,
			wantSubstr: "denylist",
			skipIf: func(t *testing.T) bool {
				// /private/var is a macOS firmlink; absent on Linux CI.
				_, err := os.Stat("/private/var")
				return err != nil
			},
		},
		// 11. /private/tmp denylist — see row 10 for why we swap the denylist.
		{
			name: "11-private-tmp-denylist",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				return allowRoot, "/private/tmp", fullDenylist, false, ""
			},
			wantErr:    true,
			wantSubstr: "denylist",
			skipIf: func(t *testing.T) bool {
				// /private/tmp is a macOS firmlink; absent on Linux CI.
				_, err := os.Stat("/private/tmp")
				return err != nil
			},
		},
		// 12. /Volumes denylist
		{
			name:       "12-volumes-denylist",
			allowRoot:  allowRoot,
			input:      "/Volumes",
			wantErr:    true,
			wantSubstr: "denylist",
			skipIf: func(t *testing.T) bool {
				// /Volumes does not exist on Linux CI; EvalSymlinks errors
				// before the denylist check runs. On darwin it always exists.
				_, err := os.Stat("/Volumes")
				return err != nil
			},
		},
		// 13. /Library/Preferences denylist (descendant)
		{
			name:       "13-library-preferences-under-library",
			allowRoot:  allowRoot,
			input:      "/Library/Preferences",
			wantErr:    true,
			wantSubstr: "denylist",
			skipIf: func(t *testing.T) bool {
				_, err := os.Stat("/Library/Preferences")
				return err != nil
			},
		},
		// 14. /Applications denylist
		{
			name:       "14-applications-denylist",
			allowRoot:  allowRoot,
			input:      "/Applications",
			wantErr:    true,
			wantSubstr: "denylist",
			skipIf: func(t *testing.T) bool {
				_, err := os.Stat("/Applications")
				return err != nil
			},
		},
		// 15. /opt descendant
		{
			name:       "15-opt-descendant-denylist",
			allowRoot:  allowRoot,
			input:      "/opt",
			wantErr:    true,
			wantSubstr: "denylist",
			skipIf: func(t *testing.T) bool {
				_, err := os.Stat("/opt")
				return err != nil
			},
		},
		// 16. / denylist
		{
			name:       "16-root-denylist",
			allowRoot:  allowRoot,
			input:      "/",
			wantErr:    true,
			wantSubstr: "denylist",
		},
		// 17. $HOME (injected into denylist for this row)
		{
			name: "17-daemon-home-denylist",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				home, err := os.UserHomeDir()
				if err != nil {
					t.Skip("cannot resolve home")
				}
				dl := append([]string{}, macOSDenylist...)
				dl = append(dl, home)
				return allowRoot, home, dl, false, ""
			},
			wantErr:    true,
			wantSubstr: "denylist",
		},
		// 18. $HOME/.ssh — under home, not under allow-root
		{
			name:       "18-home-ssh-not-under-allow-root",
			allowRoot:  allowRoot,
			input:      filepath.Join(os.Getenv("HOME"), ".ssh"),
			wantErr:    true,
			wantSubstr: "not under allow-root",
			skipIf: func(t *testing.T) bool {
				_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".ssh"))
				return err != nil || os.Getenv("HOME") == ""
			},
		},
		// 19. allow-root itself
		{
			name:        "19-allow-root-is-own-workspace",
			allowRoot:   allowRoot,
			input:       allowRoot,
			wantErr:     false,
			wantCanonEQ: allowRootCanon,
		},
		// 20. relative path
		{
			name:       "20-relative-path-rejected",
			allowRoot:  allowRoot,
			input:      "workspace-p1",
			wantErr:    true,
			wantSubstr: "absolute",
		},
		// 21. NUL byte
		{
			name:       "21-nul-byte",
			allowRoot:  allowRoot,
			input:      allowRoot + "/p\x001",
			wantErr:    true,
			wantSubstr: "NUL",
		},
		// 22. newline
		{
			name:       "22-newline",
			allowRoot:  allowRoot,
			input:      allowRoot + "/p\n1",
			wantErr:    true,
			wantSubstr: "newline",
		},
		// 23. tab OK
		{
			name:        "23-tab-allowed",
			allowRoot:   allowRoot,
			input:       tabDir,
			wantErr:     false,
			wantCanonEQ: tabCanon,
		},
		// 24. NFC café
		{
			name:        "24-nfc-cafe",
			allowRoot:   allowRoot,
			input:       cafeNFC,
			wantErr:     false,
			wantCanonEQ: cafeCanon,
		},
		// 25. NFD cafe (0x65 0xcc 0x81) normalizes to NFC match
		{
			name:        "25-nfd-cafe-normalizes",
			allowRoot:   allowRoot,
			input:       filepath.Join(allowRoot, "cafe\u0301"),
			wantErr:     false,
			wantCanonEQ: cafeCanon,
		},
		// 26. symlink out of root
		{
			name:       "26-symlink-out-of-root",
			allowRoot:  allowRoot,
			input:      linkOut,
			wantErr:    true,
			wantSubstr: "not under allow-root",
		},
		// 27. intra-root symlink
		{
			name:        "27-intra-root-symlink-ok",
			allowRoot:   allowRoot,
			input:       linkIn,
			wantErr:     false,
			wantCanonEQ: p1Canon,
		},
		// 28. AllowTrust=true, Denylist=[/etc] → /etc still forbidden
		{
			name: "28-trust-does-not-bypass-denylist",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				return allowRoot, "/etc", denyEtcOnly, true, ""
			},
			wantErr:    true,
			wantSubstr: "denylist",
		},
		// 29. AllowTrust=true, path outside root → pass
		{
			name: "29-trust-bypasses-rel-check",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				tgt := filepath.Join(tmpRoot, "elsewhere")
				if err := os.Mkdir(tgt, 0o755); err != nil {
					t.Fatalf("mkdir elsewhere: %v", err)
				}
				canon, _ := filepath.EvalSymlinks(tgt)
				return allowRoot, tgt, macOSDenylist, true, canon
			},
			wantErr: false,
		},
		// 30. invalid-reason not validator concern (CLI-level); the row
		// asserts that Validate with trust+empty-reason is identical to
		// trust+any-reason (Policy doesn't see the reason string).
		{
			name: "30-trust-reason-not-validator-concern",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				tgt := filepath.Join(tmpRoot, "trust-noreason")
				if err := os.Mkdir(tgt, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				canon, _ := filepath.EvalSymlinks(tgt)
				return allowRoot, tgt, macOSDenylist, true, canon
			},
			wantErr: false,
		},
		// 31. path length cap
		{
			name:       "31-path-too-long",
			allowRoot:  allowRoot,
			input:      "/" + strings.Repeat("a", paths.MaxPathLen+10),
			wantErr:    true,
			wantSubstr: "too long",
		},
		// 32. trailing-space path OK (POSIX permits)
		{
			name:        "32-trailing-space-ok",
			allowRoot:   allowRoot,
			input:       trailSpace,
			wantErr:     false,
			wantCanonEQ: trailCanon,
		},
		// 33. /var/run/docker.sock rejected (beta.2 PR-D). The literal
		// entry appears in DefaultDenylist BEFORE /var so the exact-match
		// branch of the validator fires and the error names the socket
		// entry explicitly rather than the broader /var descendant match.
		// Skipped on machines without a docker.sock (EvalSymlinks errors
		// before the denylist check runs) — same pattern as row 12/13/14.
		{
			name: "33-docker-sock-var-run",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				return allowRoot, "/var/run/docker.sock", fullDenylist, false, ""
			},
			wantErr:    true,
			wantSubstr: "docker",
			skipIf: func(t *testing.T) bool {
				_, err := os.Stat("/var/run/docker.sock")
				return err != nil
			},
		},
		// 34. /run/docker.sock rejected (systemd-managed Linux variant).
		// Literal denylist entry; same exact-match-before-descendant
		// ordering concern as row 33.
		{
			name: "34-docker-sock-run",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				return allowRoot, "/run/docker.sock", fullDenylist, false, ""
			},
			wantErr:    true,
			wantSubstr: "docker",
			skipIf: func(t *testing.T) bool {
				_, err := os.Stat("/run/docker.sock")
				return err != nil
			},
		},
		// 35. Docker Desktop macOS socket rejected via the substring-match
		// helper (isDockerDesktopSocket). The per-user path component
		// rules out a literal denylist entry, so we assert the helper
		// fires by constructing a path whose canonical resolution lives
		// under /Library/Containers/com.docker.docker/ and ends in
		// docker-raw.sock. Since the actual socket is runtime-created
		// by Docker Desktop, this row stages a fake directory tree under
		// tmpRoot and symlinks it into the expected shape — no live
		// docker.sock required.
		{
			name: "35-docker-sock-desktop-macos",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				// Build:
				//   tmpRoot/fake-user/Library/Containers/com.docker.docker/Data/docker-raw.sock
				// as an empty regular file so EvalSymlinks succeeds.
				bundle := filepath.Join(tmpRoot, "fake-user", "Library", "Containers", "com.docker.docker", "Data")
				if err := os.MkdirAll(bundle, 0o755); err != nil {
					t.Fatalf("mkdir bundle: %v", err)
				}
				sock := filepath.Join(bundle, "docker-raw.sock")
				if err := os.WriteFile(sock, nil, 0o600); err != nil {
					t.Fatalf("write fake sock: %v", err)
				}
				return allowRoot, sock, macOSDenylist, false, ""
			},
			wantErr:    true,
			wantSubstr: "Docker Desktop control socket",
			skipIf: func(t *testing.T) bool {
				// The suffix match is case-insensitive and the tree we
				// build has nothing Darwin-specific — runs on any OS
				// where filepath.EvalSymlinks accepts a regular file
				// (all of darwin/linux).
				return false
			},
		},
		// 36. Trailing-slash variant canonicalizes to the same path and
		// must hit the same denylist rejection as row 33. This guards
		// against a naive string-compare implementation that treated
		// "/var/run/docker.sock/" as distinct from "/var/run/docker.sock".
		// Clean() strips the trailing slash before EvalSymlinks + denylist.
		{
			name: "36-docker-sock-trailing-slash",
			runSetup: func(t *testing.T) (string, string, []string, bool, string) {
				return allowRoot, "/var/run/docker.sock/", fullDenylist, false, ""
			},
			wantErr:    true,
			wantSubstr: "docker",
			skipIf: func(t *testing.T) bool {
				_, err := os.Stat("/var/run/docker.sock")
				return err != nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipIf != nil && tc.skipIf(t) {
				t.Skipf("skipIf: precondition not met for %s", tc.name)
				return
			}
			ar, in, dl, trust, canonOverride := tc.allowRoot, tc.input, macOSDenylist, tc.allowTrust, ""
			if tc.runSetup != nil {
				ar, in, dl, trust, canonOverride = tc.runSetup(t)
			}
			pol := paths.Policy{
				AllowRoot:  ar,
				Denylist:   dl,
				AllowTrust: trust,
			}
			got, err := pol.Validate(in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: expected error, got canonical=%q", tc.name, got)
				}
				if !errors.Is(err, paths.ErrWorkspaceForbidden) {
					t.Fatalf("%s: expected ErrWorkspaceForbidden, got %v", tc.name, err)
				}
				if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
					t.Fatalf("%s: expected error to contain %q, got %q", tc.name, tc.wantSubstr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			want := tc.wantCanonEQ
			if canonOverride != "" {
				want = canonOverride
			}
			if want == "__APFS_CASEFOLD_MATCH__" {
				// APFS returns either casing depending on how EvalSymlinks
				// was invoked. Accept any case-fold-equal result.
				if !strings.EqualFold(got, p1Canon) {
					t.Fatalf("%s: canonical fold-mismatch: got %q want case-fold of %q", tc.name, got, p1Canon)
				}
			} else if want != "" && got != want {
				t.Fatalf("%s: canonical mismatch: got %q want %q", tc.name, got, want)
			}
		})
	}
}

// TestDefaultDenylistContainsWindowsEntriesOnWindows asserts that on a
// Windows build, paths.DefaultDenylist contains the well-known
// system-path entries (C:\Windows, C:\Program Files, etc.) appended by
// buildDefaultDenylist's runtime.GOOS check.
//
// Skipped on non-Windows because the entries are deliberately absent
// there — including them would either need case-insensitive comparison
// against POSIX paths (false positives) or platform-specific quirks
// that don't apply.
func TestDefaultDenylistContainsWindowsEntriesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("Windows denylist entries only present on Windows builds; GOOS=%s", runtime.GOOS)
	}
	want := []string{
		`C:\Windows`,
		`C:\Program Files`,
		`C:\Program Files (x86)`,
		`C:\ProgramData`,
		`C:\Users\Default`,
		`C:\Users\Public`,
		`C:\Users\All Users`,
	}
	for _, w := range want {
		found := false
		for _, e := range paths.DefaultDenylist {
			if e == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DefaultDenylist missing Windows entry %q", w)
		}
	}
}

// TestPolicyValidateWindowsDenylist asserts Policy.Validate rejects the
// Windows-system paths through the normal denylist path. The validator's
// existing case-insensitive EqualFold + isUnderFold logic applies
// unchanged; this test only confirms the new denylist entries land where
// we expect.
//
// Skipped on non-Windows: filepath.EvalSymlinks behavior on a path like
// `C:\Windows` is undefined on POSIX systems (the leading drive letter
// looks like a relative path and resolves under cwd, leading to a
// "no such file or directory" error before the denylist check fires).
// The Windows-gated tests are scaffolding that activates when dclaw
// gains real Windows CI; until then this skip is the expected outcome.
func TestPolicyValidateWindowsDenylist(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("Windows-path semantics required for this test; GOOS=%s", runtime.GOOS)
	}
	cases := []struct {
		name  string
		input string
	}{
		{"windows-system32", `C:\Windows\System32`},
		{"program-files-foo", `C:\Program Files\Foo`},
		{"program-files-x86-bar", `C:\Program Files (x86)\Bar`},
		{"programdata-baz", `C:\ProgramData\Baz`},
		{"users-default", `C:\Users\Default\AppData`},
		{"users-public", `C:\Users\Public\Documents`},
		{"users-all-users", `C:\Users\All Users\State`},
	}
	pol := paths.Policy{
		// AllowRoot deliberately empty: each denylist entry should
		// reject before the rel-check runs. Even with an allow-root
		// configured, the denylist takes precedence.
		Denylist: paths.DefaultDenylist,
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pol.Validate(tc.input)
			if err == nil {
				t.Fatalf("%s: expected denylist rejection, got nil", tc.name)
			}
			if !errors.Is(err, paths.ErrWorkspaceForbidden) {
				t.Fatalf("%s: expected ErrWorkspaceForbidden, got %v", tc.name, err)
			}
			if !strings.Contains(err.Error(), "denylist") {
				t.Fatalf("%s: expected error to mention denylist, got %q", tc.name, err.Error())
			}
		})
	}
}
