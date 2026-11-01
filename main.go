package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "Link Alloy",
		Description: "Independent multi-network Windows client",
		Services: []application.Service{
			application.NewService(&NetworkService{}),
		},
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac:    application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
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

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
