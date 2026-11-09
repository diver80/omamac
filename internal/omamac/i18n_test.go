package omamac

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var verbRE = regexp.MustCompile(`%[-+# 0]*[0-9]*(\.[0-9]+)?[a-zA-Z]`)

// Every English text keeps the format verbs of its German key, in order, and every key
// is still a text of the code (a renamed German text would silently stay German).
func TestEnglishMessages(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	var code strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "i18n_en.go" {
			continue
		}
		raw, _ := os.ReadFile(f)
		code.Write(raw)
	}
	src := code.String()
	for de, en := range enMessages {
		if a, b := strings.Join(verbRE.FindAllString(de, -1), " "), strings.Join(verbRE.FindAllString(en, -1), " "); a != b {
			t.Errorf("verbs differ: %q (%s) → %q (%s)", de, a, en, b)
		}
		if !strings.Contains(src, strconv.Quote(de)) && !strings.Contains(src, strconv.QuoteToGraphic(de)) {
			t.Errorf("unused key %q", de)
		}
	}
}

func TestLanguageSwitch(t *testing.T) {
	t.Cleanup(func() { SetLanguage("de") })
	SetLanguage("en-GB")
	if T("Einstellungen") != "Settings" || T("kein Eintrag dafür") != "kein Eintrag dafür" || Language() != "en" {
		t.Fatal("English")
	}
	if !isStep("Request sent to Claude (120 characters)", "Anfrage an %s gesendet (%d Zeichen)") {
		t.Fatal("progress steps are recognized in the language shown")
	}
	SetLanguage("de-CH")
	if T("Einstellungen") != "Einstellungen" || Language() != "de" {
		t.Fatal("German")
	}
}
