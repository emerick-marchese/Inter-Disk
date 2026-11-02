//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32         = syscall.NewLazyDLL("user32.dll")
	procMessageBox = user32.NewProc("MessageBoxW")
	procBeep       = user32.NewProc("MessageBeep")
)

// showErrorBox affiche une boîte d'erreur Windows (avant l'ouverture de la fenêtre).
func showErrorBox(title, msg string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	const mbIconError = 0x10
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError)
}

// beep joue le son de notification de Windows.
func beep() {
	const mbIconAsterisk = 0x40
	procBeep.Call(mbIconAsterisk)
}
