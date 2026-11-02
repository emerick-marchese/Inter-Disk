// Inter Disk — transfert de fichiers entre deux PC, par câble Ethernet ou Wi-Fi.
//
// Application de bureau : la fenêtre (Wails + WebView2 sous Windows) affiche
// l'interface de ui/index.html ; tout le travail réseau et disque est fait ici,
// en Go, sans passer par un navigateur.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

const (
	appName    = "Inter Disk"
	appVersion = "2.2.0"
	// Ancien nom de l'application (versions 1.x), pour reprendre ses réglages.
	legacyName = "CableDrop"
)

type Config struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DownloadDir string `json:"download_dir"`
	RouteMode   string `json:"route_mode"`    // cable (par défaut) | wifi
	MaxSendMBps int    `json:"max_send_mbps"` // vitesse d'envoi maximale en Mo/s (0 = illimitée)
}

type App struct {
	ctx context.Context // contexte de la fenêtre (nil en mode test)

	cfgMu   sync.Mutex
	cfg     Config
	cfgPath string

	disc  *Discovery
	xfers *TransferManager

	transferPort int
	forceQuit    atomic.Bool
}

// testHTTPServe est fourni par testhttp.go (compilé uniquement avec -tags testhttp).
var testHTTPServe func(a *App, port int) error

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
	cfgDir := flag.String("config-dir", "", "dossier de configuration (par défaut : dossier de config utilisateur)")
	announce := flag.String("announce-ports", "", "(tests) ports UDP supplémentaires vers lesquels s'annoncer, séparés par des virgules")
	testHTTP := flag.Int("test-http", 0, "(tests) pilote l'application par HTTP au lieu d'ouvrir une fenêtre")
	flag.Parse()

	app := &App{}
	app.cfgPath = configPath(*cfgDir)
	setupLog(filepath.Dir(app.cfgPath))
	go logWorker()
	app.cfg = loadConfig(app.cfgPath, *cfgDir == "")

	tln, err := net.Listen("tcp4", fmt.Sprintf(":%d", *tcpPort))
	if err != nil {
		logf("Port TCP %d occupé, utilisation d'un autre port.", *tcpPort)
		tln, err = net.Listen("tcp4", ":0")
		if err != nil {
			fatal("Impossible d'ouvrir le port de transfert : %v", err)
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
	go app.xfers.Serve(tln)
	app.disc.Start()

	cfg := app.config()
	logf("%s %s démarré — PC « %s », réception dans %s, port %d", appName, appVersion, cfg.Name, cfg.DownloadDir, app.transferPort)
	for _, ifc := range listLocalIfaces() {
		if ifc.Kind != "virtual" {
			logf("Carte réseau : %s %s (%s)", ifc.Name, ifc.IP, kindLabel(ifc.Kind, ifc.LinkLocal))
		}
	}

	if *testHTTP > 0 {
		if testHTTPServe == nil {
			fatal("Option -test-http disponible uniquement dans les versions de test.")
		}
		if err := testHTTPServe(app, *testHTTP); err != nil {
			fatal("%v", err)
		}
		return
	}

	runDesktop(app, *cfgDir)
	app.disc.SendBye()
	time.Sleep(150 * time.Millisecond)
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

func loadConfig(path string, migrate bool) Config {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil && migrate {
		// Première ouverture d'Inter Disk : on reprend les réglages de CableDrop.
		if base, e := os.UserConfigDir(); e == nil {
			if old, e := os.ReadFile(filepath.Join(base, legacyName, "config.json")); e == nil {
				b, err = old, nil
				logf("Réglages repris de %s.", legacyName)
			}
		}
	}
	if err == nil {
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
	if c.DownloadDir == "" || filepath.Clean(c.DownloadDir) == filepath.Clean(downloadsDir(legacyName)) {
		c.DownloadDir = downloadsDir(appName)
		changed = true
	}
	if c.RouteMode != modeCable && c.RouteMode != modeWifi {
		c.RouteMode = modeCable
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

// downloadsDir renvoie Téléchargements/<sub>.
func downloadsDir(sub string) string {
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
	return filepath.Join(dl, sub)
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

var (
	logCh   = make(chan string, 512)
	logFile *os.File
)

// setupLog ouvre le journal « inter-disk.log » dans le dossier de configuration
// (utile pour comprendre un problème : l'application n'a pas de console).
func setupLog(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	p := filepath.Join(dir, "inter-disk.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(p); err == nil && fi.Size() > 2<<20 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	if f, err := os.OpenFile(p, flags, 0o644); err == nil {
		logFile = f
	}
}

// logf écrit dans le journal sans jamais bloquer l'appelant.
func logf(format string, args ...any) {
	msg := time.Now().Format("2006-01-02 15:04:05") + "  " + fmt.Sprintf(format, args...)
	select {
	case logCh <- msg:
	default:
	}
}

func logWorker() {
	var w *bufio.Writer
	if logFile != nil {
		w = bufio.NewWriter(logFile)
	}
	for m := range logCh {
		fmt.Println(m)
		if w != nil {
			w.WriteString(m + "\n")
			if len(logCh) == 0 {
				w.Flush()
			}
		}
	}
}

func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if logFile != nil {
		fmt.Fprintf(logFile, "%s  ERREUR : %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
	}
	showErrorBox(appName, msg)
	os.Exit(1)
}

func startDetached(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
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
