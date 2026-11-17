package inventory

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

// bundle creates name.app under root with an Info.plist holding infoJSON,
// which the fake plutil hands back as is.
func bundle(t *testing.T, root, name, infoJSON string) string {
	t.Helper()
	app := filepath.Join(root, name)
	writeFile(t, filepath.Join(app, "Contents", "Info.plist"), infoJSON)
	return app
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeTools answers plutil with the file's own content and codesign with
// signatures[app base name], an unsigned error otherwise.
func fakeTools(signatures map[string]string) *run.Fake {
	return &run.Fake{Handler: func(c run.Cmd) (string, error) {
		last := c.Args[len(c.Args)-1]
		switch c.Name {
		case "plutil":
			data, err := os.ReadFile(last)
			return string(data), err
		case "sh":
			if out, ok := signatures[filepath.Base(last)]; ok {
				return out, nil
			}
			return last + ": code object is not signed at all\n", os.ErrInvalid
		}
		return "", os.ErrInvalid
	}}
}

func byName(apps []App) map[string]App {
	m := map[string]App{}
	for _, a := range apps {
		m[a.Name] = a
	}
	return m
}

const masList = "1500855883  CapCut            (9.7.1)\n 361285480  Keynote           (15.2.1)\n"

func TestScanClassifies(t *testing.T) {
	root := t.TempDir()
	keynote := bundle(t, root, "Keynote Creator Studio.app", `{"CFBundleIdentifier":"com.apple.Keynote","CFBundleName":"Keynote","CFBundleDisplayName":"Keynote Creator Studio","CFBundleExecutable":"Keynote","CFBundleShortVersionString":"15.2.1"}`)
	writeFile(t, filepath.Join(keynote, "Contents", "_MASReceipt", "receipt"), "r")
	bundle(t, root, "Safari.app", `{"CFBundleIdentifier":"com.apple.Safari","CFBundleExecutable":"Safari"}`)
	bundle(t, root, "Google Chrome.app", `{"CFBundleIdentifier":"com.google.Chrome","CFBundleExecutable":"Google Chrome"}`)
	bundle(t, root, "Voicebox.app", `{"CFBundleIdentifier":"sh.voicebox.app","CFBundleExecutable":"voicebox"}`)
	bundle(t, filepath.Join(root, "Utilities"), "Helper.app", `{"CFBundleIdentifier":"com.example.helper"}`)
	writeFile(t, filepath.Join(root, "Screenshot.png"), "png")
	writeFile(t, filepath.Join(root, "Python 3.12", "ReadMe.rtf"), "rtf")
	f := fakeTools(map[string]string{
		"Keynote Creator Studio.app": "Authority=Apple Mac OS Application Signing\nTeamIdentifier=K36BKF7T3D\n",
		"Safari.app":                 "Authority=macOS Software Signing\nAuthority=Apple Root CA\nTeamIdentifier=not set\n",
		"Google Chrome.app":          "Authority=Developer ID Application: Google LLC (EQHXZ8M8AV)\nTeamIdentifier=EQHXZ8M8AV\n",
	})
	casks := CaskIndex{"Google Chrome.app": "google-chrome", "Visual Studio Code.app": "visual-studio-code"}

	apps, err := Scan(context.Background(), f, []string{root, filepath.Join(root, "missing")}, casks, masList)
	if err != nil {
		t.Fatal(err)
	}

	got := byName(apps)
	if len(apps) != 5 {
		t.Fatalf("want 5 apps (no png, no plain folder), got %+v", apps)
	}
	if k := got["Keynote Creator Studio"]; k.Source != SourceAppStore || k.StoreID != "361285480" || k.BundleID != "com.apple.Keynote" || k.Executable != "Keynote" || k.Version != "15.2.1" || k.Path != keynote {
		t.Errorf("Keynote: %+v", k)
	}
	if s := got["Safari"]; s.Source != SourceSystem || s.Team != "" {
		t.Errorf("Safari: %+v", s)
	}
	if c := got["Google Chrome"]; c.Source != SourceCask || c.Cask != "google-chrome" || c.Team != "EQHXZ8M8AV" {
		t.Errorf("Chrome: %+v", c)
	}
	if v := got["Voicebox"]; v.Source != SourceBackup || v.Size == 0 {
		t.Errorf("Voicebox: %+v", v)
	}
	if h := got["Helper"]; h.Source != SourceUnknown || h.BundleID != "com.example.helper" {
		t.Errorf("app in a subfolder is recorded, never backed up: %+v", h)
	}
}

func TestScanWithoutCaskIndexBacksUpNothing(t *testing.T) {
	root := t.TempDir()
	bundle(t, root, "Voicebox.app", `{"CFBundleIdentifier":"sh.voicebox.app"}`)

	apps, err := Scan(context.Background(), fakeTools(nil), []string{root}, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(apps) != 1 || apps[0].Source != SourceUnknown {
		t.Fatalf("want Voicebox unknown, got %+v", apps)
	}
}

func TestScanRecordsNestedIDsAndTeam(t *testing.T) {
	root := t.TempDir()
	app := bundle(t, root, "Docker.app", `{"CFBundleIdentifier":"com.docker.docker"}`)
	writeFile(t, filepath.Join(app, "Contents", "PlugIns", "Share.appex", "Contents", "Info.plist"), `{"CFBundleIdentifier":"com.docker.share"}`)
	writeFile(t, filepath.Join(app, "Contents", "Library", "LoginItems", "Helper.app", "Contents", "Info.plist"), `{"CFBundleIdentifier":"com.docker.helper"}`)
	writeFile(t, filepath.Join(app, "Contents", "Frameworks", "Kit.framework", "Resources", "Info.plist"), `{}`)
	f := fakeTools(map[string]string{"Docker.app": "Authority=Developer ID Application: Docker Inc (9BNSXJN65R)\nTeamIdentifier=9BNSXJN65R\n"})

	apps, err := Scan(context.Background(), f, []string{root}, CaskIndex{"Docker.app": "docker-desktop"}, "")
	if err != nil {
		t.Fatal(err)
	}

	d := apps[0]
	if d.Team != "9BNSXJN65R" {
		t.Errorf("team = %q", d.Team)
	}
	if want := []string{"com.docker.helper", "com.docker.share"}; !reflect.DeepEqual(d.Nested, want) {
		t.Errorf("nested = %v, want %v", d.Nested, want)
	}
}

func TestWriteThenRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	want := []App{{Name: "Google Chrome", BundleID: "com.google.Chrome", Source: SourceCask, Cask: "google-chrome", Nested: []string{"com.google.Chrome.helper"}, Size: 42}}

	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"source": "cask"`) {
		t.Errorf("apps.json not readable as written: %s", data)
	}
}

func TestParseMasListKeepsNumericIDsOnly(t *testing.T) {
	// An id without a name still installs, as mas.txt from an older backup may hold one.
	out := "111  Keynote  (15.2)\n\nNo installed apps found\n 222  Tempus Stopwatch  (2026.2)\n333\n"
	want := []MasApp{{ID: "111", Name: "Keynote"}, {ID: "222", Name: "Tempus Stopwatch"}, {ID: "333"}}
	if got := ParseMasList(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseMasList = %q, want %q", got, want)
	}
}
