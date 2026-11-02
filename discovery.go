package main

import (
	"encoding/json"
	"errors"
	"net"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Détection automatique : chaque CableDrop s'annonce toutes les 2 s en
// broadcast UDP sur chacune de ses cartes réseau. On note par quelle carte
// (câble, Wi-Fi…) on voit chaque PC, pour toujours envoyer par le câble.

type Announce struct {
	App   string `json:"app"`
	V     int    `json:"v"`
	T     string `json:"t"` // hello | reply | bye
	ID    string `json:"id"`
	Name  string `json:"name"`
	OS    string `json:"os"`
	Port  int    `json:"port"`  // port TCP de transfert
	DPort int    `json:"dport"` // port UDP de détection de l'émetteur
}

type Route struct {
	IP        net.IP
	Port      int
	LocalIP   net.IP
	Iface     string
	Kind      string
	LinkLocal bool
	Manual    bool
	LastSeen  time.Time
}

func (r *Route) score() int { return routeScore(r.Kind, r.LinkLocal) }

type Peer struct {
	ID, Name, OS string
	Routes       map[string]*Route
}

type Discovery struct {
	app     *App
	port    int
	targets []int
	conn    *net.UDPConn
	ok      bool
	errMsg  string

	mu     sync.Mutex
	peers  map[string]*Peer
	manual map[string]bool
	ifaces []LocalIface
	kick   chan struct{}
}

const peerTimeout = 7 * time.Second

func NewDiscovery(app *App, port int, extra []int) *Discovery {
	targets := []int{port}
	for _, p := range extra {
		if p > 0 && p < 65536 && !slices.Contains(targets, p) {
			targets = append(targets, p)
		}
	}
	return &Discovery{
		app: app, port: port, targets: targets,
		peers: map[string]*Peer{}, manual: map[string]bool{},
		kick: make(chan struct{}, 1),
	}
}

func (d *Discovery) Start() {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: d.port})
	if err != nil {
		d.errMsg = err.Error()
		logf("⚠ Détection automatique impossible (port UDP %d) : %v", d.port, err)
	} else {
		d.conn, d.ok = conn, true
		go d.readLoop()
	}
	d.mu.Lock()
	d.ifaces = listLocalIfaces()
	d.mu.Unlock()
	go d.loop()
}

func (d *Discovery) Kick() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

func (d *Discovery) loop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		ifs := listLocalIfaces()
		d.mu.Lock()
		d.ifaces = ifs
		d.mu.Unlock()
		d.announce("hello", ifs)
		d.expire()
		select {
		case <-t.C:
		case <-d.kick:
		}
	}
}

func (d *Discovery) message(kind string) []byte {
	c := d.app.config()
	b, _ := json.Marshal(Announce{
		App: "cabledrop", V: 1, T: kind, ID: c.ID, Name: c.Name, OS: runtime.GOOS,
		Port: d.app.transferPort, DPort: d.port,
	})
	return b
}

func (d *Discovery) announce(kind string, ifs []LocalIface) {
	msg := d.message(kind)
	for _, ifc := range ifs {
		ones, bits := ifc.Net.Mask.Size()
		if bits != 32 || ones >= 31 {
			continue
		}
		// Socket liée à l'adresse de CETTE carte : le broadcast sort par elle.
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ifc.IP})
		if err != nil {
			continue
		}
		for _, p := range d.targets {
			_, _ = c.WriteToUDP(msg, &net.UDPAddr{IP: ifc.Broadcast, Port: p})
		}
		c.Close()
	}
	d.mu.Lock()
	manual := make([]string, 0, len(d.manual))
	for ip := range d.manual {
		manual = append(manual, ip)
	}
	d.mu.Unlock()
	for _, s := range manual {
		ip := net.ParseIP(s).To4()
		for _, p := range d.targets {
			d.sendTo(ip, p, msg)
		}
	}
}

// sendTo envoie un message unicast depuis une socket éphémère (évite sous
// Windows les erreurs « connexion réinitialisée » sur la socket d'écoute).
func (d *Discovery) sendTo(ip net.IP, port int, msg []byte) {
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip, Port: port})
	if err != nil {
		return
	}
	_, _ = c.Write(msg)
	c.Close()
}

func (d *Discovery) readLoop() {
	buf := make([]byte, 8192)
	myID := d.app.config().ID
	for {
		n, src, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var a Announce
		if json.Unmarshal(buf[:n], &a) != nil || a.App != "cabledrop" || a.ID == "" || a.ID == myID {
			continue
		}
		ip := src.IP.To4()
		if ip == nil {
			continue
		}
		d.handle(a, ip)
	}
}

func (d *Discovery) handle(a Announce, ip net.IP) {
	key := ip.String()
	if a.T == "bye" {
		d.mu.Lock()
		if p := d.peers[a.ID]; p != nil {
			delete(p.Routes, key)
			if len(p.Routes) == 0 {
				delete(d.peers, a.ID)
				logf("%s a quitté CableDrop.", p.Name)
			}
		}
		d.mu.Unlock()
		return
	}
	if a.Port <= 0 || a.Port > 65535 {
		return
	}
	name := cleanName(a.Name)
	if name == "" {
		name = "PC sans nom"
	}
	d.mu.Lock()
	local := d.localForLocked(ip)
	p := d.peers[a.ID]
	newPeer := p == nil
	if newPeer {
		p = &Peer{ID: a.ID, Routes: map[string]*Route{}}
		d.peers[a.ID] = p
	}
	p.Name, p.OS = name, a.OS
	r := p.Routes[key]
	newRoute := r == nil
	if newRoute {
		r = &Route{}
		p.Routes[key] = r
	}
	*r = Route{
		IP: ip, Port: a.Port, LocalIP: local.IP, Iface: local.Name, Kind: local.Kind,
		LinkLocal: ip.IsLinkLocalUnicast() || local.LinkLocal, Manual: d.manual[key], LastSeen: time.Now(),
	}
	d.mu.Unlock()

	if newRoute && a.T == "hello" && a.DPort > 0 && a.DPort < 65536 {
		d.sendTo(ip, a.DPort, d.message("reply"))
	}
	if newRoute {
		logf("PC détecté : %s — %s via %s (%s)", name, ip, local.Name, kindLabel(local.Kind, r.LinkLocal))
	}
}

// localForLocked trouve la carte de ce PC par laquelle on joint ip.
func (d *Discovery) localForLocked(ip net.IP) LocalIface {
	var best *LocalIface
	for i := range d.ifaces {
		ifc := &d.ifaces[i]
		if ifc.Net.Contains(ip) && (best == nil || ifc.score() > best.score()) {
			best = ifc
		}
	}
	if best != nil {
		return *best
	}
	// Sinon on demande au système quelle adresse locale il utiliserait.
	if c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip, Port: 9}); err == nil {
		la := c.LocalAddr().(*net.UDPAddr).IP.To4()
		c.Close()
		for _, ifc := range d.ifaces {
			if ifc.IP.Equal(la) {
				return ifc
			}
		}
		return LocalIface{Name: "?", IP: la, Kind: "other"}
	}
	return LocalIface{Name: "?", Kind: "other"}
}

func (d *Discovery) LocalFor(ip net.IP) LocalIface {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.localForLocked(ip)
}

func (d *Discovery) expire() {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, p := range d.peers {
		for k, r := range p.Routes {
			if now.Sub(r.LastSeen) > peerTimeout {
				delete(p.Routes, k)
			}
		}
		if len(p.Routes) == 0 {
			delete(d.peers, id)
			logf("%s n'est plus visible.", p.Name)
		}
	}
}

func (d *Discovery) AddManual(s string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(s)).To4()
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
		return "", errors.New("adresse IPv4 invalide (exemple : 169.254.12.34)")
	}
	d.mu.Lock()
	d.manual[ip.String()] = true
	d.mu.Unlock()
	d.Kick()
	return ip.String(), nil
}

func (d *Discovery) SendBye() {
	d.mu.Lock()
	ifs := d.ifaces
	d.mu.Unlock()
	d.announce("bye", ifs)
}

// Best renvoie le meilleur chemin (câble en priorité) vers un PC.
func (d *Discovery) Best(id string) (Peer, Route, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.peers[id]
	if p == nil {
		return Peer{}, Route{}, false
	}
	var best *Route
	for _, r := range p.Routes {
		if best == nil || r.score() > best.score() || (r.score() == best.score() && r.LastSeen.After(best.LastSeen)) {
			best = r
		}
	}
	if best == nil {
		return Peer{}, Route{}, false
	}
	return Peer{ID: p.ID, Name: p.Name, OS: p.OS}, *best, true
}

func (d *Discovery) NameOf(id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if p := d.peers[id]; p != nil {
		return p.Name
	}
	return ""
}

// ---------------------------------------------------------------- vues JSON

type RouteView struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Iface     string `json:"iface"`
	Kind      string `json:"kind"`
	LinkLocal bool   `json:"link_local"`
	Manual    bool   `json:"manual"`
}

type PeerView struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	OS     string      `json:"os"`
	Best   RouteView   `json:"best"`
	Routes []RouteView `json:"routes"`
}

type IfaceView struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Prefix    int    `json:"prefix"`
	Kind      string `json:"kind"`
	LinkLocal bool   `json:"link_local"`
}

func (d *Discovery) Peers() []PeerView {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]PeerView, 0, len(d.peers))
	for _, p := range d.peers {
		rs := make([]*Route, 0, len(p.Routes))
		for _, r := range p.Routes {
			rs = append(rs, r)
		}
		sort.Slice(rs, func(i, j int) bool { return rs[i].score() > rs[j].score() })
		v := PeerView{ID: p.ID, Name: p.Name, OS: p.OS}
		for _, r := range rs {
			v.Routes = append(v.Routes, RouteView{IP: r.IP.String(), Port: r.Port, Iface: r.Iface, Kind: r.Kind, LinkLocal: r.LinkLocal, Manual: r.Manual})
		}
		if len(v.Routes) > 0 {
			v.Best = v.Routes[0]
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (d *Discovery) Ifaces() []IfaceView {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]IfaceView, 0, len(d.ifaces))
	for _, i := range d.ifaces {
		ones, _ := i.Net.Mask.Size()
		out = append(out, IfaceView{Name: i.Name, IP: i.IP.String(), Prefix: ones, Kind: i.Kind, LinkLocal: i.LinkLocal})
	}
	return out
}
