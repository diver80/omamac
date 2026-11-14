package omamac

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// QuickCommand represents a keyboard-driven action in the Wal-Panel (app launch, window tile, or system command).
type QuickCommand struct {
	ID       string
	Title    string
	Subtitle string
	Category string // "Window Tile", "App", "OmaMac"
	Icon     string
	Run      func() error
}

// TileLayout defines supported macOS window tiling presets.
type TileLayout string

const (
	TileLeftHalf   TileLayout = "left-half"
	TileRightHalf  TileLayout = "right-half"
	TileMaximize   TileLayout = "maximize"
	TileCenter80   TileLayout = "center-80"
	TileTopHalf    TileLayout = "top-half"
	TileBottomHalf TileLayout = "bottom-half"
)

// ApplyWindowTile tiles the frontmost macOS window using System Events / Finder bounds.
func ApplyWindowTile(layout TileLayout, workAreaX, workAreaY, workAreaW, workAreaH int) error {
	if workAreaW <= 0 {
		workAreaW = 1728
	}
	if workAreaH <= 0 {
		workAreaH = 1050
	}
	x, y, w, h := workAreaX, workAreaY, workAreaW, workAreaH
	switch layout {
	case TileLeftHalf:
		w = workAreaW / 2
	case TileRightHalf:
		x = workAreaX + workAreaW/2
		w = workAreaW / 2
	case TileTopHalf:
		h = workAreaH / 2
	case TileBottomHalf:
		y = workAreaY + workAreaH/2
		h = workAreaH / 2
	case TileCenter80:
		w = int(float64(workAreaW) * 0.80)
		h = int(float64(workAreaH) * 0.82)
		x = workAreaX + (workAreaW-w)/2
		y = workAreaY + (workAreaH-h)/2
	case TileMaximize:
		// uses full workArea
	}

	script := fmt.Sprintf(`tell application "System Events"
	set frontApp to first application process whose frontmost is true
	tell frontApp
		if (count of windows) > 0 then
			set position of front window to {%d, %d}
			set size of front window to {%d, %d}
		end if
	end tell
end tell`, x, y, w, h)
	return exec.Command("osascript", "-e", script).Start()
}

// DiscoverMacApps scans /Applications, ~/Applications, /System/Applications, Utilities, and Finder for quick-launching from the Wal-Panel.
func DiscoverMacApps() []QuickCommand {
	home, _ := os.UserHomeDir()
	dirs := []string{
		"/Applications",
		filepath.Join(home, "Applications"),
		"/System/Applications",
		"/System/Applications/Utilities",
	}
	return discoverMacAppsFrom(dirs, "/System/Library/CoreServices/Finder.app")
}

func discoverMacAppsFrom(dirs []string, extraApps ...string) []QuickCommand {
	seen := make(map[string]bool)
	var apps []QuickCommand

	addApp := func(fullPath string) {
		name := filepath.Base(fullPath)
		if !strings.HasSuffix(name, ".app") {
			return
		}
		appName := strings.TrimSuffix(name, ".app")
		if seen[appName] {
			return
		}
		seen[appName] = true
		p := fullPath
		apps = append(apps, QuickCommand{
			ID:       "app-" + Slugify(appName),
			Title:    appName,
			Subtitle: p,
			Category: "App",
			Icon:     "🚀",
			Run: func() error {
				return exec.Command("open", "-a", p).Start()
			},
		})
	}

	for _, d := range dirs {
		if d == "" {
			continue
		}
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".app") {
				addApp(filepath.Join(d, e.Name()))
			}
		}
	}
	for _, p := range extraApps {
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			addApp(p)
		}
	}

	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Title < apps[j].Title
	})
	return apps
}
