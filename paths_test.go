package main

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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

func TestPickRoute(t *testing.T) {
	now := time.Now()
	direct := &Route{IP: net.IPv4(169, 254, 1, 2), Kind: "cable", LinkLocal: true, LastSeen: now}
	box := &Route{IP: net.IPv4(192, 168, 1, 20), Kind: "cable", LastSeen: now}
	wifi := &Route{IP: net.IPv4(192, 168, 1, 21), Kind: "wifi", LastSeen: now}
	all := map[string]*Route{"d": direct, "b": box, "w": wifi}
	if r := pickRoute(all, modeCable); r != direct {
		t.Errorf("mode câble : %v, attendu le câble direct", r.IP)
	}
	if r := pickRoute(all, modeWifi); r != wifi {
		t.Errorf("mode Wi-Fi : %v, attendu le Wi-Fi", r.IP)
	}
	if r := pickRoute(map[string]*Route{"w": wifi}, modeCable); r != nil {
		t.Errorf("mode câble sans câble : %v, attendu aucun chemin", r.IP)
	}
	if r := pickRoute(map[string]*Route{"d": direct}, modeWifi); r != nil {
		t.Errorf("mode Wi-Fi ne doit jamais utiliser le câble direct")
	}
	if r := pickRoute(map[string]*Route{"d": direct, "b": box}, modeWifi); r != box {
		t.Errorf("mode Wi-Fi sans Wi-Fi : attendu l'Ethernet de la box")
	}
}

func TestCollectAndDescribe(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Docs", "sous"), 0o755)
	os.WriteFile(filepath.Join(dir, "Docs", "a.txt"), []byte("12345"), 0o644)
	os.WriteFile(filepath.Join(dir, "Docs", "sous", "b.txt"), []byte("123"), 0o644)
	os.WriteFile(filepath.Join(dir, "seul.bin"), []byte("1"), 0o644)
	os.MkdirAll(filepath.Join(dir, "Vide"), 0o755)

	items, err := describePaths([]string{filepath.Join(dir, "Docs"), filepath.Join(dir, "seul.bin"), filepath.Join(dir, "Vide")})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Name != "Docs" || !items[0].IsDir || items[0].Count != 2 || items[0].Size != 8 {
		t.Errorf("dossier mal décrit : %+v", items[0])
	}
	if items[1].IsDir || items[1].Count != 1 || items[2].Count != 0 {
		t.Errorf("description inattendue : %+v", items)
	}
	files, err := collectFiles([]string{filepath.Join(dir, "Docs"), filepath.Join(dir, "seul.bin")})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = f.abs != ""
	}
	for _, want := range []string{"Docs/a.txt", "Docs/sous/b.txt", "seul.bin"} {
		if !got[want] {
			t.Errorf("fichier %s absent : %v", want, got)
		}
	}
	if _, err := collectFiles([]string{filepath.Join(dir, "Vide")}); err == nil {
		t.Error("un dossier vide doit être refusé")
	}
}
