//go:build linux

package ebpf

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cilium/ebpf/btf"
	"github.com/ulikunitz/xz"
	"github.com/yourorg/diffkeeper/pkg/config"
)

const (
	systemBTFPath = "/sys/kernel/btf/vmlinux"
	osReleasePath = "/etc/os-release"
	osReleaseSep  = "="
)

// BTFLoader discovers or downloads BTF specs for CO-RE relocations.
type BTFLoader struct {
	cacheDir      string
	allowDownload bool
	baseURL       string
	client        *http.Client
}

// NewBTFLoader constructs a loader based on CLI/env configuration.
func NewBTFLoader(cfg *config.EBPFConfig) *BTFLoader {
	if cfg == nil {
		return nil
	}

	cache := cfg.BTF.CacheDir
	if cache == "" {
		cache = filepath.Join(os.TempDir(), "diffkeeper", "btf")
	}

	baseURL := strings.TrimSuffix(cfg.BTF.HubMirror, "/")
	if baseURL == "" {
		baseURL = "https://github.com/aquasecurity/btfhub-archive/raw/main"
	}

	return &BTFLoader{
		cacheDir:      cache,
		allowDownload: cfg.BTF.AllowDownload,
		baseURL:       baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// LoadSpec returns a usable BTF spec and the source path it originated from.
func (l *BTFLoader) LoadSpec(ctx context.Context) (*btf.Spec, string, error) {
	if l == nil {
		return nil, "", fmt.Errorf("btf loader not configured")
	}

	if spec, err := btf.LoadSpec(systemBTFPath); err == nil {
		return spec, systemBTFPath, nil
	}

	if err := os.MkdirAll(l.cacheDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create btf cache dir: %w", err)
	}

	info, err := detectKernelInfo()
	if err != nil {
		return nil, "", err
	}

	cachedPath := filepath.Join(l.cacheDir, fmt.Sprintf("%s.btf", info.KernelRelease))
	if _, err := os.Stat(cachedPath); err == nil {
		spec, loadErr := btf.LoadSpec(cachedPath)
		return spec, cachedPath, loadErr
	}

	if !l.allowDownload {
		return nil, "", fmt.Errorf("no system BTF found and downloads disabled (expected cache at %s)", cachedPath)
	}

	path, err := l.downloadAndCache(ctx, info, cachedPath)
	if err != nil {
		return nil, "", err
	}

	spec, loadErr := btf.LoadSpec(path)
	return spec, path, loadErr
}

func (l *BTFLoader) downloadAndCache(ctx context.Context, info kernelInfo, destPath string) (string, error) {
	url := buildBTFHubURL(l.baseURL, info)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request for %s: %w", url, err)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download BTF from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("btfhub download failed (%s): %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(l.cacheDir, "btfhub-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return "", fmt.Errorf("write temp BTF archive: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	if strings.HasSuffix(strings.ToLower(url), ".btf") {
		if err := os.Rename(tmp.Name(), destPath); err != nil {
			return "", fmt.Errorf("move BTF file: %w", err)
		}
		return destPath, nil
	}

	if err := extractBTFArchive(tmp.Name(), destPath); err != nil {
		return "", err
	}
	return destPath, nil
}

func extractBTFArchive(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open BTF archive: %w", err)
	}
	defer f.Close()

	xzReader, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("init xz reader: %w", err)
	}

	tarReader := tar.NewReader(xzReader)
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		if !strings.HasSuffix(hdr.Name, ".btf") {
			continue
		}

		if err := writeFileFromTar(destPath, tarReader, hdr.FileInfo().Mode()); err != nil {
			return err
		}
		return nil
	}

	return fmt.Errorf("btf archive did not contain .btf file")
}

func writeFileFromTar(path string, r io.Reader, mode os.FileMode) error {
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create cached BTF: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return fmt.Errorf("write cached BTF: %w", err)
	}

	if err := out.Chmod(mode); err != nil {
		return fmt.Errorf("chmod cached BTF: %w", err)
	}

	return nil
}

type kernelInfo struct {
	Distro        string
	VersionID     string
	KernelRelease string
	Arch          string
}

func detectKernelInfo() (kernelInfo, error) {
	release, err := readTrimmed("/proc/sys/kernel/osrelease")
	if err != nil {
		return kernelInfo{}, fmt.Errorf("read kernel release: %w", err)
	}

	arch, err := normalizeArch(runtime.GOARCH)
	if err != nil {
		return kernelInfo{}, err
	}

	osMeta, err := parseOSRelease()
	if err != nil {
		return kernelInfo{}, err
	}

	return kernelInfo{
		Distro:        osMeta["ID"],
		VersionID:     osMeta["VERSION_ID"],
		KernelRelease: release,
		Arch:          arch,
	}, nil
}

func parseOSRelease() (map[string]string, error) {
	data, err := os.ReadFile(osReleasePath)
	if err != nil {
		return map[string]string{
			"ID":         "unknown",
			"VERSION_ID": "unknown",
		}, nil
	}

	meta := map[string]string{
		"ID":         "unknown",
		"VERSION_ID": "unknown",
	}

	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		parts := bytes.SplitN(line, []byte(osReleaseSep), 2)
		if len(parts) != 2 {
			continue
		}
		key := string(parts[0])
		val := strings.Trim(string(parts[1]), `"`)
		meta[key] = strings.ToLower(val)
	}
	return meta, nil
}

func readTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func normalizeArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "arm64", nil
	case "ppc64le":
		return "ppc64le", nil
	default:
		return "", fmt.Errorf("unsupported architecture for BTFHub: %s", goarch)
	}
}

func buildBTFHubURL(base string, info kernelInfo) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s.btf.tar.xz",
		strings.TrimSuffix(base, "/"),
		info.Distro,
		info.VersionID,
		info.Arch,
		info.KernelRelease)
}
