package netd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepairAptCache(t *testing.T) {
	dir := t.TempDir()
	old := aptCacheDir
	aptCacheDir = dir
	defer func() { aptCacheDir = old }()
	for _, f := range []string{"pkgcache.bin", "srcpkgcache.bin", "other"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	if repairAptCache([]byte("E: Some index files failed to download")) {
		t.Fatal("unrelated apt output must not remove the cache")
	}
	if !repairAptCache([]byte("E: The package cache file is corrupted")) {
		t.Fatal("a corrupted cache should be repaired")
	}
	for _, f := range []string{"pkgcache.bin", "srcpkgcache.bin"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Fatalf("%s should be gone", f)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "other")); err != nil {
		t.Fatal("other files stay")
	}
	if repairAptCache([]byte("E: The package cache file is corrupted")) {
		t.Fatal("nothing left to remove, so no retry")
	}
}
