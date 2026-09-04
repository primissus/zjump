package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const updateHelp = `Usage: zjump update [OPTIONS]

Update zjump to a newer version from GitHub Releases. Downloads the
archive matching this platform, verifies its SHA256 against the release's
checksums.txt, and atomically replaces the running binary.

Flags:
    --check            Print whether a newer release is available and exit
                       without modifying anything
    --force            Reinstall even when already at the latest version
    --version vX.Y.Z   Install a specific release tag instead of the latest
    --prerelease       Include prereleases when resolving the latest version
`

// updateRepoOwner/updateRepoName identify the GitHub repository whose releases
// the updater consumes.
const (
	updateRepoOwner = "primissus"
	updateRepoName  = "zjump"
)

// Test seams: overridable so the integration tests can stub the executable
// path and the GitHub API without touching the test runner's own binary.
var (
	executablePath = os.Executable
	githubAPIBase  = "https://api.github.com"
	updateClient   = &http.Client{Timeout: 30 * time.Second}
)

// ghRelease is the subset of the GitHub Releases API payload the updater
// needs. See https://docs.github.com/rest/releases/releases.
type ghRelease struct {
	TagName    string    `json:"tag_name"`
	HTMLURL    string    `json:"html_url"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var errReleaseNotFound = errors.New("release not found")

// runUpdate implements `zjump update`.
func runUpdate(args []string) error {
	fs := newFlagSet("update")
	var check, force, prerelease bool
	var version string
	fs.BoolVar(&check, "check", false, "")
	fs.BoolVar(&force, "force", false, "")
	fs.BoolVar(&prerelease, "prerelease", false, "")
	fs.StringVar(&version, "version", "", "")

	positionals, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "update", updateHelp)
			return nil
		}
		return err
	}
	if len(positionals) > 0 {
		return fmt.Errorf("update: unexpected argument %q", positionals[0])
	}
	if version != "" && prerelease {
		return fmt.Errorf("update: --version and --prerelease are mutually exclusive")
	}

	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	// Resolve symlinks so the rename targets the real binary (e.g. a Homebrew
	// symlink into the Cellar).
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	target, err := resolveRelease(version, prerelease)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	// Compare the current embedded version against the target release tag.
	// compareVersions(current, target) >= 0 means we are not behind.
	behind, err := compareVersions(Version, target.TagName)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	if check {
		if behind >= 0 {
			fmt.Printf("zjump %s is up to date\n", Version)
		} else {
			fmt.Printf("zjump %s -> %s available\n", Version, stripV(target.TagName))
		}
		return nil
	}

	if behind >= 0 && !force {
		fmt.Printf("zjump already up to date (%s)\n", Version)
		return nil
	}

	binAsset, ok := findAsset(target, binaryAssetName(target.TagName))
	if !ok {
		return fmt.Errorf("update: no release asset for %s/%s (expected %q); see %s",
			runtime.GOOS, runtime.GOARCH, binaryAssetName(target.TagName), target.HTMLURL)
	}
	sumAsset, ok := findAsset(target, "checksums.txt")
	if !ok {
		return fmt.Errorf("update: release %s has no checksums.txt asset", target.TagName)
	}

	dir := filepath.Dir(exe)
	tarPath, err := os.CreateTemp("", updateRepoName+"-download-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(tarPath.Name())
	tarPath.Close()

	newBin, err := os.CreateTemp(dir, "."+updateRepoName+".*.new")
	if err != nil {
		return fmt.Errorf("update: cannot write to %s (try 'go install github.com/primissus/zjump/cmd/zjump@latest'): %w", dir, err)
	}
	newBin.Close()
	defer os.Remove(newBin.Name())

	sumPath, err := os.CreateTemp("", "zjump-checksums-*")
	if err != nil {
		return err
	}
	defer os.Remove(sumPath.Name())
	if err := download(sumAsset.BrowserDownloadURL, sumPath.Name()); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	sumPath.Close()

	if err := download(binAsset.BrowserDownloadURL, tarPath.Name()); err != nil {
		return fmt.Errorf("update: %w", err)
	}

	if err := verifyChecksum(sumPath.Name(), binaryAssetName(target.TagName), tarPath.Name()); err != nil {
		return fmt.Errorf("update: %w", err)
	}

	if err := extractBinary(tarPath.Name(), newBin.Name()); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := os.Chmod(newBin.Name(), 0o755); err != nil {
		return fmt.Errorf("update: %w", err)
	}

	if err := os.Rename(newBin.Name(), exe); err != nil {
		return fmt.Errorf("update: cannot replace %s: %w", exe, err)
	}

	fmt.Printf("Updated zjump %s -> %s\n", Version, stripV(target.TagName))
	return nil
}

// binaryAssetName returns the goreleaser archive name for this platform, e.g.
// zjump_0.5.0_darwin_arm64.tar.gz. Goreleaser's {{ .Version }} drops the 'v'.
func binaryAssetName(tag string) string {
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", updateRepoName, stripV(tag), runtime.GOOS, runtime.GOARCH)
}

func stripV(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

// ensureV normalizes a tag or version to the GitHub tag spelling used by this
// project ("v" prefix), accepting input with or without it.
func ensureV(tag string) string {
	tag = stripV(tag)
	if tag == "" {
		return ""
	}
	return "v" + tag
}

// resolveRelease finds the release to install. A pinned --version uses the
// tags endpoint; otherwise the latest release is used, honouring --prerelease.
func resolveRelease(version string, prerelease bool) (*ghRelease, error) {
	switch {
	case version != "":
		rel := &ghRelease{}
		if err := ghGet(fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s",
			githubAPIBase, updateRepoOwner, updateRepoName, url.PathEscape(ensureV(version))), rel); err != nil {
			if errors.Is(err, errReleaseNotFound) {
				return nil, fmt.Errorf("version %s not found", version)
			}
			return nil, err
		}
		return rel, nil
	case prerelease:
		var releases []ghRelease
		if err := ghGet(fmt.Sprintf("%s/repos/%s/%s/releases?per_page=100",
			githubAPIBase, updateRepoOwner, updateRepoName), &releases); err != nil {
			return nil, err
		}
		return pickNewest(releases)
	default:
		rel := &ghRelease{}
		if err := ghGet(fmt.Sprintf("%s/repos/%s/%s/releases/latest",
			githubAPIBase, updateRepoOwner, updateRepoName), rel); err != nil {
			return nil, err
		}
		return rel, nil
	}
}

// pickNewest returns the highest-versioned, non-draft release. Unparsable
// tags are not comparable and are skipped for selection (H2: a sort with an
// inconsistent comparator yields an arbitrary order).
func pickNewest(releases []ghRelease) (*ghRelease, error) {
	best := -1
	for i := range releases {
		if releases[i].Draft {
			continue
		}
		if _, err := parseSemver(releases[i].TagName); err != nil {
			continue // unparsable tag: not comparable, skip for selection
		}
		if best == -1 {
			best = i
			continue
		}
		c, err := compareVersions(releases[i].TagName, releases[best].TagName)
		if err != nil {
			continue // defensive: best is parsable, so this is unreachable
		}
		if c > 0 {
			best = i
		}
	}
	if best == -1 {
		return nil, errors.New("no published releases found")
	}
	return &releases[best], nil
}

func findAsset(rel *ghRelease, name string) (*ghAsset, bool) {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i], true
		}
	}
	return nil, false
}

// ghGet GETs a GitHub API endpoint and decodes the JSON response.
func ghGet(url string, out any) error {
	resp, err := updateClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return errReleaseNotFound
	}
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-Ratelimit-Remaining") == "0" {
		return errors.New("GitHub API rate limit reached; retry later")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API %s returned %s", url, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// download streams url into an existing empty file.
func download(url string, dest string) error {
	resp, err := updateClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned %s", url, resp.Status)
	}

	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

// verifyChecksum checks path against the sha256 entry for name in the
// checksums.txt at sumPath. Goreleaser writes "<sha256>  <filename>".
func verifyChecksum(sumPath, name, path string) error {
	data, err := os.ReadFile(sumPath)
	if err != nil {
		return err
	}

	want := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %q", name)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s (got %s, want %s)", name, got, want)
	}
	return nil
}

// extractBinary unpacks the first regular file entry named "zjump" from the
// goreleaser tar.gz at archivePath into dest.
func extractBinary(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != updateRepoName {
			continue
		}

		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
		return nil
	}
	return fmt.Errorf("archive %s contains no %s binary", archivePath, updateRepoName)
}

// semver is a minimal parsed semantic version.
type semver struct {
	major, minor, patch int
	pre                 string
}

func parseSemver(s string) (semver, error) {
	s = stripV(s)
	core, pre, _ := strings.Cut(s, "-")
	nums := strings.Split(core, ".")
	if len(nums) < 3 {
		return semver{}, fmt.Errorf("invalid version %q", s)
	}
	vals := [3]int{}
	for i, n := range nums[:3] {
		v, err := strconv.Atoi(n)
		if err != nil {
			return semver{}, fmt.Errorf("invalid version %q", s)
		}
		vals[i] = v
	}
	return semver{vals[0], vals[1], vals[2], pre}, nil
}

// compareVersions returns -1, 0, or 1 as a < b, a == b, or a > b per semver
// precedence (releases sort before prereleases for the same core).
func compareVersions(a, b string) (int, error) {
	av, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseSemver(b)
	if err != nil {
		return 0, err
	}

	switch {
	case av.major < bv.major:
		return -1, nil
	case av.major > bv.major:
		return 1, nil
	case av.minor < bv.minor:
		return -1, nil
	case av.minor > bv.minor:
		return 1, nil
	case av.patch < bv.patch:
		return -1, nil
	case av.patch > bv.patch:
		return 1, nil
	}
	return comparePre(av.pre, bv.pre), nil
}

// comparePre compares prerelease strings: numeric identifiers compare
// numerically, alphanumeric ones by prefix then trailing number (so rc9 <
// rc10), numeric sorts before alphanumeric, and a release (empty pre) beats
// any prerelease.
func comparePre(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}

	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, bi := as[i], bs[i]
		if ai == bi {
			continue
		}
		an, bn := isNumeric(ai), isNumeric(bi)
		if an != bn {
			if an {
				return -1
			}
			return 1
		}
		if an && bn {
			x, _ := strconv.Atoi(ai)
			y, _ := strconv.Atoi(bi)
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
		} else if c := compareAlphaNumeric(ai, bi); c != 0 {
			return c
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// compareAlphaNumeric compares identifiers like "rc10" by splitting into a
// letter prefix and trailing number ("rc", 10).
func compareAlphaNumeric(a, b string) int {
	ap, an := splitAlphaNum(a)
	bp, bn := splitAlphaNum(b)
	if ap != bp {
		if ap < bp {
			return -1
		}
		return 1
	}
	if an == bn {
		if a < b {
			return -1
		}
		return 1
	}
	if an < bn {
		return -1
	}
	return 1
}

func splitAlphaNum(s string) (string, int) {
	i := 0
	for i < len(s) && (s[i] < '0' || s[i] > '9') {
		i++
	}
	num, _ := strconv.Atoi(s[i:])
	return s[:i], num
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
