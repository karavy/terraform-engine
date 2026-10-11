package tfengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validBackend() S3Backend {
	return S3Backend{Endpoint: "http://minio.storage:9000", Bucket: "tfstate", Region: "us-east-1", Key: "default/vdc/acme/network.tfstate"}
}

func TestWriteS3BackendOverrideWritesAnOverrideFile(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteS3BackendOverride(dir, validBackend())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != BackendOverrideFile || !strings.HasSuffix(path, "_override.tf") {
		t.Fatalf("path = %s: Terraform only treats *_override.tf files as overrides", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`backend "s3"`, `bucket                      = "tfstate"`, `key                         = "default/vdc/acme/network.tfstate"`,
		`region                      = "us-east-1"`, `endpoints                   = { s3 = "http://minio.storage:9000" }`,
		`use_path_style              = true`, `use_lockfile                = true`,
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("backend file misses %q:\n%s", want, content)
		}
	}
}

func TestS3BackendValidateRejectsUnsafeValues(t *testing.T) {
	mutate := func(change func(*S3Backend)) S3Backend {
		backend := validBackend()
		change(&backend)
		return backend
	}
	for name, backend := range map[string]S3Backend{
		"endpoint without scheme": mutate(func(b *S3Backend) { b.Endpoint = "minio:9000" }),
		"endpoint with a quote":   mutate(func(b *S3Backend) { b.Endpoint = `http://x"y` }),
		"empty bucket":            mutate(func(b *S3Backend) { b.Bucket = "" }),
		"bucket with capitals":    mutate(func(b *S3Backend) { b.Bucket = "TfState" }),
		"empty region":            mutate(func(b *S3Backend) { b.Region = "" }),
		"empty key":               mutate(func(b *S3Backend) { b.Key = "" }),
		"leading slash":           mutate(func(b *S3Backend) { b.Key = "/a/b" }),
		"trailing slash":          mutate(func(b *S3Backend) { b.Key = "a/b/" }),
		"double slash":            mutate(func(b *S3Backend) { b.Key = "a//b" }),
		"parent segment":          mutate(func(b *S3Backend) { b.Key = "a/../b" }),
		"current segment":         mutate(func(b *S3Backend) { b.Key = "a/./b" }),
		"interpolation":           mutate(func(b *S3Backend) { b.Key = "a/${b}" }),
		"space":                   mutate(func(b *S3Backend) { b.Key = "a b" }),
		"too long":                mutate(func(b *S3Backend) { b.Key = strings.Repeat("a", 513) }),
	} {
		if err := backend.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
		if _, err := WriteS3BackendOverride(t.TempDir(), backend); err == nil {
			t.Errorf("%s: WriteS3BackendOverride must refuse an invalid backend", name)
		}
	}
}

func TestHCLStringNeutralizesInterpolation(t *testing.T) {
	got := hclString("a${b}%{c}\"d\\e\nf")
	want := `"a$${b}%%{c}\"d\\e\nf"`
	if got != want {
		t.Fatalf("hclString = %s, want %s", got, want)
	}
}
