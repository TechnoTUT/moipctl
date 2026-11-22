package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTripAndMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	f, err := Load(path)
	if err != nil || len(f.Aliases) != 0 {
		t.Fatalf("missing file should load empty, got %+v err=%v", f, err)
	}
	if err := Save(path, File{Aliases: map[string]string{"a": "entrance"}}); err != nil {
		t.Fatal(err)
	}
	f, err = Load(path)
	if err != nil || f.Aliases["a"] != "entrance" {
		t.Fatalf("got %+v err=%v", f, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("temp files must not be left behind: %v", entries)
	}
}

func TestLoadRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte("{not json"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("a corrupt file must be an error, not silently ignored")
	}
}
