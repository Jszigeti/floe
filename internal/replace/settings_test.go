package replace

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

const oldUUID = "1A2B3C4D-0000-1111-2222-333344445555"

func plistDoc(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
` + body + "\n</plist>\n"
}

func dictBody(values map[string]string) string {
	var b strings.Builder
	b.WriteString("<dict>\n")
	for k, v := range values {
		b.WriteString("\t<key>" + k + "</key>\n\t" + v + "\n")
	}
	b.WriteString("</dict>")
	return b.String()
}

// settingsFake answers plutil from the old values and defaults export from the new Mac's.
func settingsFake(oldValues, newValues map[string]string) *run.Fake {
	return &run.Fake{Handler: func(c run.Cmd) (string, error) {
		switch {
		case c.Name == "plutil" && c.Args[0] == "-convert":
			return plistDoc(dictBody(oldValues)), nil
		case c.Name == "plutil" && c.Args[0] == "-extract":
			return plistDoc(oldValues[strings.ReplaceAll(c.Args[1], `\.`, ".")]), nil
		case c.Name == "defaults" && strings.Contains(strings.Join(c.Args, " "), "export"):
			return plistDoc(dictBody(newValues)), nil
		}
		return "", nil
	}}
}

func writes(f *run.Fake) map[string]string {
	out := map[string]string{}
	for _, c := range f.Calls {
		if c.Name == "defaults" && strings.Contains(strings.Join(c.Args, " "), "write") {
			out[c.Args[len(c.Args)-2]] = c.Args[len(c.Args)-1]
		}
	}
	return out
}

func TestIsTrace(t *testing.T) {
	bigData := "<data>" + base64.StdEncoding.EncodeToString(make([]byte, 3*1024)) + "</data>"
	cases := []struct {
		key, value string
		want       bool
	}{
		{"NSWindow Frame Main", "<string>0 0 800 600</string>", true},
		{"RecentDocuments", "<array/>", true},
		{"AKLastLocale", "<string>en_US</string>", true},
		{"LastUpdateDate", "<date>2026-11-01T10:00:00Z</date>", true},
		{"blob", bigData, true},
		{"lastHost", "<string>" + oldUUID + "</string>", true},
		{"tilesize", "<integer>36</integer>", false},
		{"wvous-tl-corner", "<integer>2</integer>", false},
		{"AppleInterfaceStyle", "<string>Dark</string>", false},
		{"KeyRepeat", "<integer>2</integer>", false},
		{"persistent-apps", "<array><dict><key>tile-type</key><string>file-tile</string></dict></array>", false},
		{"small", "<data>AAAA</data>", false},
	}
	for _, c := range cases {
		if got := IsTrace(c.key, []byte(plistDoc(c.value)), oldUUID); got != c.want {
			t.Errorf("IsTrace(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestCarrySettingsWritesOnlyChangedKeys(t *testing.T) {
	f := settingsFake(
		map[string]string{
			"tilesize":            "<integer>36</integer>",
			"orientation":         "<string>left</string>",
			"NSWindow Frame Main": "<string>0 0 10 10</string>",
		},
		map[string]string{
			"tilesize":    "<integer>64</integer>",
			"orientation": "<string>left</string>",
		},
	)
	dock := Domain{Name: "com.apple.dock"}
	carried, errs := CarrySettings(context.Background(), f, map[Domain]string{dock: "/stage/com.apple.dock.plist"}, "/Users/alice", "/Users/bob", oldUUID)
	if len(errs) != 0 || carried != 1 {
		t.Fatalf("carried = %d, errs = %v, want 1 and none", carried, errs)
	}
	w := writes(f)
	if len(w) != 1 || !strings.Contains(w["tilesize"], "<integer>36</integer>") {
		t.Fatalf("writes = %v, want only tilesize 36", w)
	}
	if !f.CalledWith("defaults write com.apple.dock tilesize") || f.CalledWith("import") {
		t.Fatalf("calls = %+v, want a defaults write and never an import", f.Calls)
	}
	if !f.CalledWith("killall Dock Finder SystemUIServer ControlCenter") {
		t.Fatal("expected killall after carrying a setting")
	}
}

func TestCarrySettingsRewritesHome(t *testing.T) {
	f := settingsFake(
		map[string]string{
			"location": "<string>/Users/alice/Pictures/Shots</string>",
			"folder":   "<string>/Users/alice/Desktop</string>",
		},
		map[string]string{"folder": "<string>/Users/bob/Desktop</string>"},
	)
	shots := Domain{Name: "com.apple.screencapture"}
	carried, errs := CarrySettings(context.Background(), f, map[Domain]string{shots: "/stage/com.apple.screencapture.plist"}, "/Users/alice", "/Users/bob", oldUUID)
	if len(errs) != 0 || carried != 1 {
		t.Fatalf("carried = %d, errs = %v, want 1 and none", carried, errs)
	}
	got := writes(f)["location"]
	if !strings.Contains(got, "/Users/bob/Pictures/Shots") || strings.Contains(got, "/Users/alice") {
		t.Fatalf("location written as %q, want it moved to /Users/bob", got)
	}
}

func TestCarrySettingsUsesCurrentHostFile(t *testing.T) {
	f := settingsFake(map[string]string{"com.apple.trackpad.scaling": "<real>1.5</real>"}, map[string]string{})
	byHost := Domain{Name: "NSGlobalDomain", CurrentHost: true}
	path := "/stage/ByHost/.GlobalPreferences." + oldUUID + ".plist"
	if _, errs := CarrySettings(context.Background(), f, map[Domain]string{byHost: path}, "/Users/alice", "/Users/bob", oldUUID); len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	for _, want := range [][]string{
		{"plutil -convert xml1 -o -", path},
		{`plutil -extract com\.apple\.trackpad\.scaling xml1 -o -`, path},
		{"defaults -currentHost export NSGlobalDomain -"},
		{"defaults -currentHost write NSGlobalDomain com.apple.trackpad.scaling"},
	} {
		if !f.CalledWith(want...) {
			t.Errorf("no call with %q in %+v", want, f.Calls)
		}
	}
}

func TestCarrySettingsKeepsGoingAfterAFailedDomain(t *testing.T) {
	f := &run.Fake{Default: run.Response{Err: errors.New("unreadable")}}
	dock := Domain{Name: "com.apple.dock"}
	finder := Domain{Name: "com.apple.finder"}
	carried, errs := CarrySettings(context.Background(), f, map[Domain]string{dock: "/a", finder: "/b"}, "/Users/alice", "/Users/bob", oldUUID)
	if carried != 0 || len(errs) != 2 {
		t.Fatalf("carried = %d, errs = %v, want 0 and one per domain", carried, errs)
	}
	if f.CalledWith("killall") {
		t.Fatal("nothing carried, nothing to reload")
	}
}
