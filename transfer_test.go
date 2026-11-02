package main

import (
	"crypto/sha256"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func newTestApp(t *testing.T, dl string) *App {
	a := &App{cfg: Config{ID: randomID(4), Name: "PC-" + randomID(2), DownloadDir: dl, RouteMode: modeCable}}
	a.cfgPath = filepath.Join(t.TempDir(), "config.json")
	a.disc = NewDiscovery(a, 0, nil)
	a.xfers = NewTransferManager(a)
	return a
}

func hashFile(t *testing.T, p string) [32]byte {
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Envoi d'un très gros fichier lu directement sur le disque : la mémoire doit
// rester faible et le fichier reçu identique.
func TestLargeTransferFromDisk(t *testing.T) {
	size := int64(4 << 30) // 4 Go
	if testing.Short() {
		size = 64 << 20
	}
	src := t.TempDir()
	big := filepath.Join(src, "machine virtuelle.ova")
	f, _ := os.Create(big)
	f.Truncate(size) // fichier creux : rapide à créer
	f.WriteAt([]byte("début"), 0)
	f.WriteAt([]byte("fin"), size-3)
	f.Close()
	os.MkdirAll(filepath.Join(src, "Photos", "2024"), 0o755)
	os.WriteFile(filepath.Join(src, "Photos", "2024", "a.jpg"), []byte("photo a"), 0o644)
	os.WriteFile(filepath.Join(src, "Photos", "vide.txt"), nil, 0o644)

	dl := t.TempDir()
	recv, send := newTestApp(t, dl), newTestApp(t, t.TempDir())
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go recv.xfers.Serve(ln)

	files, err := collectFiles([]string{big, filepath.Join(src, "Photos")})
	if err != nil || len(files) != 3 {
		t.Fatalf("collectFiles : %v, %d fichiers", err, len(files))
	}
	route := Route{IP: net.IPv4(127, 0, 0, 1), Port: ln.Addr().(*net.TCPAddr).Port, Kind: "cable"}
	tr, err := send.xfers.StartSend(Peer{ID: "x", Name: "Récepteur"}, route, files)
	if err != nil {
		t.Fatal(err)
	}

	var maxHeap uint64
	deadline := time.Now().Add(5 * time.Minute)
	for !isTerminal(tr.State()) && time.Now().Before(deadline) {
		for _, v := range recv.xfers.Views() {
			if v.State == stPending {
				recv.xfers.Get(v.ID).Answer(true)
			}
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		if ms.HeapAlloc > maxHeap {
			maxHeap = ms.HeapAlloc
		}
		time.Sleep(50 * time.Millisecond)
	}
	v := tr.view()
	if v.State != stDone {
		t.Fatalf("état %s : %s", v.State, v.Error)
	}
	t.Logf("%s en %.1f s (%.0f Mo/s), mémoire max %d Mo", fmtSize(size), v.Elapsed, v.Speed/1e6, maxHeap>>20)
	if maxHeap > 100<<20 {
		t.Errorf("mémoire utilisée trop élevée : %d Mo", maxHeap>>20)
	}
	if hashFile(t, big) != hashFile(t, filepath.Join(dl, "machine virtuelle.ova")) {
		t.Error("le gros fichier reçu est différent")
	}
	if b, _ := os.ReadFile(filepath.Join(dl, "Photos", "2024", "a.jpg")); string(b) != "photo a" {
		t.Error("photo manquante dans le dossier reçu")
	}
	if _, err := os.Stat(filepath.Join(dl, "Photos", "vide.txt")); err != nil {
		t.Error("fichier vide manquant")
	}
}
