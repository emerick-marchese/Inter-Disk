package main

// Reprise des transferts interrompus.
//
// Le PC qui reçoit garde les fichiers partiels (.cdpart) et note, pour chaque
// envoi, combien d'octets sont déjà écrits ET synchronisés sur le disque. Si le
// même PC renvoie exactement les mêmes fichiers, le transfert reprend là où il
// s'était arrêté, même après un plantage d'un des deux PC.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	partExt      = ".cdpart"
	resumeMaxAge = 14 * 24 * time.Hour
)

// checkpointEvery : fréquence des points de reprise (synchronisation disque).
var checkpointEvery int64 = 256 << 20

type resumeRec struct {
	DestDir string    `json:"dest_dir"`
	Rels    []string  `json:"rels"`
	Roots   []string  `json:"roots"`
	Safe    []int64   `json:"safe"` // octets sûrs sur le disque, par fichier
	Updated time.Time `json:"updated"`
}

type resumeStore struct {
	mu   sync.Mutex
	path string
	recs map[string]*resumeRec
}

func newResumeStore(dir string) *resumeStore {
	s := &resumeStore{path: filepath.Join(dir, "reprises.json"), recs: map[string]*resumeRec{}}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.recs)
	}
	// Nettoyage : les reprises trop anciennes sont abandonnées.
	changed := false
	for sig, r := range s.recs {
		if r == nil || time.Since(r.Updated) > resumeMaxAge || len(r.Rels) != len(r.Safe) {
			if r != nil {
				for _, rel := range r.Rels {
					os.Remove(filepath.Join(r.DestDir, filepath.FromSlash(rel)) + partExt)
				}
			}
			delete(s.recs, sig)
			changed = true
		}
	}
	if changed {
		s.saveLocked()
	}
	return s
}

func (s *resumeStore) saveLocked() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	b, _ := json.Marshal(s.recs)
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

func (s *resumeStore) get(sig string) *resumeRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.recs[sig]
	if r == nil {
		return nil
	}
	c := *r
	c.Rels = append([]string(nil), r.Rels...)
	c.Roots = append([]string(nil), r.Roots...)
	c.Safe = append([]int64(nil), r.Safe...)
	return &c
}

func (s *resumeStore) put(sig string, r *resumeRec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Updated = time.Now()
	s.recs[sig] = r
	s.saveLocked()
}

func (s *resumeStore) setSafe(sig string, i int, n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.recs[sig]; r != nil && i < len(r.Safe) {
		r.Safe[i] = n
		r.Updated = time.Now()
		s.saveLocked()
	}
}

func (s *resumeStore) del(sig string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.recs[sig]; ok {
		delete(s.recs, sig)
		s.saveLocked()
	}
}

// offerSignature identifie un envoi : même PC, mêmes fichiers (nom, taille, date).
func offerSignature(fromID string, files []FileEntry) string {
	h := sha256.New()
	h.Write([]byte(fromID + "\n"))
	for _, f := range files {
		h.Write([]byte(f.Path + "\x00" + strconv.FormatInt(f.Size, 10) + "\x00" + strconv.FormatInt(f.MTime, 10) + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// resumeOffsets calcule, pour chaque fichier, le nombre d'octets déjà reçus.
func resumeOffsets(r *resumeRec, files []FileEntry) []int64 {
	out := make([]int64, len(files))
	for i, f := range files {
		final := filepath.Join(r.DestDir, filepath.FromSlash(r.Rels[i]))
		if fi, err := os.Stat(final); err == nil && fi.Mode().IsRegular() && fi.Size() == f.Size && r.Safe[i] >= f.Size &&
			(f.MTime == 0 || fi.ModTime().UnixMilli() == f.MTime) {
			out[i] = f.Size // déjà complet
			continue
		}
		if fi, err := os.Stat(final + partExt); err == nil {
			out[i] = min(r.Safe[i], fi.Size(), f.Size)
			if out[i] == f.Size && f.Size > 0 {
				out[i] = f.Size - 1 // le fichier partiel n'a pas été finalisé : on renvoie la fin
			}
		}
		if out[i] < 0 {
			out[i] = 0
		}
	}
	return out
}

func sum64(v []int64) int64 {
	var s int64
	for _, x := range v {
		s += x
	}
	return s
}
