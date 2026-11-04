package launchd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Jszigeti/floe/internal/run"
)

const Label = "com.floe.backup"

func PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library/LaunchAgents", Label+".plist"), nil
}

func LogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library/Logs/floe.log"), nil
}

func Plist(execPath string, hour int) (string, error) {
	log, err := LogPath()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>backup</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>FLOE_SCHEDULED</key><string>1</string><key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
	<key>StartCalendarInterval</key>
	<dict><key>Hour</key><integer>%d</integer><key>Minute</key><integer>0</integer></dict>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, Label, execPath, hour, log, log), nil
}

func guiDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func Install(ctx context.Context, r run.Runner, execPath string, hour int) error {
	pp, err := PlistPath()
	if err != nil {
		return err
	}
	content, err := Plist(execPath, hour)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pp), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(pp, []byte(content), 0o644); err != nil {
		return err
	}
	// bootout fails when not loaded yet — expected, ignored.
	_, _ = r.Run(ctx, run.Cmd{Name: "launchctl", Args: []string{"bootout", guiDomain(), pp}})
	_, err = r.Run(ctx, run.Cmd{Name: "launchctl", Args: []string{"bootstrap", guiDomain(), pp}})
	return err
}

func Uninstall(ctx context.Context, r run.Runner) error {
	pp, err := PlistPath()
	if err != nil {
		return err
	}
	// bootout fails when not loaded yet — expected, ignored.
	_, _ = r.Run(ctx, run.Cmd{Name: "launchctl", Args: []string{"bootout", guiDomain(), pp}})
	err = os.Remove(pp)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func IsLoaded(ctx context.Context, r run.Runner) bool {
	_, err := r.Run(ctx, run.Cmd{Name: "launchctl", Args: []string{"print", guiDomain() + "/" + Label}})
	return err == nil
}
