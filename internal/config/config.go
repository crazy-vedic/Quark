// Package config loads and provides Quark's user configuration.
// All fields have sensible defaults; config.toml inside the config dir is optional.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/crazy-vedic/quark/internal/atomicfile"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

// Config holds the full application configuration.
type Config struct {
	UI          UI          `toml:"ui"`
	HTTP        HTTP        `toml:"http"`
	Logging     Logging     `toml:"logging"`
	Backup      Backup      `toml:"backup"`
	Updates     Updates     `toml:"updates"`
	Format      Format      `toml:"format"`
	Keybindings Keybindings `toml:"keybindings"`
}

// Keybindings is re-exported from the keybindings package so the config
// package can stay the single source of truth for TOML decoding.
type Keybindings = keybindings.Keybindings

// UI configures appearance and editing.
type UI struct {
	Theme         string `toml:"theme"`                     // built-in or themes/<name>.toml
	DefaultMethod string `toml:"default_method"`            // GET
	Editor        string `toml:"editor"`                    // empty = $EDITOR, fallback vim
	LastRequestID string `toml:"last_request_id,omitempty"` // last request opened in the TUI
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

// Updates configures version checking and self-update behavior.
type Updates struct {
	Check             bool     `toml:"check"`
	CheckInterval     duration `toml:"check_interval"`
	RetryAfterFailure duration `toml:"retry_after_failure"`
	Timeout           duration `toml:"timeout"`
	AutoApply         bool     `toml:"auto_apply"` // Deferred: only applies to brew/winget channels
}

// Format configures formatting behavior for request bodies.
type Format struct {
	JSONOnSave bool `toml:"json_on_save"`
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
		Updates: Updates{
			Check:             true,
			CheckInterval:     duration{12 * time.Hour},
			RetryAfterFailure: duration{1 * time.Hour},
			Timeout:           duration{10 * time.Second},
			AutoApply:         false,
		},
		Format: Format{
			JSONOnSave: true,
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
		// Name the file: the TOML error alone ("line 2: ...") does not say
		// which file is broken.
		return cfg, fmt.Errorf("%s: %w", path, err)
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
	if md.IsDefined("ui", "last_request_id") {
		cfg.UI.LastRequestID = override.UI.LastRequestID
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
	if md.IsDefined("updates", "check") {
		cfg.Updates.Check = override.Updates.Check
	}
	if md.IsDefined("updates", "check_interval") {
		cfg.Updates.CheckInterval = override.Updates.CheckInterval
	}
	if md.IsDefined("updates", "retry_after_failure") {
		cfg.Updates.RetryAfterFailure = override.Updates.RetryAfterFailure
	}
	if md.IsDefined("updates", "timeout") {
		cfg.Updates.Timeout = override.Updates.Timeout
	}
	if md.IsDefined("updates", "auto_apply") {
		cfg.Updates.AutoApply = override.Updates.AutoApply
	}
	if md.IsDefined("format", "json_on_save") {
		cfg.Format.JSONOnSave = override.Format.JSONOnSave
	}
	if md.IsDefined("keybindings") {
		cfg.Keybindings = mergeKeybindings(cfg.Keybindings, override.Keybindings)
		// Migrate the old default where collection deletion used "d". The
		// request-delete binding is new, so an old config has no explicit
		// request_delete field and can safely adopt the split defaults.
		if md.IsDefined("keybindings", "sidebar_delete") &&
			!md.IsDefined("keybindings", "request_delete") &&
			override.Keybindings.SidebarDelete == "d" {
			cfg.Keybindings.SidebarDelete = "D"
		}
		if md.IsDefined("keybindings", "import_curl") &&
			!md.IsDefined("keybindings", "sidebar_import_postman") &&
			override.Keybindings.ImportCurl == "I" {
			cfg.Keybindings.ImportCurl = "i"
		}
	}

	return cfg, nil
}

// saveMu serializes the read-modify-write cycles below. The TUI runs some
// saves in background commands, so two of them must not interleave and drop
// each other's changes.
var saveMu sync.Mutex

// SaveLastRequestID persists the request opened most recently in the TUI,
// preserving all other config.toml sections and values.
func SaveLastRequestID(configDir, requestID string) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.toml")
	doc, err := readConfigDoc(path)
	if err != nil {
		return err
	}
	uiDoc, ok := doc["ui"].(map[string]any)
	if !ok {
		uiDoc = make(map[string]any)
	}
	if requestID == "" {
		delete(uiDoc, "last_request_id")
	} else {
		uiDoc["last_request_id"] = requestID
	}
	doc["ui"] = uiDoc
	return writeConfigDoc(configDir, doc)
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
	saveMu.Lock()
	defer saveMu.Unlock()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.toml")

	// Decode into a generic map so we preserve the exact structure of
	// all other sections. An unreadable or invalid config is not overwritten.
	doc, err := readConfigDoc(path)
	if err != nil {
		return err
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

	return writeConfigDoc(configDir, doc)
}

// SaveTheme updates only [ui].theme. It refuses malformed existing config and
// writes a temporary file before replacement so a failed encode cannot truncate
// a working configuration containing credentials or keybindings.
func SaveTheme(configDir, name string) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.toml")
	doc, err := readConfigDoc(path)
	if err != nil {
		return err
	}
	ui, ok := doc["ui"].(map[string]any)
	if !ok {
		ui = make(map[string]any)
	}
	ui["theme"] = name
	doc["ui"] = ui

	return writeConfigDoc(configDir, doc)
}

// readConfigDoc loads config.toml as a generic document so a saver can change
// one section and keep the rest. A missing file is an empty document. Any
// other read error and any parse error is returned: carrying on with an empty
// document would overwrite the user's settings with a single section.
func readConfigDoc(path string) (map[string]any, error) {
	doc := make(map[string]any)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return doc, nil
		}
		return nil, fmt.Errorf("read config.toml: %w", err)
	}
	if len(raw) == 0 {
		return doc, nil
	}
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return nil, fmt.Errorf("decode config.toml: %w", err)
	}
	return doc, nil
}

// writeConfigDoc replaces config.toml atomically and owner-only (it can hold
// certificate passwords). See atomicfile.Write.
func writeConfigDoc(configDir string, doc map[string]any) error {
	return atomicfile.Write(
		filepath.Join(configDir, "config.toml"), 0o600,
		func(w io.Writer) error { return toml.NewEncoder(w).Encode(doc) },
	)
}

// SaveClientCertificates writes only the HTTP client_certificates setting,
// preserving all other sections in config.toml.
func SaveClientCertificates(configDir string, certs []ClientCertificate) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.toml")
	doc, err := readConfigDoc(path)
	if err != nil {
		return err
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
	return writeConfigDoc(configDir, doc)
}

// Timeout returns the configured HTTP timeout as time.Duration.
func (c Config) Timeout() time.Duration { return c.HTTP.Timeout.Duration }

// BackupDir returns the default backup directory path.
func (c Config) BackupDir(dataDir string) string {
	return filepath.Join(dataDir, "backup")
}
