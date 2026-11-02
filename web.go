package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

//go:embed ui/index.html
var indexHTML []byte

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("cabledrop " + appVersion)) })
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.state()) })
	mux.HandleFunc("GET /api/events", a.handleEvents)
	mux.HandleFunc("POST /api/send", a.handleSend)
	mux.HandleFunc("POST /api/upload", a.handleUpload)
	mux.HandleFunc("POST /api/answer", a.handleAnswer)
	mux.HandleFunc("POST /api/cancel", a.handleCancel)
	mux.HandleFunc("POST /api/clear", func(w http.ResponseWriter, r *http.Request) { a.xfers.ClearFinished(); writeJSON(w, 200, ok()) })
	mux.HandleFunc("POST /api/settings", a.handleSettings)
	mux.HandleFunc("POST /api/open", a.handleOpen)
	mux.HandleFunc("POST /api/pick-dir", a.handlePickDir)
	mux.HandleFunc("POST /api/peer", a.handleAddPeer)
	return a.guard(mux)
}

// guard : l'interface n'est utilisable que depuis ce PC, et les sites web
// ouverts dans le navigateur ne peuvent pas piloter CableDrop.
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-CableDrop") != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func ok() map[string]bool { return map[string]bool{"ok": true} }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "requête invalide")
		return false
	}
	return true
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

type MeView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OS          string `json:"os"`
	DownloadDir string `json:"download_dir"`
	RouteMode   string `json:"route_mode"`
	Port        int    `json:"port"`
}

type StateView struct {
	Version      string         `json:"version"`
	Me           MeView         `json:"me"`
	Ifaces       []IfaceView    `json:"ifaces"`
	Peers        []PeerView     `json:"peers"`
	Transfers    []TransferView `json:"transfers"`
	DiscoveryOK  bool           `json:"discovery_ok"`
	DiscoveryErr string         `json:"discovery_err,omitempty"`
}

func (a *App) state() StateView {
	c := a.config()
	return StateView{
		Version:      appVersion,
		Me:           MeView{ID: c.ID, Name: c.Name, OS: runtime.GOOS, DownloadDir: c.DownloadDir, RouteMode: c.RouteMode, Port: a.transferPort},
		Ifaces:       a.disc.Ifaces(),
		Peers:        a.disc.Peers(),
		Transfers:    a.xfers.Views(),
		DiscoveryOK:  a.disc.ok,
		DiscoveryErr: a.disc.errMsg,
	}
}

// handleEvents pousse l'état à l'interface deux fois par seconde (SSE).
func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, okf := w.(http.Flusher)
	if !okf {
		http.Error(w, "streaming non supporté", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	a.uiClients.Add(1)
	defer a.uiClients.Add(-1)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		b, _ := json.Marshal(a.state())
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return
		}
		fl.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

func (a *App) handleSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerID string      `json:"peer_id"`
		Mode   string      `json:"mode"`
		Files  []FileEntry `json:"files"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Mode != modeCable && req.Mode != modeWifi {
		req.Mode = a.config().RouteMode
	}
	peer, route, found := a.disc.BestFor(req.PeerID, req.Mode)
	if !found {
		switch {
		case peer.ID == "":
			writeErr(w, http.StatusNotFound, "Ce PC n'est plus visible. Vérifiez que CableDrop est ouvert dessus.")
		case req.Mode == modeWifi:
			writeErr(w, http.StatusNotFound, peer.Name+" n'est pas visible en Wi-Fi. Les deux PC doivent être connectés au même réseau.")
		default:
			writeErr(w, http.StatusNotFound, peer.Name+" n'est pas relié par câble. Vérifiez le câble, ou passez en mode Wi-Fi.")
		}
		return
	}
	t, err := a.xfers.StartSend(peer, route, req.Files)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"id": t.ID})
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	t := a.xfers.Get(r.URL.Query().Get("id"))
	i, err := strconv.Atoi(r.URL.Query().Get("i"))
	if t == nil || t.Dir != "out" || err != nil {
		writeErr(w, http.StatusNotFound, "transfert inconnu")
		return
	}
	if err := t.Upload(i, r.Body); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, ok())
}

func (a *App) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Accept bool   `json:"accept"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	t := a.xfers.Get(req.ID)
	if t == nil || t.Dir != "in" {
		writeErr(w, http.StatusNotFound, "demande inconnue")
		return
	}
	if err := t.Answer(req.Accept); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, ok())
}

func (a *App) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	t := a.xfers.Get(req.ID)
	if t == nil {
		writeErr(w, http.StatusNotFound, "transfert inconnu")
		return
	}
	t.Cancel(req.Reason)
	writeJSON(w, 200, ok())
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        *string `json:"name"`
		DownloadDir *string `json:"download_dir"`
		RouteMode   *string `json:"route_mode"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var name, dir, mode string
	if req.RouteMode != nil {
		mode = *req.RouteMode
		if mode != modeCable && mode != modeWifi {
			writeErr(w, http.StatusBadRequest, "mode de transfert inconnu")
			return
		}
	}
	if req.Name != nil {
		name = cleanName(*req.Name)
		if name == "" {
			writeErr(w, http.StatusBadRequest, "Le nom du PC ne peut pas être vide.")
			return
		}
	}
	if req.DownloadDir != nil {
		dir = filepath.Clean(expandHome(strings.TrimSpace(*req.DownloadDir)))
		if !filepath.IsAbs(dir) {
			writeErr(w, http.StatusBadRequest, "Indiquez un chemin complet (exemple : C:\\Users\\Moi\\Downloads).")
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeErr(w, http.StatusBadRequest, "Impossible de créer ce dossier : "+err.Error())
			return
		}
		f, err := os.CreateTemp(dir, ".cabledrop-test-*")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "Impossible d'écrire dans ce dossier : "+err.Error())
			return
		}
		f.Close()
		os.Remove(f.Name())
	}
	err := a.updateConfig(func(c *Config) {
		if name != "" {
			c.Name = name
		}
		if dir != "" {
			c.DownloadDir = dir
		}
		if mode != "" {
			c.RouteMode = mode
		}
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Impossible d'enregistrer les réglages : "+err.Error())
		return
	}
	a.disc.Kick()
	writeJSON(w, 200, ok())
}

func (a *App) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	dir := a.config().DownloadDir
	if t := a.xfers.Get(req.ID); t != nil && t.Dir == "in" {
		v := t.view()
		if v.DestDir != "" {
			dir = v.DestDir
		}
		if len(v.Roots) == 1 {
			if fi, err := os.Stat(filepath.Join(dir, v.Roots[0])); err == nil && fi.IsDir() {
				dir = filepath.Join(dir, v.Roots[0])
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := openPath(dir); err != nil {
		writeErr(w, http.StatusInternalServerError, "Impossible d'ouvrir le dossier : "+err.Error())
		return
	}
	writeJSON(w, 200, ok())
}

func (a *App) handleAddPeer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ip, err := a.disc.AddManual(req.IP)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ip": ip})
}

func (a *App) handlePickDir(w http.ResponseWriter, r *http.Request) {
	p, err := pickDirectory(a.config().DownloadDir)
	if err != nil {
		writeErr(w, http.StatusNotImplemented, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"path": p})
}

// pickDirectory ouvre la boîte de dialogue native « choisir un dossier ».
func pickDirectory(start string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$f = New-Object System.Windows.Forms.Form
$f.TopMost = $true
$f.ShowInTaskbar = $false
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = 'Dossier de réception CableDrop'
$d.ShowNewFolderButton = $true
if ($env:CD_START -and (Test-Path -LiteralPath $env:CD_START)) { $d.SelectedPath = $env:CD_START }
if ($d.ShowDialog($f) -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $d.SelectedPath }
$f.Dispose()`
		cmd = exec.Command("powershell", "-NoProfile", "-STA", "-EncodedCommand", psEncode(script))
		cmd.Env = append(os.Environ(), "CD_START="+start)
	case "darwin":
		cmd = exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "Dossier de réception CableDrop")`)
	default:
		if p, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.Command(p, "--file-selection", "--directory", "--title=Dossier de réception CableDrop", "--filename="+start+"/")
		} else if p, err := exec.LookPath("kdialog"); err == nil {
			cmd = exec.Command(p, "--getexistingdirectory", start)
		} else {
			return "", errors.New("aucun sélecteur de dossier disponible : saisissez le chemin à la main")
		}
	}
	out, _ := cmd.Output()
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", nil // annulé
	}
	return filepath.Clean(s), nil
}

func psEncode(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return base64.StdEncoding.EncodeToString(b)
}
