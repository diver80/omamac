package omamac

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AntigravityStatus describes the detected local Antigravity OAuth login state.
type AntigravityStatus struct {
	Available     bool
	CLIInstalled  bool
	CLIPath       string
	TokenFilePath string
	AuthMethod    string
	AccountEmail  string
	AccountName   string
	TokenExpiry   string
	HasRefresh    bool
	StatusSummary string
}

type agyTokenFile struct {
	AuthMethod string `json:"auth_method"`
	IDToken    string `json:"id_token"`
	Token      struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Expiry       string `json:"expiry"`
	} `json:"token"`
}

type jwtClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Sub   string `json:"sub"`
}

type googleAccountsFile struct {
	Active *string  `json:"active"`
	Old    []string `json:"old"`
}

// InspectAntigravityAuth checks whether a valid local Antigravity OAuth session and CLI exist.
func InspectAntigravityAuth(cfg *Config) AntigravityStatus {
	home, _ := os.UserHomeDir()
	cliPath := cfg.AntigravityCLIPath
	if cliPath == "" {
		cliPath = filepath.Join(home, ".local", "bin", "agy")
	}
	cliOK := false
	if st, err := os.Stat(cliPath); err == nil && !st.IsDir() {
		cliOK = true
	} else if resolved, err := exec.LookPath(cliPath); err == nil {
		cliPath = resolved
		cliOK = true
	} else if resolved, err := exec.LookPath("agy"); err == nil {
		cliPath = resolved
		cliOK = true
	}

	tokenPath := cfg.AntigravityTokenPath
	if tokenPath == "" {
		tokenPath = filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	}

	status := AntigravityStatus{
		CLIInstalled:  cliOK,
		CLIPath:       cliPath,
		TokenFilePath: tokenPath,
	}

	raw, err := os.ReadFile(tokenPath)
	if err != nil {
		status.StatusSummary = T("Nicht angemeldet – kein Antigravity-Token gefunden")
		return status
	}

	var tf agyTokenFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		status.StatusSummary = T("Antigravity-Token ist ungültig")
		return status
	}

	status.AuthMethod = tf.AuthMethod
	status.TokenExpiry = tf.Token.Expiry
	status.HasRefresh = tf.Token.RefreshToken != ""

	// Extract email & display name from JWT id_token payload (if present)
	if parts := strings.Split(tf.IDToken, "."); len(parts) >= 2 {
		if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			var claims jwtClaims
			if json.Unmarshal(payload, &claims) == nil {
				status.AccountEmail = claims.Email
				status.AccountName = claims.Name
			}
		}
	}

	// Fallback to ~/.gemini/google_accounts.json if id_token didn't include email
	if status.AccountEmail == "" {
		accPath := filepath.Join(home, ".gemini", "google_accounts.json")
		if accRaw, err := os.ReadFile(accPath); err == nil {
			var ga googleAccountsFile
			if json.Unmarshal(accRaw, &ga) == nil {
				if ga.Active != nil && *ga.Active != "" {
					status.AccountEmail = *ga.Active
				} else if len(ga.Old) > 0 {
					status.AccountEmail = ga.Old[0]
				}
			}
		}
	}

	if (tf.Token.AccessToken != "" || tf.Token.RefreshToken != "") && cliOK {
		status.Available = true
		acct := status.AccountEmail
		if acct == "" {
			acct = T("Antigravity-Konto")
		}
		if status.AccountName != "" {
			acct = fmt.Sprintf("%s (%s)", status.AccountName, acct)
		}
		status.StatusSummary = T("Angemeldet als ") + acct
		if tf.Token.Expiry != "" {
			if t, err := time.Parse(time.RFC3339, tf.Token.Expiry); err == nil {
				status.StatusSummary += T(" · Token vom ") + t.Format(T("02.01.2006 15:04"))
			}
		}
	} else if !cliOK {
		status.StatusSummary = T("Token vorhanden, aber das Programm „agy“ fehlt")
	} else {
		status.StatusSummary = T("Antigravity-Token unvollständig – bitte neu anmelden")
	}

	return status
}

// TriggerAntigravityLogin opens macOS Terminal (on Linux the chosen terminal) to run
// interactive `agy` OAuth login if re-auth is needed.
func TriggerAntigravityLogin(cfg *Config) error {
	st := InspectAntigravityAuth(cfg)
	bin := st.CLIPath
	if bin == "" {
		bin = "agy"
	}
	if goos != "darwin" {
		if p, err := exec.LookPath(bin); err == nil {
			bin = p
		}
		return startDetached(terminalCommand(chooseTerminal(cfg.Terminal), bin))
	}
	script := fmt.Sprintf(`tell application "Terminal"
	activate
	do script "%s"
end tell`, strings.ReplaceAll(bin, `"`, `\"`))
	return exec.Command("osascript", "-e", script).Start()
}
