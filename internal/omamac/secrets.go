package omamac

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// OmaMac keeps all its secrets – the vault's values and the Jira API tokens – in ONE
// Keychain item. OmaMac is signed ad hoc, so macOS asks again after every update before
// the new build may read an item: one item means one question, not one per secret.

const (
	secretsService = "de.diver80.omamac"
	secretsKey     = "secrets" // the item: a JSON object "vault:NAME" / "jira:ID" → value
)

// secretBackend is the system credential store; tests swap in a map.
type secretBackend interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
}

var secretStore = &secretBlob{backend: mygo.Secrets.Service(secretsService)}

type secretBlob struct {
	mu      sync.Mutex
	backend secretBackend
	data    map[string]string
	loadErr error
	loaded  bool
}

// load reads the item once; a missing item is an empty store.
func (s *secretBlob) load() error {
	if s.loaded {
		return s.loadErr
	}
	s.loaded = true
	s.data = map[string]string{}
	raw, err := s.backend.Get(secretsKey)
	switch {
	case errors.Is(err, mygo.ErrSecretNotFound):
		return nil
	case err != nil:
		s.loadErr = fmt.Errorf("Schlüsselbund nicht lesbar: %w", err)
		return s.loadErr
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		s.data = map[string]string{}
		s.loadErr = fmt.Errorf("Schlüsselbund-Eintrag unlesbar: %w", err)
	}
	return s.loadErr
}

// get returns a secret; ok is false when it is not stored or the Keychain cannot be read.
func (s *secretBlob) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.load() != nil {
		return "", false
	}
	v, ok := s.data[key]
	return v, ok
}

// putAll replaces every secret under prefix ("vault:", "jira:") with values (keys without
// the prefix; empty values are left out) and writes the item when something changed.
// On an error nothing changed, and the caller keeps the values in its 0600 file instead.
func (s *secretBlob) putAll(prefix string, values map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err // never overwrite an item we could not read
	}
	next := make(map[string]string, len(s.data)+len(values))
	for k, v := range s.data {
		if !strings.HasPrefix(k, prefix) {
			next[k] = v
		}
	}
	for k, v := range values {
		if v != "" {
			next[prefix+k] = v
		}
	}
	if maps.Equal(next, s.data) {
		return nil
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if len(raw) > mygo.MaxSecretSize {
		return fmt.Errorf("Schlüsselbund-Eintrag zu groß (%d Bytes)", len(raw))
	}
	if err := s.backend.Set(secretsKey, raw); err != nil {
		return fmt.Errorf("Schlüsselbund nicht beschreibbar: %w", err)
	}
	s.data = next
	return nil
}
