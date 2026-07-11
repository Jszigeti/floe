package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Retention struct {
	Daily   int `toml:"daily"`
	Weekly  int `toml:"weekly"`
	Monthly int `toml:"monthly"`
}

type Scope struct {
	Paths   []string `toml:"paths"`
	Exclude []string `toml:"exclude"`
}

type Hooks struct {
	Pre  []string `toml:"pre"`
	Post []string `toml:"post"`
}

type Config struct {
	Destination string    `toml:"destination"`
	Hour        int       `toml:"hour"`
	Retention   Retention `toml:"retention"`
	Scope       Scope     `toml:"scope"`
	Hooks       Hooks     `toml:"hooks"`
}

func ICloudDrivePath(home string) string {
	return filepath.Join(home, "Library/Mobile Documents/com~apple~CloudDocs")
}

// Excludes only cover regenerable content — app data (Containers, browser profiles, VMs) stays in.
func DefaultExcludes(home string) []string {
	return []string{
		filepath.Join(home, "Library/Mobile Documents"),
		filepath.Join(home, "Library/CloudStorage"),
		filepath.Join(home, "Library/Caches"),
		filepath.Join(home, "Library/Containers/com.docker.docker"),
		filepath.Join(home, "Library/Developer"),
		filepath.Join(home, "Library/Application Support/MobileSync"),
		filepath.Join(home, "Library/Logs"),
		filepath.Join(home, "Library/Biome"),
		filepath.Join(home, "Library/Saved Application State"),
		filepath.Join(home, "Library/HTTPStorages"),
		filepath.Join(home, "Library/Android"),
		filepath.Join(home, ".Trash"),
		filepath.Join(home, ".cache"),
		filepath.Join(home, ".npm"),
		filepath.Join(home, ".nvm"),
		filepath.Join(home, ".cargo"),
		filepath.Join(home, ".rustup"),
		filepath.Join(home, ".gradle"),
		filepath.Join(home, "go/pkg"),
		filepath.Join(home, ".ollama/models"),
		filepath.Join(home, ".m2/repository"),
		filepath.Join(home, ".vscode/extensions"),
		filepath.Join(home, ".cursor/extensions"),
		filepath.Join(home, "Library/pnpm"),
		filepath.Join(home, "Library/Application Support/Claude/vm_bundles"),
		filepath.Join(home, "Library/Application Support/Google/Chrome/OptGuideOnDeviceModel"),
		filepath.Join(home, "Library/Application Support/Google/GoogleUpdater"),
		filepath.Join(home, "Library/Application Support/Steam/steamapps/downloading"),
		filepath.Join(home, "Library/Application Support/Steam/steamapps/shadercache"),
		filepath.Join(home, "miniconda3"),
		filepath.Join(home, "anaconda3"),
		filepath.Join(home, ".yarn/berry/cache"),
		"node_modules",
		"floe-restore-*",
		".venv",
		"venv",
		"__pycache__",
		".next",
		"dist",
		"target",
		".pytest_cache",
		".mypy_cache",
		".turbo",
		".pnpm-store",
		".tox",
		".terraform",
		"state.vscdb.backup",
		"Crashpad",
		// Chromium/Electron cache conventions (Chrome, Discord, Claude, Slack…).
		"Cache",
		"Code Cache",
		"GPUCache",
		"Service Worker",
	}
}

func Default() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	return Config{
		Destination: ICloudDrivePath(home),
		Hour:        13,
		Retention:   Retention{Daily: 7, Weekly: 4, Monthly: 6},
		Scope:       Scope{Paths: []string{home}, Exclude: DefaultExcludes(home)},
	}, nil
}

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config/floe/config.toml"), nil
}

func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	var c Config
	if _, err := toml.DecodeFile(p, &c); err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("no config at %s — run `floe init` first", p)
		}
		return Config{}, fmt.Errorf("reading %s: %w", p, err)
	}
	dest, err := NormalizeDestination(c.Destination)
	if err != nil {
		return Config{}, err
	}
	c.Destination = dest
	return c, nil
}

// NormalizeDestination expands a leading ~ or $HOME and requires an absolute path.
// It does not require the path to exist — that check is the wizard's job only,
// since a config loaded outside `floe init` (e.g. hand-edited) must still resolve.
func NormalizeDestination(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("config has no destination — run `floe init`")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch {
	case input == "~" || strings.HasPrefix(input, "~/"):
		input = filepath.Join(home, strings.TrimPrefix(input, "~"))
	case input == "$HOME" || strings.HasPrefix(input, "$HOME/"):
		input = filepath.Join(home, strings.TrimPrefix(input, "$HOME"))
	}
	if !filepath.IsAbs(input) {
		return "", fmt.Errorf("destination %s must be an absolute path", input)
	}
	return input, nil
}

func (c Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(c); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (c Config) RepoPath() (string, error) {
	host, err := os.Hostname()
	if err != nil {
		return "", err
	}
	host = strings.TrimSuffix(host, ".local")
	host = strings.ReplaceAll(host, " ", "-")
	return filepath.Join(c.Destination, "Floe.backup", host), nil
}
