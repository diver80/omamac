package omamac

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const keychainService = "de.diver80.omamac.vault"

// legacyKeychainServices are earlier Keychain service names; secrets found there are moved
// to keychainService on load (the app identifier changed from com.avono.omamac to de.diver80.omamac).
var legacyKeychainServices = []string{"com.avono.omamac.vault"}

// CredentialMeta describes a secret stored in the central OmaMac Vault.
// The secret Value itself is stored in macOS Keychain (via mygo.Secrets) + 0600 local vault file,
// and is NEVER sent to AI prompts—only Name and Description are exposed to the AI generator.
type CredentialMeta struct {
	Name        string `json:"name"`        // Env var name, e.g. "JIRA_API_TOKEN", "IDEALO_MAX_PRICE"
	Description string `json:"description"` // Human/AI description of what this credential is for
	UpdatedAt   string `json:"updated_at"`
}

type vaultStoreFile struct {
	Entries []CredentialMeta  `json:"entries"`
	Values  map[string]string `json:"values,omitempty"` // 0600 fallback when running in headless test without Keychain unlock
}

// Vault manages centralized credentials for AI agents and status panels.
// Panel refreshes and AI calls read it from goroutines while Settings edits it on the
// UI thread, so mu guards Entries and values.
type Vault struct {
	FilePath string
	Entries  []CredentialMeta
	values   map[string]string
	mu       sync.RWMutex
}

// DefaultVaultPath returns ~/.config/omamac/vault.json.
func DefaultVaultPath() string {
	return filepath.Join(DefaultConfigDir(), "vault.json")
}

// LoadVault loads the credential metadata and resolves values from macOS Keychain (or 0600 fallback).
func LoadVault(path string) (*Vault, error) {
	if path == "" {
		path = DefaultVaultPath()
	}
	v := &Vault{
		FilePath: path,
		values:   make(map[string]string),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Seed helpful example metadata keys (without secret values) so user sees how it works
			v.Entries = []CredentialMeta{
				{Name: "MYDEALZ_KEYWORD", Description: "Filter keyword for mydealz 3D printer deals (e.g. Bambu, Prusa, Creality)", UpdatedAt: time.Now().Format("2006-01-02 15:04")},
				{Name: "IDEALO_TARGET_PRICE", Description: "Target price threshold in EUR for Idealo price alert status", UpdatedAt: time.Now().Format("2006-01-02 15:04")},
			}
			v.values["MYDEALZ_KEYWORD"] = "Bambu"
			v.values["IDEALO_TARGET_PRICE"] = "499"
			_ = v.Save()
			return v, nil
		}
		return v, err
	}

	var sf vaultStoreFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return v, err
	}
	v.Entries = sf.Entries
	if sf.Values != nil {
		for k, val := range sf.Values {
			v.values[k] = val
		}
	}

	// Attempt to read from macOS Keychain first via mygo.Secrets
	sec := mygo.Secrets.Service(keychainService)
	for _, e := range v.Entries {
		if raw, err := sec.Get(e.Name); err == nil && len(raw) > 0 {
			v.values[e.Name] = string(raw)
			continue
		}
		for _, legacy := range legacyKeychainServices {
			old := mygo.Secrets.Service(legacy)
			raw, err := old.Get(e.Name)
			if err != nil || len(raw) == 0 {
				continue
			}
			v.values[e.Name] = string(raw)
			if sec.Set(e.Name, raw) == nil {
				_ = old.Delete(e.Name)
			}
			break
		}
	}
	return v, nil
}

// Save writes the vault metadata (and 0600 local fallback) to disk.
func (v *Vault) Save() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.saveLocked()
}

func (v *Vault) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(v.FilePath), 0o700); err != nil {
		return err
	}
	sort.Slice(v.Entries, func(i, j int) bool {
		return v.Entries[i].Name < v.Entries[j].Name
	})
	sf := vaultStoreFile{
		Entries: v.Entries,
		Values:  v.values,
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(v.FilePath, data, 0o600)
}

// Set stores or updates a credential in macOS Keychain and the Vault index.
func (v *Vault) Set(name, value, description string) error {
	envKey := NormalizeEnvVarName(name)
	if envKey == "" {
		return fmt.Errorf("invalid credential variable name %q", name)
	}
	now := time.Now().Format("2006-01-02 15:04")
	v.mu.Lock()
	defer v.mu.Unlock()
	found := false
	for i := range v.Entries {
		if v.Entries[i].Name == envKey {
			if description != "" {
				v.Entries[i].Description = description
			}
			v.Entries[i].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		v.Entries = append(v.Entries, CredentialMeta{
			Name:        envKey,
			Description: description,
			UpdatedAt:   now,
		})
	}
	v.values[envKey] = value

	// Store in macOS Keychain via mygo.Secrets (best-effort)
	_ = mygo.Secrets.Service(keychainService).Set(envKey, []byte(value))
	return v.saveLocked()
}

// Delete removes a credential from macOS Keychain and the Vault index.
func (v *Vault) Delete(name string) error {
	envKey := NormalizeEnvVarName(name)
	v.mu.Lock()
	defer v.mu.Unlock()
	var next []CredentialMeta
	for _, e := range v.Entries {
		if e.Name != envKey {
			next = append(next, e)
		}
	}
	v.Entries = next
	delete(v.values, envKey)
	_ = mygo.Secrets.Service(keychainService).Delete(envKey)
	return v.saveLocked()
}

// Get returns the secret value for name.
func (v *Vault) Get(name string) string {
	if v == nil {
		return ""
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.values[NormalizeEnvVarName(name)]
}

// EnvSlice returns "KEY=VALUE" pairs to inject into panel shell commands at runtime.
func (v *Vault) EnvSlice() []string {
	if v == nil {
		return nil
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	var out []string
	for _, e := range v.Entries {
		if val, ok := v.values[e.Name]; ok {
			out = append(out, e.Name+"="+val)
		}
	}
	return out
}

// AIPromptCatalog returns a safe description of available environment variable names (WITHOUT secret values!)
// so the AI agent knows which $VAR_NAME variables it can use in panel commands.
func (v *Vault) AIPromptCatalog() string {
	if v == nil {
		return "None configured."
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if len(v.Entries) == 0 {
		return "None configured."
	}
	var b strings.Builder
	for _, e := range v.Entries {
		b.WriteString(fmt.Sprintf("- $%s : %s\n", e.Name, e.Description))
	}
	return b.String()
}

// NormalizeEnvVarName converts any user string into a valid UPPER_SNAKE_CASE environment variable name.
func NormalizeEnvVarName(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "$")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else if r == '-' || r == ' ' || r == '.' {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
