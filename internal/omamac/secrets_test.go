package omamac

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// memSecrets stands in for the Keychain in tests: they never touch the user's Keychain.
type memSecrets struct {
	items map[string][]byte
	fail  error
}

func (m *memSecrets) Get(key string) ([]byte, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	v, ok := m.items[key]
	if !ok {
		return nil, mygo.ErrSecretNotFound
	}
	return v, nil
}

func (m *memSecrets) Set(key string, value []byte) error {
	if m.fail != nil {
		return m.fail
	}
	m.items[key] = append([]byte(nil), value...)
	return nil
}

func (m *memSecrets) Delete(key string) error {
	delete(m.items, key)
	return nil
}

func useMemSecrets(t *testing.T) *memSecrets {
	m := &memSecrets{items: map[string][]byte{}}
	old := secretStore
	secretStore = &secretBlob{backend: m}
	if t != nil {
		t.Cleanup(func() { secretStore = old })
	}
	return m
}

func TestMain(m *testing.M) {
	useMemSecrets(nil) // the default for every test
	setLogPath("")     // tests never write to the user's log
	legacyVaultItems = &memSecrets{items: map[string][]byte{}}
	readClaudeLogin = func(context.Context) (*claudeLogin, error) { return nil, errNoClaudeLogin } // never the real keychain
	os.Exit(m.Run())
}

// Tokens and vault values from the files of earlier versions move into ONE Keychain item.
func TestSecretsMoveIntoOneKeychainItem(t *testing.T) {
	mem := useMemSecrets(t)
	dir := t.TempDir()
	jiraPath := filepath.Join(dir, "jira_instances.json")
	_ = os.WriteFile(jiraPath, []byte(`[{"id":"i1","name":"A","base_url":"https://a.example.com","email":"me@example.com","api_token":"tok-secret","enabled":true}]`), 0o600)
	vaultPath := filepath.Join(dir, "vault.json")
	_ = os.WriteFile(vaultPath, []byte(`{"entries":[{"name":"MY_KEY"}],"values":{"MY_KEY":"val-secret"}}`), 0o600)

	j := &JiraModuleState{ConfigPath: jiraPath}
	j.LoadOrImport()
	v, err := LoadVault(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if j.Instances[0].APIToken != "tok-secret" || v.Get("MY_KEY") != "val-secret" {
		t.Fatal("values lost")
	}
	for _, p := range []string{jiraPath, vaultPath} {
		raw, _ := os.ReadFile(p)
		if strings.Contains(string(raw), "-secret") {
			t.Fatalf("%s still holds a secret:\n%s", filepath.Base(p), raw)
		}
	}
	if len(mem.items) != 1 || !strings.Contains(string(mem.items[secretsKey]), `"jira:i1":"tok-secret"`) ||
		!strings.Contains(string(mem.items[secretsKey]), `"vault:MY_KEY":"val-secret"`) {
		t.Fatalf("keychain: %v", mem.items)
	}

	// A fresh start reads them back from the Keychain.
	secretStore = &secretBlob{backend: mem}
	j2 := &JiraModuleState{ConfigPath: jiraPath}
	j2.LoadOrImport()
	v2, _ := LoadVault(vaultPath)
	if j2.Instances[0].APIToken != "tok-secret" || v2.Get("MY_KEY") != "val-secret" {
		t.Fatal("not read back from the Keychain")
	}
	// Deleting removes it from the item too.
	_ = v2.Delete("MY_KEY")
	if strings.Contains(string(mem.items[secretsKey]), "MY_KEY") {
		t.Fatal("deleted value left in the Keychain")
	}
}

// Without a usable Keychain the values stay in the 0600 files, as before, and nothing is lost.
func TestSecretsFallBackToFile(t *testing.T) {
	mem := useMemSecrets(t)
	mem.fail = errors.ErrUnsupported
	dir := t.TempDir()
	v, _ := LoadVault(filepath.Join(dir, "vault.json"))
	if err := v.Set("TOKEN", "abc", ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "vault.json"))
	if !strings.Contains(string(raw), `"TOKEN": "abc"`) || v.StorageErr == "" {
		t.Fatalf("fallback file:\n%s (err %q)", raw, v.StorageErr)
	}
	j := &JiraModuleState{ConfigPath: filepath.Join(dir, "jira.json"), Instances: []JiraInstanceConfig{{ID: "i1", APIToken: "tok"}}}
	_ = j.Save()
	raw, _ = os.ReadFile(j.ConfigPath)
	if !strings.Contains(string(raw), `"api_token": "tok"`) || j.StorageErr == "" {
		t.Fatalf("fallback file:\n%s", raw)
	}
}
