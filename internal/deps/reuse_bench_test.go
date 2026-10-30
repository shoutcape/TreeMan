package deps

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const reuseBenchmarkPath = "node_modules"

// BenchmarkReuseSetup compares an offline install with copying an existing
// tree and running that same installer. The fixture is deliberately synthetic;
// it does not model npm, pnpm, Yarn, Go, or Cargo internals.
func BenchmarkReuseSetup(b *testing.B) {
	mainRoot := b.TempDir()
	cacheRoot := b.TempDir()
	if err := makeReuseBenchmarkFixture(filepath.Join(mainRoot, reuseBenchmarkPath)); err != nil {
		b.Fatal(err)
	}
	if err := makeReuseBenchmarkFixture(filepath.Join(cacheRoot, reuseBenchmarkPath)); err != nil {
		b.Fatal(err)
	}

	b.Run("cold-install", func(b *testing.B) {
		b.StopTimer()
		b.ReportMetric(256, "files")
		for range b.N {
			target := benchmarkDestination(b)
			b.StartTimer()
			if err := offlineInstall(filepath.Join(cacheRoot, reuseBenchmarkPath), filepath.Join(target, reuseBenchmarkPath)); err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
			removeBenchmarkDestination(b, target)
		}
	})

	b.Run("reuse-plus-install", func(b *testing.B) {
		b.StopTimer()
		b.ReportMetric(256, "files")
		for range b.N {
			target := benchmarkDestination(b)
			b.StartTimer()
			status, err := Reuse(mainRoot, target, reuseBenchmarkPath)
			if err != nil {
				b.Fatal(err)
			}
			if status != ReuseCopied {
				b.Fatalf("Reuse status = %v, want ReuseCopied", status)
			}
			if err := offlineInstall(filepath.Join(cacheRoot, reuseBenchmarkPath), filepath.Join(target, reuseBenchmarkPath)); err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
			removeBenchmarkDestination(b, target)
		}
	})
}

func benchmarkDestination(b *testing.B) string {
	b.Helper()
	target, err := os.MkdirTemp("", "treeman-reuse-bench-")
	if err != nil {
		b.Fatal(err)
	}
	return target
}

func removeBenchmarkDestination(b *testing.B, target string) {
	b.Helper()
	if err := os.RemoveAll(target); err != nil {
		b.Fatal(err)
	}
}

func makeReuseBenchmarkFixture(root string) error {
	for directory := 0; directory < 16; directory++ {
		dir := filepath.Join(root, fmt.Sprintf("package-%02d", directory))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for file := 0; file < 16; file++ {
			contents := make([]byte, 4096)
			for index := range contents {
				contents[index] = byte((directory + file + index) % 251)
			}
			path := filepath.Join(dir, fmt.Sprintf("file-%02d.dat", file))
			if err := os.WriteFile(path, contents, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// offlineInstall is a small fixture installer. It skips files already in the
// destination, like an installer reusing an intact local artifact, but reads
// the complete cache tree on every run.
func offlineInstall(source, destination string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, err := os.Stat(target); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
}
