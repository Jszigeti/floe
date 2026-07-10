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

func DefaultExcludes(home string) []string {
	return []string{
		filepath.Join(home, "Library/Mobile Documents"),
		filepath.Join(home, "Library/CloudStorage"),
		filepath.Join(home, "Library/Caches"),
		filepath.Join(home, "Library/Containers/com.docker.docker"),
		filepath.Join(home, "Library/Developer"),
		filepath.Join(home, "Library/Application Support/MobileSync"),
		filepath.Join(home, ".Trash"),
		filepath.Join(home, ".cache"),
		filepath.Join(home, ".npm"),
		filepath.Join(home, ".nvm"),
		filepath.Join(home, ".cargo"),
		filepath.Join(home, ".rustup"),
		filepath.Join(home, ".gradle"),
		"node_modules",
		"floe-restore-*",
		".venv",
		"venv",
		"__pycache__",
		".next",
		"dist",
		"target",
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
	return c, nil
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
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
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
