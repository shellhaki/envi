package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	releaseRepo = "shellhaki/envi"
	// Downloads are capped well above any real archive: a compromised or
	// confused host must not be able to fill the disk.
	maxArchiveBytes = 128 << 20
)

// Release is the subset of a GitHub release this CLI needs.
type Release struct {
	Tag string
}

// LatestRelease asks GitHub for the newest published release.
func LatestRelease(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+releaseRepo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("couldn't reach GitHub to check for updates: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests {
		return Release{}, errors.New("GitHub rate-limited the update check; try again shortly")
	}
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub returned %d looking up the latest release", res.StatusCode)
	}
	var body struct {
		Tag string `json:"tag_name"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, err
	}
	if body.Tag == "" {
		return Release{}, errors.New("no published release found")
	}
	return Release{Tag: body.Tag}, nil
}

// assetName mirrors the archive naming in .goreleaser.yaml. Keeping the two in
// step matters: a mismatch here is a 404 at the worst possible moment.
func assetName(version string) (string, error) {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	switch arch {
	case "amd64", "arm64":
	case "arm":
		// GOARM=7 is the only arm build published, and goreleaser names it
		// armv7 — the same mapping install.sh makes.
		arch = "armv7"
	default:
		return "", fmt.Errorf("no published build for %s/%s", goos, runtime.GOARCH)
	}
	if goos == "windows" {
		return fmt.Sprintf("envi_%s_windows_%s.zip", version, arch), nil
	}
	if goos != "darwin" && goos != "linux" {
		return "", fmt.Errorf("no published build for %s/%s", goos, runtime.GOARCH)
	}
	return fmt.Sprintf("envi_%s_%s_%s.tar.gz", version, goos, arch), nil
}

// FetchBinary downloads the release archive for this platform, verifies it
// against the release's checksums.txt, and extracts the envi binary into dir.
// Nothing is trusted until the checksum matches.
func FetchBinary(ctx context.Context, ui UI, tag, dir string) (string, error) {
	version := strings.TrimPrefix(tag, "v")
	name, err := assetName(version)
	if err != nil {
		return "", err
	}
	base := "https://github.com/" + releaseRepo + "/releases/download/" + tag

	sums, err := fetchText(ctx, base+"/checksums.txt")
	if err != nil {
		return "", fmt.Errorf("couldn't fetch checksums for %s: %w", tag, err)
	}
	want := checksumFor(sums, name)
	if want == "" {
		return "", fmt.Errorf("release %s has no checksum entry for %s", tag, name)
	}

	archive := filepath.Join(dir, name)
	sum, err := download(ctx, ui, base+"/"+name, archive)
	if err != nil {
		return "", err
	}
	// Compared before a single byte is unpacked, let alone executed.
	if sum != want {
		return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", name, want, sum)
	}
	return extractBinary(archive, dir)
}

func fetchText(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return string(b), err
}

func checksumFor(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			return fields[0]
		}
	}
	return ""
}

// download streams url to path, drawing a progress bar, and returns the
// SHA-256 of what actually landed on disk.
func download(ctx context.Context, ui UI, url, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: GitHub returned %d", res.StatusCode)
	}

	total, _ := strconv.ParseInt(res.Header.Get("Content-Length"), 10, 64)
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	bar := ui.Progress("downloading", total)
	digest := sha256.New()
	// Hash what is written, not what was promised, so the checksum covers the
	// exact bytes on disk.
	_, err = io.Copy(io.MultiWriter(file, digest), io.LimitReader(bar.Wrap(res.Body), maxArchiveBytes))
	bar.Done()
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// extractBinary pulls just the envi executable out of the archive, ignoring
// the LICENSE and README that ship alongside it.
func extractBinary(archive, dir string) (string, error) {
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(archive, dir)
	}
	return extractTarGz(archive, dir)
}

func extractTarGz(archive, dir string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		head, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.New("archive did not contain the envi binary")
		}
		if err != nil {
			return "", err
		}
		if head.Typeflag != tar.TypeReg || filepath.Base(head.Name) != "envi" {
			continue
		}
		return writeBinary(filepath.Join(dir, "envi"), tr)
	}
}

func extractZip(archive, dir string) (string, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != "envi.exe" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return writeBinary(filepath.Join(dir, "envi.exe"), rc)
	}
	return "", errors.New("archive did not contain the envi binary")
}

func writeBinary(path string, r io.Reader) (string, error) {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err = io.Copy(out, io.LimitReader(r, maxArchiveBytes)); err != nil {
		return "", err
	}
	return path, nil
}
