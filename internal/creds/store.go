// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package creds

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sebastienrousseau/scout/auth"
)

// StoredToken is what `scout login` persists so later runs can resume the
// authorization-code flow without a browser.
type StoredToken struct {
	Endpoint     string    `json:"endpoint"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	TokenURL     string    `json:"token_url"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	Resource     string    `json:"resource,omitempty"`
	Issuer       string    `json:"issuer,omitempty"`
	SavedAt      time.Time `json:"saved_at"`
}

// Store is the on-disk token store, one entry per endpoint.
//
// Entries hold refresh tokens and, for dynamically registered clients,
// client secrets. The file is created 0600 and a store that is readable by
// anyone else is refused rather than used: a long-lived refresh token is
// worth as much as a password.
type Store struct {
	Path string

	mu sync.Mutex
}

// ErrInsecurePermissions reports a token store other users can read.
type ErrInsecurePermissions struct {
	Path string
	Mode os.FileMode
}

func (e *ErrInsecurePermissions) Error() string {
	return fmt.Sprintf("token store %s is mode %#o; it holds refresh tokens and must not be readable by other users (chmod 600 %s)", e.Path, e.Mode.Perm(), e.Path)
}

// DefaultStorePath is $XDG_CONFIG_HOME/scout/tokens.json.
func DefaultStorePath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "scout", "tokens.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "scout", "tokens.json")
	}
	return filepath.Join(home, ".config", "scout", "tokens.json")
}

func (s *Store) path() string {
	if s.Path != "" {
		return s.Path
	}
	return DefaultStorePath()
}

func (s *Store) load() (map[string]StoredToken, error) {
	p := s.path()
	info, err := os.Stat(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return map[string]StoredToken{}, nil
	case err != nil:
		return nil, err
	case info.Mode().Perm()&0o077 != 0:
		return nil, &ErrInsecurePermissions{Path: p, Mode: info.Mode()}
	}
	b, err := os.ReadFile(p) // #nosec G304 -- the path is the operator's own store
	if errors.Is(err, os.ErrNotExist) {
		return map[string]StoredToken{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]StoredToken{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("token store %s: %w", s.path(), err)
	}
	return out, nil
}

// Get returns the stored token for endpoint, if any.
func (s *Store) Get(endpoint string) (*StoredToken, error) {
	all, err := s.load()
	if err != nil {
		return nil, err
	}
	t, ok := all[endpoint]
	if !ok {
		return nil, nil
	}
	return &t, nil
}

// Put saves a token, creating the store with 0600 permissions.
func (s *Store) Put(t StoredToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	t.SavedAt = time.Now()
	all[t.Endpoint] = t
	return s.save(all)
}

// Delete removes the entry for endpoint.
func (s *Store) Delete(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	delete(all, endpoint)
	return s.save(all)
}

// save writes the whole store atomically: a crash between truncate and
// write would otherwise lose every other endpoint's token, not just the one
// being changed.
func (s *Store) save(all map[string]StoredToken) error {
	p := s.path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	// #nosec G117 -- persisting the token is this file's whole purpose; it is
	// written 0600 and refused if the mode ever widens.
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tokens-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp) // no-op once the rename succeeded
	}()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Source builds a refreshing token source from a stored token.
func (t *StoredToken) Source(hc httpClient) auth.TokenSource {
	tok := &auth.Token{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, TokenType: t.TokenType, Scope: t.Scope, Expiry: t.Expiry}
	return auth.NewRefreshingSource(hc.client(), auth.Endpoint{TokenURL: t.TokenURL}, auth.Credentials{ClientID: t.ClientID, ClientSecret: t.ClientSecret}, t.Resource, tok)
}
