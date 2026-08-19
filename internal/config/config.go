// Package config loads and provides Quark's user configuration.
// All fields have sensible defaults; config.toml inside the config dir is optional.
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/crazy-vedic/quark/internal/keybindings"
)

// Config holds the full application configuration.
type Config struct {
	UI          UI          `toml:"ui"`
	HTTP        HTTP        `toml:"http"`
	Logging     Logging     `toml:"logging"`
	Backup      Backup      `toml:"backup"`
	Keybindings Keybindings `toml:"keybindings"`
}

// Keybindings is re-exported from the keybindings package so the config
// package can stay the single source of truth for TOML decoding.
type Keybindings = keybindings.Keybindings

// UI configures appearance and editing.
type UI struct {
	Theme         string `toml:"theme"`          // auto | dark | light | transparent
	DefaultMethod string `toml:"default_method"` // GET
	Editor        string `toml:"editor"`         // empty = $EDITOR, fallback vim
}

// HTTP configures request defaults.
type HTTP struct {
	Timeout            duration            `toml:"timeout"`
	FollowRedirects    bool                `toml:"follow_redirects"`
	MaxRedirects       int                 `toml:"max_redirects"`
	ClientCertificates []ClientCertificate `toml:"client_certificates"`
}

// ClientCertificate configures TLS identity and trust material for one host.
// PasswordEnv is preferred for deployments because it keeps the secret out of
// config.toml. Password is supported for interactive TUI setup.
type ClientCertificate struct {
	Host        string `toml:"host"`
	File        string `toml:"file"`
	Type        string `toml:"type,omitempty"`
	KeyFile     string `toml:"key_file,omitempty"`
	CAFile      string `toml:"ca_file,omitempty"`
	Password    string `toml:"password,omitempty"`
	PasswordEnv string `toml:"password_env,omitempty"`
}

// Logging configures the application log.
type Logging struct {
	Level   string `toml:"level"` // debug | info | warn | error
	File    string `toml:"file"`
	MaxSize string `toml:"max_size"`
}

// Backup configures auto-backup behaviour.
type Backup struct {
	AutoBackup bool `toml:"auto_backup"`
	KeepLast   int  `toml:"keep_last"`
}

// duration is a TOML-decodable wrapper for time.Duration.
type duration struct{ time.Duration }

func (d *duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

// Default returns a Config populated with safe defaults.
// configDir is the directory where config.toml and the DB are stored.
func Default(configDir string) Config {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	return Config{
		UI: UI{
			Theme:         "auto",
			DefaultMethod: "GET",
			Editor:        editor,
		},
		HTTP: HTTP{
			Timeout:         duration{30 * time.Second},
			FollowRedirects: true,
			MaxRedirects:    10,
		},
		Logging: Logging{
			Level:   "info",
			File:    filepath.Join(configDir, "quark.log"),
			MaxSize: "10MB",
		},
		Backup: Backup{
			AutoBackup: true,
			KeepLast:   10,
		},
		Keybindings: keybindings.DefaultKeybindings(),
	}
}

// Load reads configDir/config.toml and merges only the explicitly-set fields
// onto the defaults. Absent fields retain their defaults; this avoids the
// zero-value override problem for booleans (FollowRedirects, AutoBackup).
func Load(configDir string) (Config, error) {
	cfg := Default(configDir)

	path := filepath.Join(configDir, "config.toml")

	// Decode into an override struct, then merge only defined keys via MetaData.
	var override Config
	md, err := toml.DecodeFile(path, &override)
	if err != nil {
		// File absent — not an error.
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	// Merge only fields that the user explicitly set in their config.toml.
	if md.IsDefined("ui", "theme") {
		cfg.UI.Theme = override.UI.Theme
	}
	if md.IsDefined("ui", "default_method") {
		cfg.UI.DefaultMethod = override.UI.DefaultMethod
	}
	if md.IsDefined("ui", "editor") {
		cfg.UI.Editor = override.UI.Editor
	}
	if md.IsDefined("http", "timeout") {
		cfg.HTTP.Timeout = override.HTTP.Timeout
	}
	if md.IsDefined("http", "follow_redirects") {
		cfg.HTTP.FollowRedirects = override.HTTP.FollowRedirects
	}
	if md.IsDefined("http", "max_redirects") {
		cfg.HTTP.MaxRedirects = override.HTTP.MaxRedirects
	}
	if md.IsDefined("http", "client_certificates") {
		cfg.HTTP.ClientCertificates = override.HTTP.ClientCertificates
	}
	if md.IsDefined("logging", "level") {
		cfg.Logging.Level = override.Logging.Level
	}
	if md.IsDefined("logging", "file") {
		cfg.Logging.File = override.Logging.File
	}
	if md.IsDefined("backup", "auto_backup") {
		cfg.Backup.AutoBackup = override.Backup.AutoBackup
	}
	if md.IsDefined("backup", "keep_last") {
		cfg.Backup.KeepLast = override.Backup.KeepLast
	}
	if md.IsDefined("keybindings") {
		cfg.Keybindings = mergeKeybindings(cfg.Keybindings, override.Keybindings)
	}

	return cfg, nil
}

// mergeKeybindings copies non-empty fields from src into dst.
func mergeKeybindings(dst, src keybindings.Keybindings) keybindings.Keybindings {
	v := reflect.ValueOf(&dst).Elem()
	s := reflect.ValueOf(src)
	for i := 0; i < v.NumField(); i++ {
		if s.Field(i).String() != "" {
			v.Field(i).SetString(s.Field(i).String())
		}
	}
	return dst
}

// SaveKeybindings writes only the [keybindings] section to configDir/config.toml,
// preserving all other sections.
func SaveKeybindings(configDir string, binds keybindings.Keybindings) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.toml")

	// Read existing file as raw bytes.
	var raw []byte
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		raw, _ = os.ReadFile(path)
	}

	// Decode into a generic map so we preserve the exact structure of
	// all other sections.
	var doc map[string]any
	if len(raw) > 0 {
		if _, err := toml.Decode(string(raw), &doc); err != nil {
			// If the existing file is invalid, start fresh.
			doc = make(map[string]any)
		}
	} else {
		doc = make(map[string]any)
	}

	// Build the keybindings map from the struct via reflection.
	bindsMap := make(map[string]any)
	v := reflect.ValueOf(binds)
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		key := t.Field(i).Tag.Get("toml")
		val := v.Field(i).String()
		if key != "" && val != "" {
			bindsMap[key] = val
		}
	}
	doc["keybindings"] = bindsMap

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(doc)
}

// SaveClientCertificates writes only the HTTP client_certificates setting,
// preserving all other sections in config.toml.
func SaveClientCertificates(configDir string, certs []ClientCertificate) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.toml")
	var raw []byte
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		raw, _ = os.ReadFile(path)
	}
	var doc map[string]any
	if len(raw) > 0 {
		if _, err := toml.Decode(string(raw), &doc); err != nil {
			doc = make(map[string]any)
		}
	} else {
		doc = make(map[string]any)
	}
	httpDoc, ok := doc["http"].(map[string]any)
	if !ok {
		httpDoc = make(map[string]any)
	}
	entries := make([]map[string]any, 0, len(certs))
	for _, cert := range certs {
		entry := map[string]any{"host": cert.Host, "file": cert.File}
		if cert.Type != "" {
			entry["type"] = cert.Type
		}
		if cert.KeyFile != "" {
			entry["key_file"] = cert.KeyFile
		}
		if cert.CAFile != "" {
			entry["ca_file"] = cert.CAFile
		}
		if cert.Password != "" {
			entry["password"] = cert.Password
		}
		if cert.PasswordEnv != "" {
			entry["password_env"] = cert.PasswordEnv
		}
		entries = append(entries, entry)
	}
	httpDoc["client_certificates"] = entries
	doc["http"] = httpDoc
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	return toml.NewEncoder(f).Encode(doc)
}

// Timeout returns the configured HTTP timeout as time.Duration.
func (c Config) Timeout() time.Duration { return c.HTTP.Timeout.Duration }

// BackupDir returns the default backup directory path.
func (c Config) BackupDir(dataDir string) string {
	return filepath.Join(dataDir, "backup")
}
