package omamac

import (
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
)

// OmaMac's texts are written in German in the code; T says them in the language the app
// shows. English comes from enMessages (i18n_en.go), keyed by the German text, so a text
// without a translation still shows – in German. Go format verbs stay in the texts:
// fmt.Sprintf(T("%d Panels erstellt"), n).

// UI languages, in the order of the settings' control after "System".
var uiLanguages = []string{"de", "en"}

var english atomic.Bool // false: German, the language of the code (and of the tests)

// T returns s in the language the app shows.
func T(s string) string {
	if english.Load() {
		if v, ok := enMessages[s]; ok {
			return v
		}
	}
	return s
}

// SetLanguage shows the UI in tag ("de…" is German, everything else English).
func SetLanguage(tag string) {
	english.Store(!strings.HasPrefix(strings.ToLower(tag), "de"))
}

// Language is the language T answers in: "de" or "en".
func Language() string {
	if english.Load() {
		return "en"
	}
	return "de"
}

// InitLanguage makes OmaMac follow the language macOS chose for it among German and
// English – English for everyone who reads neither – or the one chosen in the settings
// ("de", "en"; "" follows macOS). The desktop app calls it before it builds windows.
func InitLanguage(chosen string) {
	mygo.App.SetLanguages("en", "de") // the first is what other languages fall back to
	mygo.App.OnLocaleChanged(func(tag string) { SetLanguage(tag) })
	if chosen != "" {
		SetLanguage(chosen)
		mygo.App.SetLocale(chosen)
		return
	}
	SetLanguage(mygo.App.Locale())
}

// SyncLanguage takes the language over once the app is ready (the system's choice
// among the app's languages is only settled then).
func (a *App) SyncLanguage() {
	if a.Config.Language == "" {
		SetLanguage(mygo.App.Locale())
	}
}

// applyLanguage shows the language chosen in the settings at once.
func (a *App) applyLanguage() {
	mygo.App.SetLocale(a.Config.Language) // "" follows macOS again; OnLocaleChanged sets ours
	if a.Config.Language != "" {
		SetLanguage(a.Config.Language)
	}
	a.syncAllPanelRuntimes()
	a.refreshAllNow() // status texts of the built-in modules are written while refreshing
}
