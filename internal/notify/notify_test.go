package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestSend(t *testing.T) {
	tests := []struct {
		name       string
		msg        string
		wantSubstr string
	}{
		{
			name:       "builds AppleScript notification",
			msg:        "backup failed",
			wantSubstr: `display notification "backup failed" with title "floe"`,
		},
		{
			name:       "escapes quotes",
			msg:        `error: "boom"`,
			wantSubstr: `\"boom\"`,
		},
		{
			name:       "escapes backslashes",
			msg:        `path: C:\path`,
			wantSubstr: `C:\\path`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &run.Fake{}
			if err := Send(context.Background(), f, "floe", tt.msg); err != nil {
				t.Fatal(err)
			}
			c := f.Calls[0]
			if c.Name != "osascript" || c.Args[0] != "-e" {
				t.Fatalf("call = %+v, want osascript -e", c)
			}
			if got := c.Args[1]; !strings.Contains(got, tt.wantSubstr) {
				t.Errorf("script = %q, want substring %q", got, tt.wantSubstr)
			}
		})
	}
}
