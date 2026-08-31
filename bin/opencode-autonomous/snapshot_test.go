package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newCmd(dir string, args ...string) *exec.Cmd {
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	return c
}

func TestSnapshotCreateRestore(t *testing.T) {
	// Cria repo git temp
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := newCmd(dir, args...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	run("git", "init", "-q")
	run("git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "init")

	// Cria arquivo e commita
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "x.txt")
	run("git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "v1")

	// Snapshot
	meta, err := CreateSnapshot(dir, "test")
	if err != nil {
		t.Fatalf("snapshot create: %v", err)
	}
	if meta.ID == "" || meta.Tag == "" {
		t.Fatalf("meta incompleto: %+v", meta)
	}

	// Modifica o arquivo
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("v2-MODIFICADO"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Confirma que md5 mudou
	before := readFile(t, filepath.Join(dir, "x.txt"))
	if before != "v2-MODIFICADO" {
		t.Fatalf("esperava v2-MODIFICADO, got %q", before)
	}

	// Restaura
	if err := RestoreSnapshot(meta.ID, dir, false); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Verifica que voltou para v1
	after := readFile(t, filepath.Join(dir, "x.txt"))
	if after != "v1" {
		t.Fatalf("restore falhou: esperava v1, got %q", after)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}