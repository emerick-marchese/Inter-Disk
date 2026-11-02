package main

import (
	"context"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"io/fs"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed ui
var uiFiles embed.FS

// runDesktop ouvre la fenêtre de l'application et ne rend la main qu'à sa fermeture.
func runDesktop(a *App, cfgDir string) {
	ui, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		fatal("Interface introuvable : %v", err)
	}
	// Une seule fenêtre Inter Disk par utilisateur : relancer l'application
	// remet simplement la fenêtre existante au premier plan.
	lockID := "inter-disk-3f9c2a71"
	if cfgDir != "" {
		h := sha1.Sum([]byte(cfgDir))
		lockID += "-" + hex.EncodeToString(h[:4])
	}
	bg := &options.RGBA{R: 243, G: 245, B: 250, A: 255}
	if systemDark() {
		bg = &options.RGBA{R: 10, G: 12, B: 17, A: 255}
	}
	err = wails.Run(&options.App{
		Title:            appName,
		Width:            1180,
		Height:           800,
		MinWidth:         900,
		MinHeight:        600,
		Frameless:        true, // barre de titre et bordures dessinées par l'application
		BackgroundColour: bg,
		AssetServer:      &assetserver.Options{Assets: ui},
		OnStartup:        a.startup,
		OnBeforeClose:    a.beforeClose,
		DragAndDrop:      &options.DragAndDrop{EnableFileDrop: true},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               lockID,
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { a.showWindow(false) },
		},
		Bind:    []interface{}{a},
		Windows: &windows.Options{Theme: windows.SystemDefault},
		Linux:   &linux.Options{ProgramName: appName},
	})
	if err != nil {
		fatal("Impossible d'ouvrir la fenêtre d'%s : %v", appName, err)
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Envoie l'état (PC détectés, transferts…) à l'interface deux fois par seconde.
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				wruntime.EventsEmit(ctx, "state", a.State())
			}
		}
	}()
}

// beforeClose : si un transfert est en cours, la fenêtre reste ouverte et
// l'interface demande confirmation (puis appelle ForceQuit).
func (a *App) beforeClose(ctx context.Context) bool {
	if a.forceQuit.Load() || !a.xfers.HasActive() {
		return false
	}
	a.showWindow(false)
	wruntime.EventsEmit(ctx, "confirm-close")
	return true // true = on garde la fenêtre ouverte
}

// ForceQuit ferme l'application même si un transfert est en cours.
func (a *App) ForceQuit() {
	a.forceQuit.Store(true)
	if a.ctx != nil {
		wruntime.Quit(a.ctx)
	}
}

// showWindow ramène la fenêtre au premier plan (demande entrante, relance…).
func (a *App) showWindow(alert bool) {
	ctx := a.ctx
	if ctx == nil {
		return
	}
	wruntime.WindowUnminimise(ctx)
	wruntime.WindowShow(ctx)
	wruntime.WindowSetAlwaysOnTop(ctx, true)
	go func() {
		time.Sleep(1200 * time.Millisecond)
		wruntime.WindowSetAlwaysOnTop(ctx, false)
	}()
	if alert {
		beep()
	}
}

// notifyIncoming est appelé quand un autre PC propose des fichiers.
func (a *App) notifyIncoming() {
	a.showWindow(true)
}
