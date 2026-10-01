package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(filepath.Join(t.TempDir(), "todos.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddAssignsSequentialIDs(t *testing.T) {
	s := tempStore(t)

	a := s.Add("first")
	b := s.Add("second")
	if a.ID != 1 || b.ID != 2 {
		t.Errorf("ids = %d, %d; want 1, 2", a.ID, b.ID)
	}
	if a.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := tempStore(t)
	s.Add("persisted")
	if _, err := s.MarkDone(1); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Todos) != 1 || !loaded.Todos[0].Done() {
		t.Errorf("round trip lost data: %+v", loaded.Todos)
	}
	if loaded.NextID != 2 {
		t.Errorf("NextID = %d, want 2 (must survive reload or ids repeat)", loaded.NextID)
	}
}

func TestMarkDoneAndDeleteUnknownID(t *testing.T) {
	s := tempStore(t)
	s.Add("only")

	if _, err := s.MarkDone(99); err == nil {
		t.Error("MarkDone(99) should fail")
	}
	if err := s.Delete(99); err == nil {
		t.Error("Delete(99) should fail")
	}
	if err := s.Delete(1); err != nil {
		t.Errorf("Delete(1): %v", err)
	}
	if len(s.Todos) != 0 {
		t.Errorf("todos left: %v", s.Todos)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("err = %v, want corrupt-file error", err)
	}
}
