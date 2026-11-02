package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSanitizePath(t *testing.T) {
	cases := map[string]string{
		"a/b/c.txt":              "a/b/c.txt",
		"../../etc/passwd":       "_/_/etc/passwd",
		"/abs/file":              "abs/file",
		`C:\Windows\x.dll`:       "C_/Windows/x.dll",
		"notes:CON?.txt":         "notes_CON_.txt",
		"CON.txt":                "_CON.txt",
		"dossier./fin ":          "dossier/fin",
		"vidéo été/à la mer.mp4": "vidéo été/à la mer.mp4",
	}
	for in, want := range cases {
		got, err := sanitizePath(in)
		if err != nil || got != want {
			t.Errorf("sanitizePath(%q) = %q, %v ; attendu %q", in, got, err, want)
		}
	}
	if _, err := sanitizePath("/./"); err == nil {
		t.Error("un chemin vide doit être refusé")
	}
}

func TestPlanPathsNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, "Photos"), 0o755)
	files := []FileEntry{{Path: "a.txt"}, {Path: "Photos/1.jpg"}, {Path: "Photos/2.jpg"}, {Path: "b"}, {Path: "b"}}
	rels, roots := planPaths(dir, files, nil)
	want := []string{"a (1).txt", "Photos (1)/1.jpg", "Photos (1)/2.jpg", "b", "b (1)"}
	if !reflect.DeepEqual(rels, want) {
		t.Errorf("planPaths = %v ; attendu %v", rels, want)
	}
	if !reflect.DeepEqual(roots, []string{"a (1).txt", "Photos (1)", "b"}) {
		t.Errorf("roots = %v", roots)
	}
}
