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
	err = wails.Run(&options.App{
		Title:            appName,
		Width:            1120,
		Height:           820,
		MinWidth:         760,
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 243, G: 245, B: 248, A: 255},
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

// beforeClose demande confirmation si un transfert est en cours.
func (a *App) beforeClose(ctx context.Context) bool {
	if !a.xfers.HasActive() {
		return false
	}
	res, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{
		Type:    wruntime.QuestionDialog,
		Title:   "Quitter " + appName + " ?",
		Message: "Un transfert est en cours : il sera interrompu.\n\nQuitter quand même ?",
	})
	if err != nil {
		return false
	}
	return res != "Yes" && res != "Oui" // true = on garde la fenêtre ouverte
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
