package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var winReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// sanitizeComponent rend un nom de fichier valide sur Windows, macOS et Linux.
func sanitizeComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), " .")
	if out == "" {
		out = "_"
	}
	base := strings.ToUpper(strings.TrimSpace(strings.SplitN(out, ".", 2)[0]))
	if winReserved[base] {
		out = "_" + out
	}
	if len(out) > 240 {
		ext := path.Ext(out)
		if len(ext) > 20 {
			ext = ""
		}
		cut := 240 - len(ext)
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		out = out[:cut] + ext
	}
	return out
}

// sanitizePath transforme un chemin relatif reçu en chemin sûr (pas de « .. »,
// pas de chemin absolu, pas de lettre de lecteur).
func sanitizePath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	var parts []string
	for _, c := range strings.Split(p, "/") {
		if c == "" || c == "." {
			continue
		}
		parts = append(parts, sanitizeComponent(c))
	}
	if len(parts) == 0 {
		return "", errors.New("chemin vide")
	}
	if len(parts) > 64 {
		return "", errors.New("arborescence trop profonde")
	}
	return strings.Join(parts, "/"), nil
}

func withSuffix(name string, n int, isDir bool) string {
	if isDir {
		return fmt.Sprintf("%s (%d)", name, n)
	}
	ext := path.Ext(name)
	if ext == name || ext == "" || len(ext) > 12 {
		return fmt.Sprintf("%s (%d)", name, n)
	}
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext)
}

// planPaths choisit où écrire chaque fichier reçu sans jamais écraser un
// fichier existant : « photo.jpg » devient « photo (1).jpg », un dossier
// « Vacances » déjà présent devient « Vacances (1) ».
func planPaths(dest string, files []FileEntry, reserved func(string) bool) (rels []string, roots []string) {
	rootMap := map[string]string{}
	usedRoot := map[string]bool{}
	usedFile := map[string]bool{}
	rels = make([]string, len(files))
	for i, f := range files {
		parts := strings.Split(f.Path, "/")
		isDir := len(parts) > 1
		key := "f:" + strings.ToLower(parts[0])
		if isDir {
			key = "d:" + strings.ToLower(parts[0])
		}
		mapped, ok := rootMap[key]
		if !ok {
			for n := 0; ; n++ {
				cand := parts[0]
				if n > 0 {
					cand = withSuffix(parts[0], n, isDir)
				}
				full := filepath.Join(dest, cand)
				if usedRoot[strings.ToLower(cand)] || (reserved != nil && reserved(full)) {
					continue
				}
				if _, err := os.Lstat(full); err == nil {
					continue
				}
				mapped = cand
				break
			}
			rootMap[key] = mapped
			usedRoot[strings.ToLower(mapped)] = true
			roots = append(roots, mapped)
		}
		parts[0] = mapped
		rel := strings.Join(parts, "/")
		if usedFile[strings.ToLower(rel)] {
			dir, name := path.Split(rel)
			for n := 1; ; n++ {
				cand := dir + withSuffix(name, n, false)
				if !usedFile[strings.ToLower(cand)] {
					rel = cand
					break
				}
			}
		}
		usedFile[strings.ToLower(rel)] = true
		rels[i] = rel
	}
	return rels, roots
}

// uniquePath renvoie p, ou « p (1) », « p (2) »… si p existe déjà.
func uniquePath(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	dir, name := filepath.Split(p)
	for n := 1; ; n++ {
		cand := filepath.Join(dir, withSuffix(name, n, false))
		if _, err := os.Lstat(cand); err != nil {
			return cand
		}
	}
}

// existingParent remonte jusqu'au premier dossier qui existe (pour l'espace libre).
func existingParent(p string) string {
	p = filepath.Clean(p)
	for {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// ---------------------------------------------------------------- côté expéditeur

// Item décrit un élément choisi par l'utilisateur (fichier ou dossier).
type Item struct {
	Path  string `json:"path"` // chemin complet
	Name  string `json:"name"` // nom affiché
	IsDir bool   `json:"is_dir"`
	Count int    `json:"count"` // nombre de fichiers (1 pour un fichier)
	Size  int64  `json:"size"`
}

// displayName donne le nom d'un fichier/dossier, y compris pour une racine de
// disque (« D:\ » devient « D »).
func displayName(p string) string {
	p = filepath.Clean(p)
	name := filepath.Base(p)
	if name == "." || name == string(filepath.Separator) || name == "" || strings.HasSuffix(name, ":") || name == filepath.VolumeName(p) {
		if v := strings.Trim(filepath.VolumeName(p), `:\/`); v != "" {
			return v
		}
		return "Disque"
	}
	return name
}

// walkFiles parcourt un fichier ou un dossier et appelle fn pour chaque fichier
// normal (les raccourcis/liens symboliques sont ignorés pour éviter les boucles).
func walkFiles(root string, fn func(abs, rel string, info fs.FileInfo)) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	name := displayName(root)
	if !info.IsDir() {
		if info.Mode().IsRegular() {
			fn(root, name, info)
		}
		return nil
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && p != root {
				return fs.SkipDir // sous-dossier illisible : on continue sans lui
			}
			if p == root {
				return err
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		fn(p, name+"/"+filepath.ToSlash(rel), fi)
		return nil
	})
}

// describePaths résume les éléments choisis (taille, nombre de fichiers).
func describePaths(paths []string) ([]Item, error) {
	var out []Item
	for _, p := range paths {
		p = filepath.Clean(p)
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("« %s » est introuvable", p)
		}
		it := Item{Path: p, Name: displayName(p), IsDir: info.IsDir()}
		if err := walkFiles(p, func(_, _ string, fi fs.FileInfo) {
			it.Count++
			it.Size += fi.Size()
		}); err != nil {
			return nil, fmt.Errorf("lecture impossible de « %s » : %v", p, err)
		}
		out = append(out, it)
	}
	return out, nil
}

// collectFiles transforme les éléments choisis en liste de fichiers à envoyer.
func collectFiles(paths []string) ([]FileEntry, error) {
	var out []FileEntry
	for _, p := range paths {
		p = filepath.Clean(p)
		if err := walkFiles(p, func(abs, rel string, fi fs.FileInfo) {
			out = append(out, FileEntry{Path: rel, Size: fi.Size(), MTime: fi.ModTime().UnixMilli(), abs: abs})
		}); err != nil {
			return nil, fmt.Errorf("lecture impossible de « %s » : %v", p, err)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("aucun fichier à envoyer (dossier vide ?)")
	}
	return out, nil
}
