package omamac

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif" // decoders for convertImage
	"image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// What differs between macOS, where OmaMac comes from, and Linux (Omarchy): the shell
// scripts run in, the terminals agents open in, converting images and the clipboard.
// macOS keeps exactly what it had; the rest is for the other systems.

// goos is the system OmaMac runs on; tests set it to cover the others.
var goos = runtime.GOOS

// scriptShell runs panel commands, action buttons and the agents' terminal scripts:
// zsh on macOS, as ever; elsewhere zsh too when it is there (panels are written for
// it), else bash (Omarchy's), else sh.
var scriptShell = sync.OnceValue(func() string {
	return pickShell(goos, isExecutable)
})

func pickShell(system string, exists func(string) bool) string {
	if system == "darwin" {
		return "/bin/zsh"
	}
	for _, sh := range []string{"/bin/zsh", "/usr/bin/zsh", "/bin/bash", "/usr/bin/bash"} {
		if exists(sh) {
			return sh
		}
	}
	return "/bin/sh"
}

func isExecutable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// copyText puts text on the clipboard.
func copyText(text string) {
	if goos == "darwin" {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return
	}
	mygo.Clipboard.WriteText(text)
}

// convertImage makes a JPEG of at most maxSide pixels of an image: with macOS's sips,
// which reads HEIC too, else in Go (PNG, JPEG, GIF, WebP, TIFF, BMP).
func convertImage(path, out string, maxSide int) error {
	if goos == "darwin" {
		cmd := exec.Command("/usr/bin/sips", "-s", "format", "jpeg", "-s", "formatOptions", "82",
			"-Z", fmt.Sprint(maxSide), path, "--out", out)
		if msg, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v %s", err, strings.TrimSpace(string(msg)))
		}
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	jpg, err := scaleToJPEG(data, maxSide)
	if err != nil {
		return err
	}
	return os.WriteFile(out, jpg, 0o600)
}

// scaleToJPEG decodes an image and encodes it as a JPEG that fits maxSide.
func scaleToJPEG(data []byte, maxSide int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxSide || h > maxSide {
		if w >= h {
			w, h = maxSide, max(1, h*maxSide/w)
		} else {
			w, h = max(1, w*maxSide/h), maxSide
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src) // JPEG has no transparency
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Terminals agents run in on Linux, by the name shown in the settings: "Standard" is
// the system's (xdg-terminal-exec, which Omarchy sets up), the others by their command.
var linuxTerminals = []string{"Standard", "ghostty", "alacritty", "kitty", "foot", "wezterm", "gnome-terminal", "konsole", "xterm"}

// linuxTerminalCommand is the command that opens term running script.
func linuxTerminalCommand(term, script string) *exec.Cmd {
	switch term {
	case "Standard":
		return exec.Command("xdg-terminal-exec", script)
	case "kitty", "foot":
		return exec.Command(term, script)
	case "wezterm":
		return exec.Command(term, "start", "--", script)
	case "gnome-terminal":
		return exec.Command(term, "--", script)
	}
	return exec.Command(term, "-e", script) // ghostty, alacritty, konsole, xterm
}

// linuxTerminalProgram is what has to be on the PATH for term.
func linuxTerminalProgram(term string) string {
	if term == "Standard" {
		return "xdg-terminal-exec"
	}
	return term
}

// startDetached starts cmd without waiting for it (a terminal runs until its window
// closes) and reaps it when it ends.
func startDetached(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// homePath is ~/rel.
func homePath(rel ...string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(append([]string{home}, rel...)...)
}
