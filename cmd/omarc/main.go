package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"omarc/internal/omamac"
)

func main() {
	configPath := flag.String("config", omamac.DefaultConfigPath(), "Path to OmaMac configuration file (~/.config/omamac/config.json)")
	startWalOnly := flag.Bool("wal", false, "Open the floating Wal-Panel immediately on startup")
	checkFile := flag.String("check", "", "Run owl-check / omamac-check on a panel JSON file: validates, executes live data refresh, and renders a PNG preview")
	flag.Parse()

	if *checkFile != "" {
		runOmaMacCheck(*configPath, *checkFile)
		return
	}

	mygo.App.SetName("OmaMac")
	if mygo.App.Version() == "" {
		mygo.App.SetVersion("1.0.0")
	}

	appState, err := omamac.NewApp(*configPath)
	if err != nil {
		log.Fatalf("Failed to initialize OmaMac: %v", err)
	}

	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	}))

	mygo.App.WhenReady(func() {
		studioWin := mygo.NewWindow(mygo.WindowOptions{
			Title:     "OmaMac – Omarchy Wal-Panel, 2-Prompt Status Panel Studio & Window Manager",
			Width:     1320,
			Height:    840,
			MinWidth:  980,
			MinHeight: 640,
			StateKey:  "omamac-studio-main",
			Content:   ui.View(appState.RenderStudioView),
		})
		appState.StartDesktopServices(studioWin)
		if *startWalOnly {
			appState.ToggleWalPanel()
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// runOmaMacCheck implements the macOS equivalent of OmaLauncher's `owl-check <file>`:
// It loads the panel JSON, verifies Homebrew dependencies, executes the live data command,
// renders a PNG preview of the widget/panel, and prints the PNG path for visual inspection.
func runOmaMacCheck(configPath, panelFile string) {
	raw, err := os.ReadFile(panelFile)
	if err != nil {
		log.Fatalf("omamac-check error reading %s: %v", panelFile, err)
	}
	var spec omamac.PanelSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		log.Fatalf("omamac-check invalid JSON in %s: %v", panelFile, err)
	}

	appState, err := omamac.NewApp(configPath)
	if err != nil {
		log.Fatalf("omamac-check app init failed: %v", err)
	}

	var env []string
	if appState.Vault != nil {
		env = appState.Vault.EnvSlice()
	}
	notes, checkErr := spec.CheckPanelSpec(context.Background(), env...)
	for _, n := range notes {
		fmt.Println("•", n)
	}
	if checkErr != nil {
		log.Fatalf("omamac-check FAILED: %v", checkErr)
	}

	outDir := filepath.Join(omamac.DefaultConfigDir(), "previews")
	_ = os.MkdirAll(outDir, 0o755)
	pngPath := filepath.Join(outDir, spec.ID+"-preview.png")

	img := ui.Render(appState.RenderStudioView, 1280, 800, 2.0)
	f, err := os.Create(pngPath)
	if err != nil {
		log.Fatalf("omamac-check create PNG failed: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatalf("omamac-check encode PNG failed: %v", err)
	}
	fmt.Printf("LIVE_PREVIEW_PNG=%s\n", pngPath)
}
