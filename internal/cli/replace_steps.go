package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/manifest"
	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/run"
)

// inventoryOf reads the app inventory the backup carries. A backup made before it existed only
// lists its apps, which are matched against Homebrew's cask list on this Mac.
func inventoryOf(ctx context.Context, recipe, caskURL, home string) ([]inventory.App, error) {
	list, err := inventory.Read(filepath.Join(recipe, "apps.json"))
	if err == nil {
		return list, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("reading the app inventory: %w", err)
	}
	// A cache that cannot be saved still gives the list.
	casks, _ := inventory.LoadCasks(ctx, filepath.Join(home, ".local/state/floe/cask.json"), caskURL, 7*24*time.Hour, time.Now())
	return apps.ListedApps(recipe, casks), nil
}

// appsSize is what the apps to install take on disk.
func appsSize(list []inventory.App) int64 {
	var size int64
	for _, a := range list {
		if a.Source == inventory.SourceAppStore || a.Source == inventory.SourceCask || a.Source == inventory.SourceBackup {
			size += a.Size
		}
	}
	return size
}

// readFlag reads a true/false flag of the recipe; a backup made before it existed says false.
func readFlag(recipe, name string) bool {
	data, err := os.ReadFile(filepath.Join(recipe, name))
	return err == nil && strings.TrimSpace(string(data)) == "true"
}

// askForPower has a MacBook on battery plugged in, since a flat battery or a closed lid would
// cut the run. It returns the power the screen shows, read again once the user plugged it in.
func askForPower(ctx context.Context, p prompter, r run.Runner) string {
	if !onBattery(ctx, r) {
		return "on power"
	}
	p.cmd.Println("This Mac runs on battery: plug it in and keep its lid open until the end.")
	p.answer("Press Enter to continue")
	if onBattery(ctx, r) {
		return "on battery"
	}
	return "on power"
}

// onBattery is false on a desktop, or when pmset cannot tell.
func onBattery(ctx context.Context, r run.Runner) bool {
	out, err := r.Run(ctx, run.Cmd{Name: "pmset", Args: []string{"-g", "batt"}})
	return err == nil && strings.Contains(out, "Battery Power")
}

// openAppStore has the user sign in to the App Store, which mas cannot do. A skip takes the App
// Store apps out of the plan and returns their ids, to install later.
func openAppStore(ctx context.Context, p prompter, r run.Runner, plan *apps.Plan) []string {
	if len(plan.StoreIDs) == 0 {
		return nil
	}
	p.cmd.Printf("%d of your apps come from the App Store: sign in to it with your Apple Account.\n", len(plan.StoreIDs))
	_, _ = r.Run(ctx, run.Cmd{Name: "open", Args: []string{"-a", "App Store"}}) // the user can still open it by hand
	if a, _ := p.answer("Press Enter once signed in, or s to skip the App Store apps"); !strings.EqualFold(a, "s") {
		return nil
	}
	skipped := plan.StoreIDs
	plan.StoreIDs = nil
	return skipped
}

// keepAwake keeps the Mac from sleeping until stop. caffeinate -w also ends with floe, even a killed one.
func keepAwake(ctx context.Context, r run.Runner) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Without it the Mac may sleep, and the restore resumes on wake.
		_, _ = r.Run(ctx, run.Cmd{Name: "caffeinate", Args: []string{"-i", "-w", strconv.Itoa(os.Getpid())}})
	}()
	return func() {
		cancel()
		<-done
	}
}

// oldMac is what the recipe says beyond the run: the sessions to sign in to again, the taps to
// trust, and whether Desktop & Documents followed iCloud Drive there and not here.
func oldMac(ctx context.Context, r run.Runner, recipe string, list []inventory.App) replace.OldMac {
	old := replace.OldMac{Apps: list, KeychainApps: readLines(recipe, "keychain-apps.txt"), UntrustedTaps: readLines(recipe, "untrusted-taps.txt")}
	if readFlag(recipe, "desktop-documents-icloud") {
		// A state that cannot be read keeps the reminder, which costs nothing when it is on.
		on, _ := manifest.DesktopDocumentsICloud(ctx, r)
		old.TurnOnDesktopDocuments = !on
	}
	return old
}

// readLines reads a list of the recipe, none when a backup made before it has no such file.
func readLines(recipe, name string) []string {
	data, _ := os.ReadFile(filepath.Join(recipe, name))
	var lines []string
	for line := range strings.Lines(string(data)) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// offerActions runs each action the user picks, until Enter or the end of the input: without
// a terminal the summary is the end.
func offerActions(ctx context.Context, p prompter, r run.Runner, actions []replace.Action) {
	for {
		choice, ok := p.answer("Choice, or Enter to quit")
		if !ok {
			p.cmd.Println()
		}
		if !ok || choice == "" {
			return
		}
		i := slices.IndexFunc(actions, func(a replace.Action) bool { return strings.EqualFold(a.Key, choice) })
		switch {
		case i < 0:
			p.cmd.Println("Type one of the numbers above, R, or Enter to quit.")
		case actions[i].Command == "":
			p.cmd.Println("Only you can do this one.")
		default:
			p.cmd.Println(outcome(replace.RunAction(ctx, r, apps.BrewDirs, actions[i]), actions[i]))
		}
	}
}

// outcome is the line after an action ran.
func outcome(err error, a replace.Action) string {
	switch {
	case errors.Is(err, replace.ErrNoHomebrew):
		return "! Homebrew is still not installed: its download failed, check the internet connection and try again."
	case err != nil:
		return "! It did not finish, see the lines above."
	case a.Key == "R":
		return "Restarting…"
	}
	return "✓ Done: " + a.Label
}
