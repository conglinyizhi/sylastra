package bootstrap

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/conglinyizhi/sylastra/internal/config"
)

// EnsureMCP checks for better-edit-tools at the fallback path.
// If missing (or force==true), downloads the latest release from GitHub.
// Returns the resolved binary path and whether it was downloaded or pre-existing.
func EnsureMCP(ctx context.Context, force bool) (string, error) {
	fallbackPath, err := config.DefaultFallbackMCPPath()
	if err != nil {
		return "", err
	}

	// Check if already exists
	if !force {
		if info, statErr := os.Stat(fallbackPath); statErr == nil && !info.IsDir() {
			return fallbackPath, nil
		}
	}

	// Download latest release
	tag, err := fetchLatestReleaseTag(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}

	osArch := runtime.GOOS + "-" + runtime.GOARCH
	archiveName := fmt.Sprintf("better-edit-tools-%s.tar.gz", osArch)
	downloadURL := fmt.Sprintf("https://github.com/conglinyizhi/better-edit-tools-mcp/releases/download/%s/%s", tag, archiveName)

	destDir := filepath.Dir(fallbackPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create mcp dir: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Downloading better-edit-tools (%s) from %s ...\n", tag, downloadURL)
	if err := downloadAndExtractMCP(ctx, downloadURL, destDir); err != nil {
		return "", fmt.Errorf("download MCP: %w", err)
	}
	if err := os.Chmod(fallbackPath, 0o755); err != nil {
		return "", fmt.Errorf("chmod MCP: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Installed to %s\n", fallbackPath)
	return fallbackPath, nil
}

func fetchLatestReleaseTag(ctx context.Context) (string, error) {
	url := "https://api.github.com/repos/conglinyizhi/better-edit-tools-mcp/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sylastra/0.1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub API: %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("no releases found for conglinyizhi/better-edit-tools-mcp")
	}
	return release.TagName, nil
}

func downloadAndExtractMCP(ctx context.Context, url, destDir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "sylastra/0.1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		name := filepath.Base(header.Name)
		if name != "better-edit-tools" && name != "better-edit-tools.exe" {
			continue
		}

		destPath := filepath.Join(destDir, name)
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(f, tr); err != nil {
			return err
		}
		return nil
	}

	return fmt.Errorf("better-edit-tools binary not found in archive (url=%s)", url)
}
