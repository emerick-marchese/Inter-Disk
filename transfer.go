package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Protocole TCP (une connexion par envoi) :
//   1. expéditeur → "CDROP\x01" + Offer   (liste des fichiers et tailles)
//   2. destinataire → Answer               (accepté / refusé, après clic)
//   3. expéditeur → contenu brut des fichiers, l'un après l'autre
//   4. destinataire → Done                 (tout est écrit sur le disque)
// Chaque message JSON est précédé de sa longueur sur 4 octets.

// Version 2 du protocole : reprise des transferts interrompus (Answer.Offsets).
// Un PC en version 1 continue de fonctionner, simplement sans reprise.
var protoVersion = 2

const (
	protoMagic     = "CDROP\x01"
	maxFiles       = 500000
	askTimeout     = 2 * time.Minute
	idleTimeout    = 60 * time.Second
	copyBufferSize = 1 << 20
)

type FileEntry struct {
	Path  string `json:"path"` // chemin relatif envoyé à l'autre PC (séparateur « / »)
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime,omitempty"` // millisecondes Unix
	abs   string // chemin complet sur le disque (expéditeur uniquement, jamais transmis)
}

type Offer struct {
	App      string      `json:"app"`
	V        int         `json:"v"`
	FromID   string      `json:"from_id"`
	FromName string      `json:"from_name"`
	Files    []FileEntry `json:"files"`
	Total    int64       `json:"total"`
}

type Answer struct {
	Accept  bool    `json:"accept"`
	Reason  string  `json:"reason,omitempty"`
	Offsets []int64 `json:"offsets,omitempty"` // v2 : octets déjà reçus par fichier (reprise)
}

type Done struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

const (
	stConnecting   = "connecting"
	stWaiting      = "waiting"
	stPending      = "pending"
	stTransferring = "transferring"
	stDone         = "done"
	stRejected     = "rejected"
	stExpired      = "expired"
	stCancelled    = "cancelled"
	stError        = "error"
)

func isTerminal(s string) bool {
	switch s {
	case stDone, stRejected, stExpired, stCancelled, stError:
		return true
	}
	return false
}

func writeMsg(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err = w.Write(buf)
	return err
}

func readMsg(r io.Reader, v any, max int) error {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(h[:])
	if int64(n) > int64(max) {
		return errors.New("message trop grand")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// idleConn coupe la connexion si plus rien ne passe pendant `timeout`
// (câble débranché, PC en veille…).
type idleConn struct {
	net.Conn
	timeout time.Duration
}

func (c idleConn) Read(p []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.timeout))
	return c.Conn.Read(p)
}

func (c idleConn) Write(p []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(c.timeout))
	return c.Conn.Write(p)
}

type countingWriter struct {
	w   io.Writer
	n   *atomic.Int64
	err error
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	if err != nil {
		c.err = err
	}
	return n, err
}

// pacedWriter limite le débit d'envoi (réglage « Vitesse d'envoi maximale ») et
// découpe les écritures en petits morceaux réguliers, pour ménager les pilotes
// réseau qui supportent mal la pleine charge.
type pacedWriter struct {
	w     io.Writer
	rate  float64 // octets par seconde
	start time.Time
	sent  int64
}

func (p *pacedWriter) Write(b []byte) (int, error) {
	if p.start.IsZero() {
		p.start = time.Now()
	}
	total := 0
	for len(b) > 0 {
		n := min(len(b), 64<<10)
		due := p.start.Add(time.Duration(float64(p.sent) / p.rate * float64(time.Second)))
		if d := time.Until(due); d > 0 {
			time.Sleep(d)
		}
		w, err := p.w.Write(b[:n])
		total += w
		p.sent += int64(w)
		if err != nil {
			return total, err
		}
		b = b[n:]
	}
	return total, nil
}

// ---------------------------------------------------------------- Transfer

type Transfer struct {
	ID        string
	Dir       string // out | in
	PeerID    string
	PeerName  string
	PeerIP    string
	Iface     string
	Kind      string
	LinkLocal bool
	Files     []FileEntry
	Total     int64
	Created   time.Time

	done      atomic.Int64
	cancelled atomic.Bool

	mu        sync.Mutex
	state     string
	errMsg    string
	cancelMsg string
	cur       int
	started   time.Time
	finished  time.Time
	expires   time.Time
	destDir   string
	roots     []string
	freeSpace int64
	conn      net.Conn
	lastT     time.Time
	lastB     int64
	speed     float64
	resumed   int64     // octets déjà présents au départ (reprise)
	lastDone  int64     // pour détecter un transfert bloqué
	lastMove  time.Time // dernier moment où des données sont passées

	decision   chan bool
	cancelCh   chan struct{}
	endCh      chan struct{}
	cancelOnce sync.Once
	endOnce    sync.Once
}

func newTransfer(dir string) *Transfer {
	return &Transfer{
		ID: randomID(6), Dir: dir, Created: time.Now(), freeSpace: -1,
		decision: make(chan bool, 1),
		cancelCh: make(chan struct{}), endCh: make(chan struct{}),
	}
}

func (t *Transfer) State() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

func (t *Transfer) setState(s, msg string) {
	t.mu.Lock()
	if isTerminal(t.state) {
		t.mu.Unlock()
		return
	}
	t.state = s
	if msg != "" {
		t.errMsg = msg
	}
	if s == stTransferring {
		t.started = time.Now()
	}
	if isTerminal(s) {
		t.finished = time.Now()
	}
	t.mu.Unlock()
	if isTerminal(s) {
		t.endOnce.Do(func() { close(t.endCh) })
	}
}

// fail termine le transfert en erreur, sauf s'il a été annulé par l'utilisateur.
func (t *Transfer) fail(msg string) {
	if t.cancelled.Load() {
		t.mu.Lock()
		reason := t.cancelMsg
		t.mu.Unlock()
		if reason != "" {
			t.setState(stError, reason)
		} else {
			t.setState(stCancelled, "")
		}
		return
	}
	t.setState(stError, msg)
}

func (t *Transfer) setConn(c net.Conn) bool {
	t.mu.Lock()
	t.conn = c
	t.mu.Unlock()
	return !t.cancelled.Load()
}

func (t *Transfer) setCurrent(i int) {
	t.mu.Lock()
	t.cur = i
	t.mu.Unlock()
}

// Cancel annule (ou refuse, pour une demande en attente).
func (t *Transfer) Cancel(reason string) {
	t.mu.Lock()
	st, c := t.state, t.conn
	if reason != "" && t.cancelMsg == "" {
		t.cancelMsg = reason
	}
	t.mu.Unlock()
	if isTerminal(st) {
		return
	}
	t.cancelled.Store(true)
	t.cancelOnce.Do(func() { close(t.cancelCh) })
	if st == stPending {
		select {
		case t.decision <- false:
		default:
		}
		return
	}
	if c != nil {
		c.Close()
	}
}

func (t *Transfer) Answer(accept bool) error {
	if t.State() != stPending {
		return errors.New("cette demande a expiré ou a été annulée")
	}
	select {
	case t.decision <- accept:
	default:
	}
	return nil
}

type TransferView struct {
	ID        string   `json:"id"`
	Dir       string   `json:"dir"`
	PeerName  string   `json:"peer_name"`
	PeerIP    string   `json:"peer_ip"`
	Iface     string   `json:"iface"`
	Kind      string   `json:"kind"`
	LinkLocal bool     `json:"link_local"`
	State     string   `json:"state"`
	Error     string   `json:"error,omitempty"`
	Count     int      `json:"count"`
	Total     int64    `json:"total"`
	Done      int64    `json:"done"`
	Speed     float64  `json:"speed"`
	ETA       float64  `json:"eta"`
	Elapsed   float64  `json:"elapsed"`
	Current   string   `json:"current,omitempty"`
	CurIndex  int      `json:"cur_index"`
	First     string   `json:"first"`
	ExpiresIn float64  `json:"expires_in,omitempty"`
	Preview   []string `json:"preview,omitempty"`
	DestDir   string   `json:"dest_dir,omitempty"`
	Roots     []string `json:"roots,omitempty"`
	FreeSpace int64    `json:"free_space"`
	Resumed   int64    `json:"resumed"`
	Stalled   float64  `json:"stalled"` // secondes sans aucune donnée
}

func (t *Transfer) view() TransferView {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	done := t.done.Load()
	v := TransferView{
		ID: t.ID, Dir: t.Dir, PeerName: t.PeerName, PeerIP: t.PeerIP, Iface: t.Iface, Kind: t.Kind,
		LinkLocal: t.LinkLocal, State: t.state, Error: t.errMsg, Count: len(t.Files), Total: t.Total,
		Done: done, CurIndex: t.cur, DestDir: t.destDir, Roots: t.roots, FreeSpace: t.freeSpace,
		Resumed: t.resumed,
	}
	if len(t.Files) > 0 {
		v.First = t.Files[0].Path
	}
	switch t.state {
	case stTransferring:
		if t.lastT.IsZero() {
			t.lastT, t.lastB = now, done
		} else if dt := now.Sub(t.lastT).Seconds(); dt >= 0.4 {
			inst := float64(done-t.lastB) / dt
			if t.speed == 0 {
				t.speed = inst
			} else {
				t.speed = 0.5*t.speed + 0.5*inst
			}
			t.lastT, t.lastB = now, done
		}
		if t.lastMove.IsZero() || done != t.lastDone {
			t.lastDone, t.lastMove = done, now
		}
		if s := now.Sub(t.lastMove).Seconds(); s >= 3 {
			v.Stalled = s
			t.speed = 0
		}
		v.Speed = t.speed
		if v.Speed > 0 {
			v.ETA = float64(t.Total-done) / v.Speed
		}
		v.Elapsed = now.Sub(t.started).Seconds()
		if t.cur >= 0 && t.cur < len(t.Files) {
			v.Current = t.Files[t.cur].Path
		}
	case stDone:
		if !t.started.IsZero() {
			v.Elapsed = t.finished.Sub(t.started).Seconds()
			if v.Elapsed > 0 {
				v.Speed = float64(t.Total-t.resumed) / v.Elapsed
			}
		}
	case stPending:
		v.ExpiresIn = max(0, t.expires.Sub(now).Seconds())
		for i, f := range t.Files {
			if i >= 8 {
				break
			}
			v.Preview = append(v.Preview, f.Path)
		}
	}
	return v
}

// ---------------------------------------------------------------- Manager

type TransferManager struct {
	app      *App
	mu       sync.Mutex
	list     []*Transfer
	byID     map[string]*Transfer
	reserved map[string]int // dossiers/fichiers de destination en cours d'écriture
	resume   *resumeStore
}

func NewTransferManager(app *App) *TransferManager {
	return &TransferManager{app: app, byID: map[string]*Transfer{}, reserved: map[string]int{},
		resume: newResumeStore(filepath.Dir(app.cfgPath))}
}

func (m *TransferManager) add(t *Transfer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list = append(m.list, t)
	m.byID[t.ID] = t
	// Garde un historique raisonnable.
	for len(m.list) > 200 {
		old := m.list[0]
		if !isTerminal(old.State()) {
			break
		}
		m.list = m.list[1:]
		delete(m.byID, old.ID)
	}
}

func (m *TransferManager) Get(id string) *Transfer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id]
}

func (m *TransferManager) Views() []TransferView {
	m.mu.Lock()
	list := append([]*Transfer(nil), m.list...)
	m.mu.Unlock()
	out := make([]TransferView, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		out = append(out, list[i].view())
	}
	return out
}

// HasActive indique si un transfert est en cours (ou une demande en attente).
func (m *TransferManager) HasActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.list {
		if !isTerminal(t.State()) {
			return true
		}
	}
	return false
}

func (m *TransferManager) ClearFinished() {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := m.list[:0]
	for _, t := range m.list {
		if isTerminal(t.State()) {
			delete(m.byID, t.ID)
		} else {
			keep = append(keep, t)
		}
	}
	m.list = keep
}

func reservedKey(p string) string { return strings.ToLower(filepath.Clean(p)) }

func (m *TransferManager) isReserved(p string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reserved[reservedKey(p)] > 0
}

func (m *TransferManager) reserve(paths []string, delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range paths {
		k := reservedKey(p)
		m.reserved[k] += delta
		if m.reserved[k] <= 0 {
			delete(m.reserved, k)
		}
	}
}

// ---------------------------------------------------------------- envoi

func (m *TransferManager) StartSend(peer Peer, r Route, files []FileEntry) (*Transfer, error) {
	if len(files) == 0 {
		return nil, errors.New("aucun fichier sélectionné")
	}
	if len(files) > maxFiles {
		return nil, fmt.Errorf("trop de fichiers (%d maximum)", maxFiles)
	}
	clean := make([]FileEntry, 0, len(files))
	var total int64
	for _, f := range files {
		p := strings.Trim(strings.ReplaceAll(f.Path, "\\", "/"), "/")
		if p == "" || f.Size < 0 || f.abs == "" {
			return nil, errors.New("liste de fichiers invalide")
		}
		clean = append(clean, FileEntry{Path: p, Size: f.Size, MTime: f.MTime, abs: f.abs})
		total += f.Size
	}
	t := newTransfer("out")
	t.PeerID, t.PeerName, t.PeerIP = peer.ID, peer.Name, r.IP.String()
	t.Iface, t.Kind, t.LinkLocal = r.Iface, r.Kind, r.LinkLocal
	t.Files, t.Total, t.state = clean, total, stConnecting
	m.add(t)
	logf("Envoi vers %s (%s via %s) : %d fichier(s), %s", peer.Name, r.IP, r.Iface, len(clean), fmtSize(total))
	go m.runSend(t, r)
	return t, nil
}

func (m *TransferManager) runSend(t *Transfer, r Route) {
	addr := net.JoinHostPort(r.IP.String(), strconv.Itoa(r.Port))
	d := net.Dialer{Timeout: 6 * time.Second}
	if r.LocalIP != nil {
		// On force la sortie par la carte du câble.
		d.LocalAddr = &net.TCPAddr{IP: r.LocalIP}
	}
	conn, err := d.Dial("tcp4", addr)
	if err != nil && d.LocalAddr != nil {
		d.LocalAddr = nil
		conn, err = d.Dial("tcp4", addr)
	}
	if err != nil {
		t.fail("Connexion impossible à " + t.PeerName + " (" + addr + "). Vérifiez qu'Inter Disk est ouvert sur l'autre PC et que le pare-feu l'autorise.")
		return
	}
	defer conn.Close()
	if !t.setConn(conn) {
		t.fail("")
		return
	}

	cfg := m.app.config()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := io.WriteString(conn, protoMagic); err == nil {
		err = writeMsg(conn, Offer{App: "cabledrop", V: protoVersion, FromID: cfg.ID, FromName: cfg.Name, Files: t.Files, Total: t.Total})
	}
	if err != nil {
		t.fail("Échec de l'envoi de la demande : " + err.Error())
		return
	}
	t.setState(stWaiting, "")

	_ = conn.SetDeadline(time.Now().Add(askTimeout + 30*time.Second))
	var ans Answer
	if err := readMsg(conn, &ans, 1<<16); err != nil {
		t.fail("Pas de réponse de " + t.PeerName + ".")
		return
	}
	_ = conn.SetDeadline(time.Time{})
	if !ans.Accept {
		msg := "Refusé par " + t.PeerName
		if ans.Reason != "" {
			msg += " : " + ans.Reason
		}
		t.setState(stRejected, msg)
		logf("%s", msg)
		return
	}
	// Reprise : le destinataire indique ce qu'il a déjà (v2). Sinon, tout est envoyé.
	resumable := ans.Offsets != nil
	offsets := ans.Offsets
	if len(offsets) != len(t.Files) {
		offsets = make([]int64, len(t.Files))
	}
	for i, off := range offsets {
		if off < 0 || off > t.Files[i].Size {
			t.fail("Réponse invalide de " + t.PeerName + ".")
			return
		}
	}
	t.mu.Lock()
	t.resumed = sum64(offsets)
	t.mu.Unlock()
	t.done.Store(t.resumed)
	if t.resumed > 0 {
		logf("Reprise de l'envoi vers %s : %s déjà reçus.", t.PeerName, fmtSize(t.resumed))
	}
	t.setState(stTransferring, "")

	// Les fichiers sont lus directement sur le disque, par blocs de 1 Mo :
	// la mémoire utilisée reste la même quelle que soit la taille du fichier.
	var out io.Writer = idleConn{conn, idleTimeout}
	if lim := cfg.MaxSendMBps; lim > 0 {
		out = &pacedWriter{w: out, rate: float64(lim) * 1e6}
		logf("Vitesse d'envoi limitée à %d Mo/s.", lim)
	}
	cw := &countingWriter{w: out, n: &t.done}
	buf := make([]byte, copyBufferSize)
	interrupted := "Transfert interrompu par l'autre PC (annulé, câble débranché, PC en veille ou planté)."
	if resumable {
		interrupted += " Renvoyez les mêmes fichiers : le transfert reprendra là où il s'est arrêté."
	}
	for i, f := range t.Files {
		t.setCurrent(i)
		off := offsets[i]
		if f.Size == 0 || off >= f.Size {
			continue
		}
		fh, err := os.Open(f.abs)
		if err != nil {
			t.fail("Impossible d'ouvrir « " + f.Path + " » : " + err.Error())
			return
		}
		if off > 0 {
			if _, err := fh.Seek(off, io.SeekStart); err != nil {
				fh.Close()
				t.fail("Lecture impossible : " + err.Error())
				return
			}
		}
		n, err := io.CopyBuffer(cw, io.LimitReader(fh, f.Size-off), buf)
		fh.Close()
		if err == nil && n != f.Size-off {
			err = fmt.Errorf("le fichier « %s » a été modifié ou raccourci pendant l'envoi", f.Path)
		}
		if err != nil {
			if cw.err != nil {
				t.fail(interrupted)
			} else {
				t.fail("Lecture impossible : " + err.Error())
			}
			return
		}
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
	var done Done
	if err := readMsg(conn, &done, 1<<16); err != nil {
		t.fail("Pas de confirmation de réception de " + t.PeerName + ".")
		return
	}
	if !done.OK {
		t.fail("Erreur côté destinataire : " + done.Error)
		return
	}
	t.setState(stDone, "")
	v := t.view()
	logf("Envoi terminé vers %s : %s en %.1f s (%s/s)", t.PeerName, fmtSize(t.Total), v.Elapsed, fmtSize(int64(v.Speed)))
}

// ---------------------------------------------------------------- réception

func (m *TransferManager) Serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go m.handleIncoming(c)
	}
}

func (m *TransferManager) handleIncoming(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	magic := make([]byte, len(protoMagic))
	if _, err := io.ReadFull(conn, magic); err != nil || string(magic) != protoMagic {
		return
	}
	var offer Offer
	if err := readMsg(conn, &offer, 128<<20); err != nil || offer.App != "cabledrop" {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if len(offer.Files) == 0 || len(offer.Files) > maxFiles {
		_ = writeMsg(conn, Answer{Accept: false, Reason: "liste de fichiers invalide"})
		return
	}
	files := make([]FileEntry, 0, len(offer.Files))
	var total int64
	for _, f := range offer.Files {
		p, err := sanitizePath(f.Path)
		if err != nil || f.Size < 0 {
			_ = writeMsg(conn, Answer{Accept: false, Reason: "liste de fichiers invalide"})
			return
		}
		files = append(files, FileEntry{Path: p, Size: f.Size, MTime: f.MTime})
		total += f.Size
	}

	remote := conn.RemoteAddr().(*net.TCPAddr).IP.To4()
	local := m.app.disc.LocalFor(remote)
	name := cleanName(offer.FromName)
	if n := m.app.disc.NameOf(offer.FromID); n != "" {
		name = n
	}
	if name == "" {
		name = remote.String()
	}

	t := newTransfer("in")
	t.PeerID, t.PeerName, t.PeerIP = offer.FromID, name, remote.String()
	t.Iface, t.Kind, t.LinkLocal = local.Name, local.Kind, remote.IsLinkLocalUnicast() || local.LinkLocal
	t.Files, t.Total, t.state = files, total, stPending
	t.expires = time.Now().Add(askTimeout)
	t.destDir = m.app.config().DownloadDir
	t.freeSpace = freeSpace(existingParent(t.destDir))

	// Reprise possible ? (même PC, mêmes fichiers, même dossier de réception)
	resumable := offer.V >= 2
	sig := offerSignature(offer.FromID, files)
	var rec *resumeRec
	if resumable {
		if r := m.resume.get(sig); r != nil && r.DestDir == t.destDir && len(r.Rels) == len(files) && len(r.Safe) == len(files) {
			rec = r
			t.resumed = sum64(resumeOffsets(rec, files))
		}
	}
	m.add(t)
	logf("Demande de %s : %d fichier(s), %s%s.", name, len(files), fmtSize(total),
		map[bool]string{true: " (reprise : " + fmtSize(t.resumed) + " déjà reçus)", false: ""}[t.resumed > 0])
	m.app.notifyIncoming()

	// Pendant l'attente, on surveille si l'expéditeur abandonne.
	gone := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := conn.Read(b[:])
		gone <- err
	}()
	accept, reason, expired := false, "", false
	timer := time.NewTimer(askTimeout)
	defer timer.Stop()
	select {
	case accept = <-t.decision:
	case <-timer.C:
		expired, reason = true, "pas de réponse dans le délai"
	case <-gone:
		t.setState(stCancelled, "Annulé par l'expéditeur.")
		return
	}
	_ = conn.SetReadDeadline(time.Now())
	if err := <-gone; err == nil {
		t.setState(stError, "Erreur de protocole.")
		return
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.setState(stCancelled, "Annulé par l'expéditeur.")
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if !accept {
		_ = writeMsg(conn, Answer{Accept: false, Reason: reason})
		if expired {
			t.setState(stExpired, "Demande expirée (pas de réponse).")
		} else {
			t.setState(stRejected, "Vous avez refusé.")
		}
		return
	}

	dest := t.destDir
	if err := os.MkdirAll(dest, 0o755); err != nil {
		_ = writeMsg(conn, Answer{Accept: false, Reason: "dossier de réception inaccessible"})
		t.setState(stError, "Dossier de réception inaccessible : "+err.Error())
		return
	}
	var rels, roots []string
	offsets := make([]int64, len(files))
	if rec != nil {
		rels, roots = rec.Rels, rec.Roots
		offsets = resumeOffsets(rec, files)
	} else {
		rels, roots = planPaths(dest, files, m.isReserved)
		if resumable {
			m.resume.put(sig, &resumeRec{DestDir: dest, Rels: rels, Roots: roots, Safe: make([]int64, len(files))})
		}
	}
	full := make([]string, len(roots))
	for i, r := range roots {
		full[i] = filepath.Join(dest, r)
	}
	m.reserve(full, 1)
	defer m.reserve(full, -1)
	t.mu.Lock()
	t.roots = roots
	t.mu.Unlock()

	if !t.setConn(conn) {
		t.fail("")
		return
	}
	ans := Answer{Accept: true}
	if resumable {
		ans.Offsets = offsets
	}
	if err := writeMsg(conn, ans); err != nil {
		t.fail("Connexion perdue avec " + name + ".")
		return
	}
	t.mu.Lock()
	t.resumed = sum64(offsets)
	t.mu.Unlock()
	t.done.Store(t.resumed)
	t.setState(stTransferring, "")
	logf("Réception depuis %s vers %s (déjà reçus : %s)…", name, dest, fmtSize(t.resumed))
	interrupted := "Transfert interrompu par l'autre PC (annulé, câble débranché, PC en veille ou planté)."
	if resumable {
		interrupted += " Le début est conservé : renvoyez les mêmes fichiers depuis l'autre PC pour reprendre."
	}

	ic := idleConn{conn, idleTimeout}
	buf := make([]byte, copyBufferSize)
	madeDirs := map[string]bool{}
	for i, f := range files {
		t.setCurrent(i)
		final := filepath.Join(dest, filepath.FromSlash(rels[i]))
		dir := filepath.Dir(final)
		if !madeDirs[dir] {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.fail("Impossible de créer le dossier " + dir + " : " + err.Error())
				return
			}
			madeDirs[dir] = true
		}
		part := final + partExt
		off := offsets[i]
		if off >= f.Size && f.Size > 0 {
			continue // déjà reçu lors d'un envoi précédent
		}
		fh, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			err = fh.Truncate(off)
			if err == nil {
				_, err = fh.Seek(off, io.SeekStart)
			}
			if err != nil {
				fh.Close()
			}
		}
		if err != nil {
			t.fail("Impossible d'écrire " + final + " : " + err.Error())
			return
		}
		bw := bufio.NewWriterSize(fh, 2<<20)
		cw := &countingWriter{w: bw, n: &t.done}
		written, remaining := off, f.Size-off
		for remaining > 0 && err == nil {
			chunk := min(remaining, checkpointEvery)
			var n int64
			n, err = io.CopyBuffer(cw, io.LimitReader(ic, chunk), buf)
			written += n
			remaining -= n
			if err == nil && n != chunk {
				err = io.ErrUnexpectedEOF
			}
			if err == nil && resumable && remaining > 0 {
				// Point de reprise : données vidées et synchronisées sur le disque.
				if err = bw.Flush(); err == nil {
					err = fh.Sync()
				}
				if err != nil {
					cw.err = err
				} else {
					m.resume.setSafe(sig, i, written)
				}
			}
		}
		if err == nil {
			if err = bw.Flush(); err == nil && f.Size >= 64<<20 {
				err = fh.Sync()
			}
			if err != nil {
				cw.err = err
			}
		}
		if cerr := fh.Close(); err == nil && cerr != nil {
			err, cw.err = cerr, cerr
		}
		if err != nil {
			if cw.err != nil {
				t.fail("Erreur d'écriture sur le disque (disque plein ?) : " + cw.err.Error())
			} else {
				_ = bw.Flush()
				t.fail(interrupted)
			}
			if !resumable {
				os.Remove(part)
			}
			return
		}
		final = uniquePath(final)
		if err := os.Rename(part, final); err != nil {
			os.Remove(part)
			t.fail("Impossible de finaliser " + final + " : " + err.Error())
			return
		}
		if f.MTime > 0 {
			mt := time.UnixMilli(f.MTime)
			_ = os.Chtimes(final, mt, mt)
		}
		if resumable {
			m.resume.setSafe(sig, i, f.Size)
		}
	}
	if resumable {
		m.resume.del(sig)
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_ = writeMsg(conn, Done{OK: true})
	t.setState(stDone, "")
	v := t.view()
	logf("Réception terminée depuis %s : %s en %.1f s (%s/s)", name, fmtSize(total), v.Elapsed, fmtSize(int64(v.Speed)))
}
