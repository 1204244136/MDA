package main

import (
	"path/filepath"
	"testing"
)

func TestFrameworkLibDirUsesHostDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native libraries")
	t.Setenv("MAAFW_BINARY_PATH", path)
	if got := frameworkLibDir(); got != path {
		t.Fatalf("frameworkLibDir() = %q, want host directory %q", got, path)
	}
}

func TestFrameworkLibDirPreservesDesktopLayout(t *testing.T) {
	t.Setenv("MAAFW_BINARY_PATH", "")
	if got, want := frameworkLibDir(), filepath.Join(getCwd(), "maafw"); got != want {
		t.Fatalf("frameworkLibDir() = %q, want desktop directory %q", got, want)
	}
}
