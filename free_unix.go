//go:build linux || darwin

package main

import "syscall"

// freeSpace renvoie l'espace disque disponible en octets (-1 si inconnu).
func freeSpace(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
