package main

import (
	"embed"
	"log"
	"os"

	"github.com/sayanpramanik2012/Link-Alloy/internal/engine"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if parentPID, watch, err := engine.IsRecoveryWatchdogArgument(os.Args); watch {
		if err != nil {
			log.Print(err)
			return
		}
		if err := engine.RunRecoveryWatchdog(parentPID); err != nil {
			log.Print(err)
		}
		return
	}
	release, err := engine.AcquireProcessLock()
	if err != nil {
		log.Printf("Link Alloy was not started: %v", err)
		return
	}
	defer release()
	networkService := NewNetworkService()
	stopWatchdog, watchdogErr := engine.StartRecoveryWatchdog()
	if watchdogErr != nil {
		log.Printf("crash recovery watcher is unavailable: %v", watchdogErr)
	}
	app := application.New(application.Options{
		Name:        "Link Alloy",
		Description: "Independent multi-network Windows client",
		Services: []application.Service{
			application.NewService(networkService),
		},
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Link Alloy",
		Width:            1220,
		Height:           860,
		MinWidth:         960,
		MinHeight:        720,
		BackgroundColour: application.NewRGB(10, 14, 20),
		URL:              "/",
	})

	runErr := app.Run()
	cleanupErr := networkService.Shutdown()
	if stopWatchdog != nil {
		stopWatchdog()
	}
	if cleanupErr != nil {
		log.Printf("tunnel cleanup failed during application exit: %v", cleanupErr)
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}
