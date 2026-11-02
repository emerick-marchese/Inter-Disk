package main

import (
	"crypto/sha256"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func autoAccept(recv *App, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-time.After(20 * time.Millisecond):
		}
		for _, v := range recv.xfers.Views() {
			if v.State == stPending {
				recv.xfers.Get(v.ID).Answer(true)
			}
		}
	}
}

func waitEnd(t *testing.T, tr *Transfer, d time.Duration) TransferView {
	deadline := time.Now().Add(d)
	for !isTerminal(tr.State()) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	return tr.view()
}

// Transfert coupé en plein milieu (l'autre PC plante), puis relancé :
// il doit reprendre là où il s'est arrêté et donner un fichier identique.
func TestResumeAfterInterruption(t *testing.T) {
	old := checkpointEvery
	checkpointEvery = 16 << 20
	defer func() { checkpointEvery = old }()

	src := t.TempDir()
	big := filepath.Join(src, "vm.ova")
	data := make([]byte, 200<<20)
	for i := range data {
		data[i] = byte(i*7 + i>>13)
	}
	os.WriteFile(big, data, 0o644)
	os.WriteFile(filepath.Join(src, "petit.txt"), []byte("bonjour"), 0o644)

	dl := t.TempDir()
	recv, send := newTestApp(t, dl), newTestApp(t, t.TempDir())
	send.cfg.MaxSendMBps = 100 // assez lent pour couper au milieu
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer ln.Close()
	go recv.xfers.Serve(ln)
	stop := make(chan struct{})
	defer close(stop)
	go autoAccept(recv, stop)
	route := Route{IP: net.IPv4(127, 0, 0, 1), Port: ln.Addr().(*net.TCPAddr).Port, Kind: "cable"}
	paths := []string{filepath.Join(src, "petit.txt"), big}

	// 1er envoi : on coupe la connexion vers 60 %.
	files, _ := collectFiles(paths)
	tr, _ := send.xfers.StartSend(Peer{ID: "x", Name: "R"}, route, files)
	for tr.done.Load() < 120<<20 && !isTerminal(tr.State()) {
		time.Sleep(10 * time.Millisecond)
	}
	tr.Cancel("")
	waitEnd(t, tr, 10*time.Second)
	time.Sleep(300 * time.Millisecond)
	var recvView TransferView
	for _, v := range recv.xfers.Views() {
		recvView = v
	}
	if recvView.State != stError || !strings.Contains(recvView.Error, "renvoyez") {
		t.Fatalf("réception interrompue attendue, obtenu %s : %s", recvView.State, recvView.Error)
	}
	if _, err := os.Stat(filepath.Join(dl, "vm.ova"+partExt)); err != nil {
		t.Fatal("le fichier partiel doit être conservé")
	}

	// 2e envoi des mêmes fichiers : reprise.
	send.cfg.MaxSendMBps = 0
	files, _ = collectFiles(paths)
	tr2, _ := send.xfers.StartSend(Peer{ID: "x", Name: "R"}, route, files)
	v := waitEnd(t, tr2, time.Minute)
	if v.State != stDone {
		t.Fatalf("reprise : %s %s", v.State, v.Error)
	}
	t.Logf("reprise à partir de %s sur %s", fmtSize(v.Resumed), fmtSize(v.Total))
	if v.Resumed < 64<<20 {
		t.Errorf("la reprise devrait partir d'au moins 64 Mo, obtenu %s", fmtSize(v.Resumed))
	}
	got, _ := os.ReadFile(filepath.Join(dl, "vm.ova"))
	if sha256.Sum256(got) != sha256.Sum256(data) {
		t.Fatal("le fichier repris est différent de l'original")
	}
	if _, err := os.Stat(filepath.Join(dl, "vm.ova"+partExt)); err == nil {
		t.Error("le fichier partiel doit disparaître après la reprise")
	}
	if entries, _ := os.ReadDir(dl); len(entries) != 2 {
		t.Errorf("le dossier de réception devrait contenir 2 fichiers, il en a %d", len(entries))
	}
	if recv.xfers.resume.get(offerSignature(send.cfg.ID, files)) != nil {
		t.Error("la reprise terminée doit être oubliée")
	}
}

// Limite de vitesse d'envoi.
func TestSpeedLimit(t *testing.T) {
	src := t.TempDir()
	p := filepath.Join(src, "f.bin")
	f, _ := os.Create(p)
	f.Truncate(60 << 20)
	f.Close()
	recv, send := newTestApp(t, t.TempDir()), newTestApp(t, t.TempDir())
	send.cfg.MaxSendMBps = 30
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer ln.Close()
	go recv.xfers.Serve(ln)
	stop := make(chan struct{})
	defer close(stop)
	go autoAccept(recv, stop)
	files, _ := collectFiles([]string{p})
	tr, _ := send.xfers.StartSend(Peer{ID: "x", Name: "R"}, Route{IP: net.IPv4(127, 0, 0, 1), Port: ln.Addr().(*net.TCPAddr).Port}, files)
	v := waitEnd(t, tr, time.Minute)
	if v.State != stDone {
		t.Fatalf("%s %s", v.State, v.Error)
	}
	t.Logf("60 Mo limités à 30 Mo/s : %.1f s (%.1f Mo/s)", v.Elapsed, v.Speed/1e6)
	if v.Speed > 33e6 || v.Speed < 20e6 {
		t.Errorf("vitesse %.1f Mo/s hors de la limite de 30 Mo/s", v.Speed/1e6)
	}
}
