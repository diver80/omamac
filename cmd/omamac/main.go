package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	updaternative "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"

	"omamac/internal/omamac"
)

func main() {
	configPath := flag.String("config", omamac.DefaultConfigPath(), "path of the OmaMac configuration file (~/.config/omamac/config.json)")
	startWalOnly := flag.Bool("wal", false, "open the launcher right at the start")
	checkFile := flag.String("check", "", "check a panel JSON file (the same as: omamac check <file>)")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), "OmaMac – menu bar panels, launcher and agents.\n\nFlags:\n")
		flag.PrintDefaults()
		fmt.Fprint(flag.CommandLine.Output(), "\nCommands for agents and scripts: omamac help\n")
	}
	flag.Parse()

	// Commands for coding agents in the panels folder (AGENTS.md): check, vars, log, help.
	if *checkFile != "" {
		os.Exit(omamac.RunCLI(*configPath, []string{"check", *checkFile}, os.Stdout, os.Stderr))
	}
	// Linux: `omamac open …` (Waybar's modules, keys) goes to the running app, or starts it.
	openCmd := runtime.GOOS == "linux" && flag.Arg(0) == "open"
	if flag.NArg() > 0 && !openCmd {
		os.Exit(omamac.RunCLI(*configPath, flag.Args(), os.Stdout, os.Stderr))
	}

	mygo.App.SetName("OmaMac")
	if runtime.GOOS == "linux" && !mygo.App.RequestSingleInstanceLock() {
		return // the running app got the command line
	}
	if mygo.App.Version() == "" {
		mygo.App.SetVersion("dev")
	}

	// Signed auto-updates from GitHub releases (diver80/omamac, see mygo.json "updates").
	// In development builds the updater is disabled and stays silent.
	mygo.Use(updaternative.Plugin)

	appState, err := omamac.NewApp(*configPath)
	if err != nil {
		log.Fatalf("Failed to initialize OmaMac: %v", err)
	}

	omamac.InitLanguage(appState.Config.Language)

	// .omamac files opened from Finder (see fileAssociations in mygo.json) go to the import
	mygo.App.OnOpenFile(func(path string) { appState.OpenImportFile(path) })
	mygo.App.OnSecondInstance(func(args []string, _ string) { appState.HandleOpen(args) })

	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
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
		appState.SyncLanguage()
		studioWin := mygo.NewWindow(mygo.WindowOptions{
			Title:     "OmaMac",
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
		if openCmd {
			appState.HandleOpen(flag.Args())
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
