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

// DiscoverMacApps scans /Applications and /System/Applications/Utilities for quick-launching from the Wal-Panel.
func DiscoverMacApps() []QuickCommand {
	dirs := []string{
		"/Applications",
		"/System/Applications",
		"/System/Applications/Utilities",
	}
	seen := make(map[string]bool)
	var apps []QuickCommand

	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".app") {
				continue
			}
			appName := strings.TrimSuffix(name, ".app")
			if seen[appName] {
				continue
			}
			seen[appName] = true
			fullPath := filepath.Join(d, name)
			apps = append(apps, QuickCommand{
				ID:       "app-" + Slugify(appName),
				Title:    appName,
				Subtitle: fullPath,
				Category: "App",
				Icon:     "🚀",
				Run: func() error {
					return exec.Command("open", "-a", fullPath).Start()
				},
			})
		}
	}

	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Title < apps[j].Title
	})
	return apps
}
