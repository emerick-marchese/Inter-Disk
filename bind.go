package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Méthodes appelées par l'interface (window.go.main.App.<Méthode> en JavaScript).
// Une erreur renvoyée ici devient une promesse rejetée côté interface.

var errNoWindow = errors.New("fenêtre indisponible")

type MeView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OS          string `json:"os"`
	DownloadDir string `json:"download_dir"`
	RouteMode   string `json:"route_mode"`
	MaxSendMBps int    `json:"max_send_mbps"`
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

// State renvoie tout ce que l'interface affiche.
func (a *App) State() StateView {
	c := a.config()
	return StateView{
		Version:      appVersion,
		Me:           MeView{ID: c.ID, Name: c.Name, OS: runtime.GOOS, DownloadDir: c.DownloadDir, RouteMode: c.RouteMode, MaxSendMBps: c.MaxSendMBps, Port: a.transferPort},
		Ifaces:       a.disc.Ifaces(),
		Peers:        a.disc.Peers(),
		Transfers:    a.xfers.Views(),
		DiscoveryOK:  a.disc.ok,
		DiscoveryErr: a.disc.errMsg,
	}
}

// PickFiles ouvre la fenêtre Windows « choisir des fichiers ».
func (a *App) PickFiles() ([]Item, error) {
	if a.ctx == nil {
		return nil, errNoWindow
	}
	paths, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Choisir les fichiers à envoyer"})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	return describePaths(paths)
}

// PickFolder ouvre la fenêtre Windows « choisir un dossier ».
func (a *App) PickFolder() ([]Item, error) {
	if a.ctx == nil {
		return nil, errNoWindow
	}
	p, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Choisir le dossier à envoyer"})
	if err != nil || p == "" {
		return nil, err
	}
	return describePaths([]string{p})
}

// Describe résume des fichiers/dossiers glissés dans la fenêtre.
func (a *App) Describe(paths []string) ([]Item, error) {
	return describePaths(paths)
}

// Send envoie les fichiers/dossiers choisis au PC peerID, par câble ou Wi-Fi.
func (a *App) Send(peerID, mode string, paths []string) (string, error) {
	if mode != modeCable && mode != modeWifi {
		mode = a.config().RouteMode
	}
	peer, route, found := a.disc.BestFor(peerID, mode)
	if !found {
		switch {
		case peer.ID == "":
			return "", errors.New("ce PC n'est plus visible. Vérifiez qu'Inter Disk est ouvert dessus")
		case mode == modeWifi:
			return "", errors.New(peer.Name + " n'est pas visible en Wi-Fi. Les deux PC doivent être connectés au même réseau")
		default:
			return "", errors.New(peer.Name + " n'est pas relié par câble. Vérifiez le câble, ou passez en mode Wi-Fi")
		}
	}
	files, err := collectFiles(paths)
	if err != nil {
		return "", err
	}
	t, err := a.xfers.StartSend(peer, route, files)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

// Answer accepte ou refuse une demande de réception.
func (a *App) Answer(id string, accept bool) error {
	t := a.xfers.Get(id)
	if t == nil || t.Dir != "in" {
		return errors.New("demande inconnue")
	}
	return t.Answer(accept)
}

// Cancel annule un transfert (ou refuse une demande en attente).
func (a *App) Cancel(id string) error {
	t := a.xfers.Get(id)
	if t == nil {
		return errors.New("transfert inconnu")
	}
	t.Cancel("")
	return nil
}

// ClearFinished efface l'historique des transferts terminés.
func (a *App) ClearFinished() { a.xfers.ClearFinished() }

// SetMode choisit le mode de transfert : « cable » ou « wifi ».
func (a *App) SetMode(mode string) error {
	if mode != modeCable && mode != modeWifi {
		return errors.New("mode de transfert inconnu")
	}
	return a.updateConfig(func(c *Config) { c.RouteMode = mode })
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

// SaveSettings enregistre le nom du PC, le dossier de réception et la vitesse
// d'envoi maximale (Mo/s, 0 = illimitée).
func (a *App) SaveSettings(name, dir string, maxMBps int) error {
	if maxMBps < 0 || maxMBps > 10000 {
		return errors.New("vitesse maximale invalide")
	}
	name = cleanName(name)
	if name == "" {
		return errors.New("le nom du PC ne peut pas être vide")
	}
	dir = filepath.Clean(expandHome(strings.TrimSpace(dir)))
	if !filepath.IsAbs(dir) {
		return errors.New(`indiquez un chemin complet (exemple : C:\Users\Moi\Downloads)`)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.New("impossible de créer ce dossier : " + err.Error())
	}
	f, err := os.CreateTemp(dir, ".inter-disk-test-*")
	if err != nil {
		return errors.New("impossible d'écrire dans ce dossier : " + err.Error())
	}
	f.Close()
	os.Remove(f.Name())
	if err := a.updateConfig(func(c *Config) { c.Name, c.DownloadDir, c.MaxSendMBps = name, dir, maxMBps }); err != nil {
		return errors.New("impossible d'enregistrer les réglages : " + err.Error())
	}
	a.disc.Kick()
	return nil
}

// PickDownloadDir ouvre la fenêtre Windows « choisir un dossier » pour la réception.
func (a *App) PickDownloadDir() (string, error) {
	if a.ctx == nil {
		return "", errNoWindow
	}
	start := existingParent(a.config().DownloadDir)
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Dossier de réception d'" + appName, DefaultDirectory: start, CanCreateDirectories: true,
	})
}

// OpenFolder ouvre l'explorateur sur les fichiers reçus (id vide : dossier de réception).
func (a *App) OpenFolder(id string) error {
	dir := a.config().DownloadDir
	if t := a.xfers.Get(id); t != nil && t.Dir == "in" {
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
		return err
	}
	if err := openPath(dir); err != nil {
		return errors.New("impossible d'ouvrir le dossier : " + err.Error())
	}
	return nil
}

// OpenLog ouvre le journal de l'application (utile en cas de problème).
func (a *App) OpenLog() error {
	p := filepath.Join(filepath.Dir(a.cfgPath), "inter-disk.log")
	if _, err := os.Stat(p); err != nil {
		return errors.New("journal introuvable")
	}
	return openPath(p)
}

// AddPeer cherche Inter Disk à une adresse IP précise.
func (a *App) AddPeer(ip string) (string, error) {
	return a.disc.AddManual(ip)
}
