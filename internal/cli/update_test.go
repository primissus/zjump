package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateFixture stubs the GitHub API and the executable path so runUpdate can
// be exercised without touching the real network or the test runner's binary.
type updateFixture struct {
	t           *testing.T
	srv         *httptest.Server
	exec        string
	latest      map[string]any
	list        []map[string]any
	tags        map[string]map[string]any
	assets      map[string][]byte
	rateLimited bool
}

func newUpdateFixture(t *testing.T) *updateFixture {
	fx := &updateFixture{
		t:      t,
		exec:   filepath.Join(t.TempDir(), "zjump"),
		tags:   map[string]map[string]any{},
		assets: map[string][]byte{},
	}
	if err := os.WriteFile(fx.exec, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldExe := executablePath
	oldAPI := githubAPIBase
	t.Cleanup(func() {
		executablePath = oldExe
		githubAPIBase = oldAPI
		fx.srv.Close()
	})
	executablePath = func() (string, error) { return fx.exec, nil }

	fx.srv = httptest.NewServer(http.HandlerFunc(fx.handle))
	githubAPIBase = fx.srv.URL
	return fx
}

func (fx *updateFixture) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/releases/latest"):
		if fx.rateLimited {
			w.Header().Set("X-Ratelimit-Remaining", "0")
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		if fx.latest == nil {
			http.Error(w, "no latest", http.StatusNotFound)
			return
		}
		writeJSON(w, fx.latest)
	case strings.Contains(r.URL.Path, "/releases/tags/"):
		tag := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "")
		rel, ok := fx.tags[tag]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, rel)
	case strings.HasSuffix(r.URL.Path, "/releases"):
		writeJSON(w, fx.list)
	case strings.HasPrefix(r.URL.Path, "/assets/"):
		name := strings.TrimPrefix(r.URL.Path, "/assets/")
		data, ok := fx.assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

// release builds a release payload whose assets are served by the fixture and
// records the tarballs plus a matching checksums.txt for the given assets.
func (fx *updateFixture) release(tag string, prerelease bool, assets ...string) map[string]any {
	var as []map[string]any
	var sb strings.Builder
	for _, name := range assets {
		data := makeTarball(tag)
		fx.assets[name] = data
		as = append(as, map[string]any{
			"name":                 name,
			"browser_download_url": fx.srv.URL + "/assets/" + name,
		})
		fmt.Fprintf(&sb, "%s  %s\n", sha256hex(data), name)
	}
	if len(assets) > 0 {
		fx.assets["checksums.txt"] = []byte(sb.String())
		as = append(as, map[string]any{
			"name":                 "checksums.txt",
			"browser_download_url": fx.srv.URL + "/assets/checksums.txt",
		})
	}
	return map[string]any{
		"tag_name":   tag,
		"html_url":   fx.srv.URL + "/releases/tag/" + tag,
		"draft":      false,
		"prerelease": prerelease,
		"assets":     as,
	}
}

// setLatest configures /releases/latest and the per-tag endpoint for tag.
func (fx *updateFixture) setLatest(tag string, prerelease bool) {
	rel := fx.release(tag, prerelease, binaryAssetName(tag))
	fx.latest = rel
	fx.tags[tag] = rel
}

// setList configures the /releases list endpoint.
func (fx *updateFixture) setList(tags ...string) {
	fx.list = nil
	for _, tag := range tags {
		prerelease := strings.Contains(tag, "-")
		rel := fx.release(tag, prerelease, binaryAssetName(tag))
		fx.tags[tag] = rel
		fx.list = append(fx.list, rel)
	}
}

func makeTarball(tag string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("new-binary " + tag)
	tw.WriteHeader(&tar.Header{Name: "zjump", Mode: 0o755, Size: int64(len(content))})
	tw.Write(content)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func runCapture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = runUpdate(args)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String(), err
}

func TestUpdateUpToDate(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v"+Version, false)

	out, err := runCapture(t)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "already up to date") {
		t.Errorf("output = %q, want 'already up to date'", out)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "old-binary" {
		t.Errorf("binary was modified: %q", data)
	}
}

func TestUpdateNewerAvailable(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v0.99.0", false)

	out, err := runCapture(t)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Updated zjump "+Version+" -> 0.99.0") {
		t.Errorf("output = %q", out)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "new-binary v0.99.0" {
		t.Errorf("binary was not replaced: %q", data)
	}
}

func TestUpdateCheck(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v0.99.0", false)

	out, err := runCapture(t, "--check")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, Version+" -> 0.99.0 available") {
		t.Errorf("output = %q", out)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "old-binary" {
		t.Errorf("binary was modified by --check: %q", data)
	}
}

func TestUpdateCheckUpToDate(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v"+Version, false)

	out, err := runCapture(t, "--check")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "is up to date") {
		t.Errorf("output = %q", out)
	}
}

func TestUpdateForce(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v"+Version, false)

	out, err := runCapture(t, "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Updated zjump "+Version+" -> "+Version) {
		t.Errorf("output = %q", out)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "new-binary v"+Version {
		t.Errorf("binary was not replaced: %q", data)
	}
}

func TestUpdateVersion(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v0.99.0", false)
	fx.setList("v0.98.0", "v0.97.0")
	fx.tags["v1.2.3"] = fx.release("v1.2.3", false, binaryAssetName("v1.2.3"))

	out, err := runCapture(t, "--version", "v1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Updated zjump "+Version+" -> 1.2.3") {
		t.Errorf("output = %q", out)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "new-binary v1.2.3" {
		t.Errorf("binary was not replaced: %q", data)
	}
}

func TestUpdateVersionMissing(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v"+Version, false)

	_, err := runCapture(t, "--version", "v9.9.9")
	if err == nil {
		t.Fatal("expected error for missing version")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v", err)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "old-binary" {
		t.Errorf("binary was modified: %q", data)
	}
}

func TestUpdatePrerelease(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v0.98.0", false) // stable latest
	fx.setList("v0.98.0", "v0.99.0-rc1")

	out, err := runCapture(t, "--prerelease")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Updated zjump "+Version+" -> 0.99.0-rc1") {
		t.Errorf("output = %q", out)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "new-binary v0.99.0-rc1" {
		t.Errorf("binary was not replaced: %q", data)
	}
}

func TestUpdateChecksumMismatch(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v0.99.0", false)
	// Corrupt the tarball that the asset URL serves.
	name := binaryAssetName("v0.99.0")
	fx.assets[name] = []byte("tampered")
	fx.assets["checksums.txt"] = []byte(sha256hex(makeTarball("v0.99.0")) + "  " + name + "\n")

	_, err := runCapture(t)
	if err == nil {
		t.Fatal("expected checksum error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v", err)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "old-binary" {
		t.Errorf("binary was modified: %q", data)
	}
}

func TestUpdateUnsupportedPlatform(t *testing.T) {
	fx := newUpdateFixture(t)
	rel := fx.release("v0.99.0", false, "zjump_0.99.0_other_other.tar.gz")
	fx.latest = rel

	_, err := runCapture(t)
	if err == nil {
		t.Fatal("expected error for missing asset")
	}
	if !strings.Contains(err.Error(), "no release asset") {
		t.Errorf("error = %v", err)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "old-binary" {
		t.Errorf("binary was modified: %q", data)
	}
}

func TestUpdateRateLimit(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.latest = map[string]any{"error": "rate limited"}
	fx.rateLimited = true

	_, err := runCapture(t)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error = %v", err)
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "old-binary" {
		t.Errorf("binary was modified: %q", data)
	}
}

func TestUpdateMalformedArchive(t *testing.T) {
	fx := newUpdateFixture(t)
	name := binaryAssetName("v0.99.0")
	rel := fx.release("v0.99.0", false, name)
	fx.latest = rel
	// Corrupt the served tarball and checksum it so the failure is in the
	// extraction step, not the checksum verification.
	fx.assets[name] = []byte("not a tarball")
	fx.assets["checksums.txt"] = []byte(sha256hex([]byte("not a tarball")) + "  " + name + "\n")

	_, err := runCapture(t)
	if err == nil {
		t.Fatal("expected archive error")
	}
	if data, _ := os.ReadFile(fx.exec); string(data) != "old-binary" {
		t.Errorf("binary was modified: %q", data)
	}
}

func TestUpdateUnexpectedArgs(t *testing.T) {
	fx := newUpdateFixture(t)
	fx.setLatest("v"+Version, false)

	_, err := runCapture(t, "extra")
	if err == nil {
		t.Fatal("expected error for unexpected arg")
	}
}

func TestUpdateHelp(t *testing.T) {
	out, err := runCapture(t, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "zjump "+Version) || !strings.Contains(out, "--prerelease") {
		t.Errorf("help output = %q", out)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.5.0", "0.5.0", 0},
		{"0.5.0", "0.6.0", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.5.0", "0.5.1", -1},
		{"0.6.0", "0.5.9", 1},
		{"v0.5.0", "0.5.0", 0},
		{"0.6.0-rc1", "0.6.0", -1},
		{"0.6.0", "0.6.0-rc1", 1},
		{"0.6.0-rc2", "0.6.0-rc1", 1},
		{"0.6.0-rc10", "0.6.0-rc9", 1},
		{"0.6.0-alpha", "0.6.0-beta", -1},
		{"0.5.0", "0.5.0-alpha", 1},
		{"0.6.0-rc1", "0.6.0-rc1", 0},
	}
	for _, c := range cases {
		got, err := compareVersions(c.a, c.b)
		if err != nil {
			t.Fatalf("compareVersions(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
