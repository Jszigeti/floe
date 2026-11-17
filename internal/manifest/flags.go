package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/run"
)

// photosICloud reads the marker Photos keeps in the system library while iCloud Photos syncs it.
func photosICloud(home string) (bool, error) {
	_, err := os.Stat(filepath.Join(home, "Pictures/Photos Library.photoslibrary/resources/cpl/cloudsync.noindex/cpl_enabled_marker"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// DesktopDocumentsICloud reads the Finder key set by "Desktop & Documents Folders"; a missing key means never turned on.
func DesktopDocumentsICloud(ctx context.Context, r run.Runner) (bool, error) {
	out, err := r.Run(ctx, run.Cmd{Name: "defaults", Args: []string{"read", "com.apple.finder", "FXICloudDriveDesktop"}})
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(out) == "1", nil
}

// writeFlag writes "true" or "false", or removes a stale flag when it could not be read.
func writeFlag(path string, on bool, err error) error {
	if err != nil {
		_ = os.Remove(path) // ignore not-exist
		return err
	}
	return os.WriteFile(path, fmt.Appendf(nil, "%t\n", on), 0o644)
}

// ReadFlag reads the flag name in dir as writeFlag wrote it; a missing flag, as in a backup made
// before it existed, is false.
func ReadFlag(dir, name string) bool {
	data, err := os.ReadFile(filepath.Join(dir, name))
	return err == nil && strings.TrimSpace(string(data)) == "true"
}

// dumpUntrustedTaps lists the taps Homebrew 7 refuses to load until `brew trust`.
func dumpUntrustedTaps(ctx context.Context, r run.Runner, path string) error {
	out, err := r.Run(ctx, run.Cmd{Name: "brew", Args: []string{"tap-info", "--json", "--installed"}})
	if err != nil {
		_ = os.Remove(path) // ignore not-exist
		return fmt.Errorf("brew tap-info failed: %w", err)
	}
	var taps []struct {
		Name    string `json:"name"`
		Trusted *bool  `json:"trusted"` // absent before Homebrew 7: every tap loads
	}
	if err := json.Unmarshal([]byte(out), &taps); err != nil {
		_ = os.Remove(path) // ignore not-exist
		return fmt.Errorf("reading brew tap-info: %w", err)
	}
	var names []string
	for _, t := range taps {
		if t.Trusted != nil && !*t.Trusted {
			names = append(names, t.Name)
		}
	}
	return writeLines(path, names)
}

// keychainLabel matches an item label in `security dump-keychain` output (attributes only, never -d).
var keychainLabel = regexp.MustCompile(`^\s*0x00000007 <blob>=.*"(.+) Safe Storage"$`)

const keychainTimeout = 10 * time.Second

// dumpKeychainApps lists the apps keeping a session key in the login
// keychain ("Chrome Safe Storage" → Chrome), from item labels only.
func dumpKeychainApps(ctx context.Context, r run.Runner, home, path string) error {
	// A keychain prompt would otherwise hold the nightly backup until someone answers it.
	ctx, cancel := context.WithTimeout(ctx, keychainTimeout)
	defer cancel()
	out, err := r.Run(ctx, run.Cmd{Name: "security", Args: []string{"dump-keychain", filepath.Join(home, "Library/Keychains/login.keychain-db")}})
	if err != nil {
		_ = os.Remove(path) // ignore not-exist
		return fmt.Errorf("security dump-keychain failed: %w", err)
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if m := keychainLabel.FindStringSubmatch(line); m != nil && !slices.Contains(names, m[1]) {
			names = append(names, m[1])
		}
	}
	slices.Sort(names)
	return writeLines(path, names)
}

// ReadList reads the list name in dir as writeLines wrote it, none when a backup made before it has
// no such file.
func ReadList(dir, name string) []string {
	data, _ := os.ReadFile(filepath.Join(dir, name))
	var lines []string
	for line := range strings.Lines(string(data)) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func writeLines(path string, lines []string) error {
	if len(lines) == 0 {
		return writeOrRemove(path, "")
	}
	return writeOrRemove(path, strings.Join(lines, "\n")+"\n")
}
