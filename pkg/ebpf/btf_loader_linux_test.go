//go:build linux

package ebpf

import (
	"archive/tar"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ulikunitz/xz"
	"github.com/yourorg/diffkeeper/pkg/config"
)

func TestBuildBTFHubURL(t *testing.T) {
	info := kernelInfo{
		Distro:        "ubuntu",
		VersionID:     "22.04",
		KernelRelease: "5.15.0-test",
		Arch:          "x86_64",
	}

	url := buildBTFHubURL("https://example.com/base", info)
	want := "https://example.com/base/ubuntu/22.04/x86_64/5.15.0-test.btf.tar.xz"
	if url != want {
		t.Fatalf("unexpected BTFHub URL\nwant: %s\ngot : %s", want, url)
	}
}

func TestDownloadAndCacheBTF(t *testing.T) {
	tarBytes := buildBTFTar(t, "dummy content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(tarBytes); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig().EBPF
	cfg.BTF.CacheDir = t.TempDir()
	cfg.BTF.AllowDownload = true
	cfg.BTF.HubMirror = server.URL

	loader := NewBTFLoader(&cfg)
	info := kernelInfo{
		Distro:        "ubuntu",
		VersionID:     "22.04",
		KernelRelease: "5.15.0-test",
		Arch:          "x86_64",
	}
	dest := filepath.Join(cfg.BTF.CacheDir, info.KernelRelease+".btf")

	path, err := loader.downloadAndCache(context.Background(), info, dest)
	if err != nil {
		t.Fatalf("downloadAndCache failed: %v", err)
	}

	if path != dest {
		t.Fatalf("expected dest path %s, got %s", dest, path)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read cached BTF: %v", err)
	}

	if string(data) != "dummy content" {
		t.Fatalf("unexpected BTF contents: %q", string(data))
	}
}

func buildBTFTar(t *testing.T, payload string) []byte {
	t.Helper()

	var buf bytes.Buffer
	xzw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create xz writer: %v", err)
	}
	tw := tar.NewWriter(xzw)

	content := []byte(payload)
	if err := tw.WriteHeader(&tar.Header{
		Name: "core.btf",
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatalf("failed to write header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if err := xzw.Close(); err != nil {
		t.Fatalf("failed to close xz writer: %v", err)
	}
	return buf.Bytes()
}
