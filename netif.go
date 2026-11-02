package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// LocalIface est une adresse IPv4 d'une carte réseau de ce PC.
type LocalIface struct {
	Name      string
	IP        net.IP
	Net       *net.IPNet
	Broadcast net.IP
	Kind      string // cable | wifi | virtual | other
	LinkLocal bool   // 169.254.x.x : typique d'un câble direct PC <-> PC
}

func (l LocalIface) score() int { return routeScore(l.Kind, l.LinkLocal) }

// routeScore classe les chemins : le câble direct passe toujours avant le Wi-Fi.
func routeScore(kind string, linkLocal bool) int {
	s := 0
	switch kind {
	case "cable":
		s = 30
	case "other":
		s = 20
	case "virtual":
		s = 10
	case "wifi":
		s = 5
	}
	if linkLocal && kind != "wifi" {
		s += 5
	}
	return s
}

// routeClass range un chemin dans l'un des modes proposés à l'utilisateur :
// « cable » (câble direct ou Ethernet), « wifi », ou « network » (autre).
func routeClass(kind string, linkLocal bool) string {
	switch {
	case kind == "cable", linkLocal && kind != "wifi":
		return "cable"
	case kind == "wifi":
		return "wifi"
	}
	return "network"
}

func kindLabel(kind string, linkLocal bool) string {
	switch kind {
	case "cable":
		if linkLocal {
			return "câble Ethernet direct"
		}
		return "Ethernet"
	case "wifi":
		return "Wi-Fi"
	case "virtual":
		return "carte virtuelle"
	}
	if linkLocal {
		return "lien direct"
	}
	return "réseau"
}

var (
	reVirtual = regexp.MustCompile(`(?i)(vethernet|virtual|vmware|vmnet|vbox|hyper-v|bluetooth|loopback|tailscale|zerotier|hamachi|vpn|wireguard|openvpn|tap-|^tap|^tun|utun|wsl|docker|^br-|^veth|^virbr|^lxc|^lxd|^awdl|^llw|^anpi|^gif|^stf|npcap|radmin|\*)`)
	reWifi    = regexp.MustCompile(`(?i)(wi-?fi|wlan|wireless|sans fil|802\.11|airport|^wl)`)
	reCable   = regexp.MustCompile(`(?i)(ethernet|^eth|^en|^em\d|r[ée]seau local|local area|\blan\b|thunderbolt)`)
)

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// kindOverride force le type d'une carte (tests uniquement),
// ex. CABLEDROP_IFACE_KINDS="eth0=wifi,ifb0=cable".
var kindOverride = func() map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(os.Getenv("CABLEDROP_IFACE_KINDS"), ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(kv), "="); ok {
			m[k] = v
		}
	}
	return m
}()

func classify(name string) string {
	if k, ok := kindOverride[name]; ok {
		return k
	}
	switch runtime.GOOS {
	case "linux":
		base := filepath.Join("/sys/class/net", name)
		if exists(base) {
			if exists(base+"/wireless") || exists(base+"/phy80211") {
				return "wifi"
			}
			if !exists(base + "/device") {
				return "virtual"
			}
			if strings.HasPrefix(name, "ww") {
				return "other"
			}
			return "cable"
		}
	case "darwin":
		if k, ok := macPorts()[name]; ok {
			return k
		}
	}
	switch {
	case reVirtual.MatchString(name):
		return "virtual"
	case reWifi.MatchString(name):
		return "wifi"
	case reCable.MatchString(name):
		return "cable"
	}
	return "other"
}

var (
	macMu      sync.Mutex
	macCache   map[string]string
	macCacheAt time.Time
)

// macPorts associe les cartes macOS (en0, en5…) à leur type via networksetup.
func macPorts() map[string]string {
	macMu.Lock()
	defer macMu.Unlock()
	if macCache != nil && time.Since(macCacheAt) < time.Minute {
		return macCache
	}
	m := map[string]string{}
	out, err := exec.Command("networksetup", "-listallhardwareports").Output()
	if err == nil {
		port := ""
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "Hardware Port:"):
				port = strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))
			case strings.HasPrefix(line, "Device:"):
				dev := strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
				lp := strings.ToLower(port)
				switch {
				case strings.Contains(lp, "wi-fi") || strings.Contains(lp, "airport"):
					m[dev] = "wifi"
				case strings.Contains(lp, "ethernet") || strings.Contains(lp, "lan") || strings.Contains(lp, "thunderbolt"):
					m[dev] = "cable"
				case strings.Contains(lp, "bluetooth") || strings.Contains(lp, "iphone"):
					m[dev] = "virtual"
				default:
					m[dev] = "other"
				}
			}
		}
	}
	macCache, macCacheAt = m, time.Now()
	return m
}

// listLocalIfaces liste les adresses IPv4 actives, le câble en premier.
func listLocalIfaces() []LocalIface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []LocalIface
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		kind := ""
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil {
				continue
			}
			mask := ipn.Mask
			if len(mask) == net.IPv6len {
				mask = mask[12:]
			}
			if len(mask) != net.IPv4len {
				continue
			}
			if kind == "" {
				kind = classify(ifc.Name)
			}
			bc := make(net.IP, 4)
			for i := range bc {
				bc[i] = ip4[i] | ^mask[i]
			}
			out = append(out, LocalIface{
				Name:      ifc.Name,
				IP:        ip4,
				Net:       &net.IPNet{IP: ip4.Mask(mask), Mask: mask},
				Broadcast: bc,
				Kind:      kind,
				LinkLocal: ip4.IsLinkLocalUnicast(),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score() > out[j].score() })
	return out
}
