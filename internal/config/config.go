package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// Repo and Machine tie a config to one backup folder and one Mac: a rename keeps the
// folder, and a config copied to another Mac is caught before it backs up.
type Config struct {
	Destination string    `toml:"destination"`
	Hour        int       `toml:"hour"`
	Repo        string    `toml:"repo"`
	Machine     string    `toml:"machine"`
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
		// Unreadable whatever permission is granted, and scoped to exactly that: everything readable around them stays in the backup.
		filepath.Join(home, "Library/Application Support/FileProvider/*/wharf"),
		filepath.Join(home, "Library/Group Containers/group.com.apple.CoreSpeech/Caches"),
		filepath.Join(home, "Library/Group Containers/group.com.apple.secure-control-center-preferences/Library/Preferences"),
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
		_ = f.Close() // Don't mask the encode error with a secondary close failure.
		return err
	}
	return f.Close()
}

// MachineName is the hostname floe names a new backup folder after.
func MachineName() (string, error) {
	host, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(strings.TrimSuffix(host, ".local"), " ", "-"), nil
}

// RepoPath is the recorded repository, or the one a config from before v0.2.0 implies.
func (c Config) RepoPath() (string, error) {
	if c.Repo != "" {
		return c.Repo, nil
	}
	name, err := MachineName()
	if err != nil {
		return "", err
	}
	return filepath.Join(c.Destination, "Floe.backup", name), nil
}

// Parse reads a config from TOML text, such as the one a snapshot holds.
func Parse(data string) (Config, error) {
	var c Config
	_, err := toml.Decode(data, &c)
	return c, err
}

// Rehome moves every path under oldHome to newHome, so a config taken from another account
// points at this one. The character after oldHome must end a name, which keeps /Users/alice2,
// /Users/alice-work and /Users/alice.old out of a move from /Users/alice.
func (c Config) Rehome(oldHome, newHome string) Config {
	re := regexp.MustCompile(regexp.QuoteMeta(oldHome) + `([^\w.-]|$)`)
	rehome := func(s string) string {
		return re.ReplaceAllStringFunc(s, func(m string) string { return newHome + m[len(oldHome):] })
	}
	move := func(list []string) []string {
		out := slices.Clone(list)
		for i := range out {
			out[i] = rehome(out[i])
		}
		return out
	}
	c.Destination = rehome(c.Destination)
	c.Scope.Paths = move(c.Scope.Paths)
	c.Scope.Exclude = move(c.Scope.Exclude)
	c.Hooks.Pre = move(c.Hooks.Pre)
	c.Hooks.Post = move(c.Hooks.Post)
	return c
}
