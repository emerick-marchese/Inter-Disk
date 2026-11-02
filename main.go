// CableDrop — transfert de fichiers entre deux PC reliés par un câble Ethernet.
//
// Un seul exécutable : il détecte l'autre PC sur le câble, et l'interface
// s'affiche dans le navigateur (http://127.0.0.1:47822).
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
)

const (
	appName    = "CableDrop"
	appVersion = "1.0.0"
)

type Config struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DownloadDir string `json:"download_dir"`
}

type App struct {
	cfgMu   sync.Mutex
	cfg     Config
	cfgPath string

	disc  *Discovery
	xfers *TransferManager

	transferPort int
	uiURL        string
	noBrowser    bool

	uiClients   atomic.Int32
	browserMu   sync.Mutex
	lastBrowser time.Time
}

func (a *App) config() Config {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.cfg
}

func (a *App) updateConfig(fn func(*Config)) error {
	a.cfgMu.Lock()
	fn(&a.cfg)
	c := a.cfg
	a.cfgMu.Unlock()
	return saveConfig(a.cfgPath, c)
}

func main() {
	discPort := flag.Int("discovery-port", 47820, "port UDP de détection des PC")
	tcpPort := flag.Int("port", 47821, "port TCP de transfert")
	uiPort := flag.Int("ui-port", 47822, "port de l'interface web (accessible uniquement depuis ce PC)")
	cfgDir := flag.String("config-dir", "", "dossier de configuration (par défaut : dossier de config utilisateur)")
	noBrowser := flag.Bool("no-browser", false, "ne pas ouvrir le navigateur automatiquement")
	announce := flag.String("announce-ports", "", "(tests) ports UDP supplémentaires vers lesquels s'annoncer, séparés par des virgules")
	flag.Parse()

	go logWorker()

	app := &App{noBrowser: *noBrowser}
	app.cfgPath = configPath(*cfgDir)
	app.cfg = loadConfig(app.cfgPath)

	// Interface web, uniquement sur 127.0.0.1. Si le port est déjà pris par
	// CableDrop, c'est qu'il tourne déjà : on rouvre simplement la page.
	uiAddr := fmt.Sprintf("127.0.0.1:%d", *uiPort)
	uiLn, err := net.Listen("tcp", uiAddr)
	if err != nil {
		if pingExisting(uiAddr) {
			fmt.Println("CableDrop est déjà lancé : ouverture de l'interface…")
			if !*noBrowser {
				openURL("http://" + uiAddr + "/")
				time.Sleep(time.Second)
			}
			return
		}
		uiLn, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fatal("impossible de démarrer l'interface : %v", err)
		}
	}
	app.uiURL = "http://" + uiLn.Addr().String() + "/"

	// Port de transfert (accessible depuis l'autre PC).
	tln, err := net.Listen("tcp4", fmt.Sprintf(":%d", *tcpPort))
	if err != nil {
		logf("Port TCP %d occupé, utilisation d'un autre port.", *tcpPort)
		tln, err = net.Listen("tcp4", ":0")
		if err != nil {
			fatal("impossible d'ouvrir le port de transfert : %v", err)
		}
	}
	app.transferPort = tln.Addr().(*net.TCPAddr).Port

	var extra []int
	for _, s := range strings.Split(*announce, ",") {
		if p, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			extra = append(extra, p)
		}
	}
	app.disc = NewDiscovery(app, *discPort, extra)
	app.xfers = NewTransferManager(app)

	srv := &http.Server{Handler: app.routes(), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(uiLn)
	go app.xfers.Serve(tln)
	app.disc.Start()

	cfg := app.config()
	fmt.Println()
	fmt.Println("  ┌──────────────────────────────────────────────────────────┐")
	fmt.Printf("  │  CableDrop %-46s│\n", appVersion+" — transfert par câble Ethernet")
	fmt.Println("  └──────────────────────────────────────────────────────────┘")
	fmt.Printf("  Ce PC        : %s\n", cfg.Name)
	fmt.Printf("  Interface    : %s\n", app.uiURL)
	fmt.Printf("  Fichiers reçus dans : %s\n", cfg.DownloadDir)
	for _, ifc := range listLocalIfaces() {
		if ifc.Kind == "virtual" {
			continue
		}
		fmt.Printf("  Carte réseau : %-24s %-15s (%s)\n", ifc.Name, ifc.IP, kindLabel(ifc.Kind, ifc.LinkLocal))
	}
	fmt.Println()
	fmt.Println("  Laissez cette fenêtre ouverte. Fermez-la pour quitter CableDrop.")
	fmt.Println()

	if !*noBrowser {
		app.openUI()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	app.disc.SendBye()
	time.Sleep(150 * time.Millisecond)
}

// openUI ouvre l'interface dans le navigateur (au plus une fois toutes les 8 s).
func (a *App) openUI() {
	if a.noBrowser {
		return
	}
	a.browserMu.Lock()
	if time.Since(a.lastBrowser) < 8*time.Second {
		a.browserMu.Unlock()
		return
	}
	a.lastBrowser = time.Now()
	a.browserMu.Unlock()
	openURL(a.uiURL)
}

// ---------------------------------------------------------------- config

func configPath(dir string) string {
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = "."
		}
		dir = filepath.Join(base, appName)
	}
	return filepath.Join(dir, "config.json")
}

func loadConfig(path string) Config {
	var c Config
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	changed := false
	if c.ID == "" {
		c.ID = randomID(8)
		changed = true
	}
	if cleanName(c.Name) == "" {
		c.Name = defaultName()
		changed = true
	}
	if c.DownloadDir == "" {
		c.DownloadDir = defaultDownloadDir()
		changed = true
	}
	if changed {
		if err := saveConfig(path, c); err != nil {
			logf("Impossible d'enregistrer la configuration : %v", err)
		}
	}
	return c
}

func saveConfig(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func defaultName() string {
	h, err := os.Hostname()
	if err != nil || cleanName(h) == "" {
		return "PC-" + randomID(2)
	}
	return cleanName(strings.Split(h, ".")[0])
}

func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dl := filepath.Join(home, "Downloads")
	if runtime.GOOS == "linux" {
		// Respecte le dossier « Téléchargements » des bureaux Linux en français.
		if b, err := os.ReadFile(filepath.Join(home, ".config", "user-dirs.dirs")); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "XDG_DOWNLOAD_DIR=") {
					v := strings.Trim(strings.TrimPrefix(line, "XDG_DOWNLOAD_DIR="), `"`)
					v = strings.ReplaceAll(v, "$HOME", home)
					if v != "" && filepath.Clean(v) != filepath.Clean(home) {
						dl = v
					}
				}
			}
		}
	}
	return filepath.Join(dl, appName)
}

// ---------------------------------------------------------------- helpers

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// cleanName nettoie un nom de PC affiché (pas de caractères de contrôle, 40 car. max).
func cleanName(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
		n++
		if n >= 40 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

var logCh = make(chan string, 512)

// logf écrit dans la console sans jamais bloquer l'appelant (sous Windows,
// une sélection dans la console met les écritures en pause).
func logf(format string, args ...any) {
	msg := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	select {
	case logCh <- msg:
	default:
	}
}

func logWorker() {
	for m := range logCh {
		fmt.Println(m)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\nERREUR : "+format+"\n", args...)
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "\nAppuyez sur Entrée pour fermer…")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	os.Exit(1)
}

func pingExisting(addr string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	r, err := c.Get("http://" + addr + "/api/ping")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 64))
	return strings.HasPrefix(string(b), "cabledrop")
}

func startDetached(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func openURL(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := startDetached(cmd); err != nil {
		logf("Ouvrez %s dans votre navigateur.", u)
	}
}

func openPath(p string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", p)
	case "darwin":
		cmd = exec.Command("open", p)
	default:
		cmd = exec.Command("xdg-open", p)
	}
	return startDetached(cmd)
}

func fmtSize(n int64) string {
	f := float64(n)
	units := []string{"o", "Ko", "Mo", "Go", "To"}
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d o", n)
	}
	return strings.Replace(fmt.Sprintf("%.1f %s", f, units[i]), ".", ",", 1)
}
