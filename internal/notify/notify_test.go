package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestSendBuildsAppleScript(t *testing.T) {
	f := &run.Fake{}
	if err := Send(context.Background(), f, "floe", "backup failed"); err != nil {
		t.Fatal(err)
	}
	c := f.Calls[0]
	if c.Name != "osascript" || c.Args[0] != "-e" {
		t.Fatalf("call = %+v", c)
	}
	if !strings.Contains(c.Args[1], `display notification "backup failed" with title "floe"`) {
		t.Fatalf("script = %q", c.Args[1])
	}
}

func TestSendEscapesQuotes(t *testing.T) {
	f := &run.Fake{}
	Send(context.Background(), f, "floe", `error: "boom"`)
	if !strings.Contains(f.Calls[0].Args[1], `\"boom\"`) {
		t.Fatalf("script = %q", f.Calls[0].Args[1])
	}
}
