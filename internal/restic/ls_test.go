package restic

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

// Shapes printed by restic 0.19.1 `ls --recursive --json`, trimmed to the fields that matter.
const lsOutput = `{"time":"2026-11-16T14:19:49+01:00","paths":["/Users/x"],"id":"e1e1","short_id":"e1e1ce62","message_type":"snapshot","struct_type":"snapshot"}
{"name":"x","type":"dir","path":"/Users/x","uid":501,"mode":2147484141,"message_type":"node","struct_type":"node"}
{"name":"a.txt","type":"file","path":"/Users/x/a.txt","uid":501,"size":6,"mode":420,"message_type":"node","struct_type":"node"}
{"name":"link","type":"symlink","path":"/Users/x/link","uid":501,"mode":134218221,"message_type":"node","struct_type":"node"}
`

// streamingFake writes out to the command's Stdout in small chunks that cut lines in the middle,
// and returns nothing as captured output, so List can only see what was streamed.
func streamingFake(out string, err error) *run.Fake {
	return &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Stdout == nil {
			return out, errors.New("Stdout not set")
		}
		for len(out) > 0 {
			n := min(7, len(out))
			_, _ = io.WriteString(c.Stdout, out[:n])
			out = out[n:]
		}
		return "", err
	}}
}

func collect(t *testing.T, f *run.Fake) ([]Entry, error) {
	t.Helper()
	var got []Entry
	err := newTestClient(f).List(context.Background(), "e1e1ce62", "/Users/x", func(e Entry) { got = append(got, e) })
	return got, err
}

func TestListStreamsEntries(t *testing.T) {
	want := []Entry{
		{Path: "/Users/x", Type: "dir"},
		{Path: "/Users/x/a.txt", Type: "file", Size: 6},
		{Path: "/Users/x/link", Type: "symlink"},
	}
	wantArgs := []string{"ls", "--recursive", "--json", "e1e1ce62", "/Users/x"}
	for _, tc := range []struct {
		name, out string
	}{
		{"every node", lsOutput},
		{"last line without newline", strings.TrimSuffix(lsOutput, "\n")},
		{"malformed lines skipped", "not json\n" + lsOutput + "\n{\"struct_type\":\"node\",\"path\":\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := streamingFake(tc.out, nil)
			got, err := collect(t, f)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("entries = %+v, want %+v", got, want)
			}
			if !reflect.DeepEqual(f.Calls[0].Args, wantArgs) {
				t.Errorf("args = %q, want %q", f.Calls[0].Args, wantArgs)
			}
		})
	}
}

func TestListReturnsRunnerError(t *testing.T) {
	if _, err := collect(t, streamingFake("", errors.New("repo unreachable"))); err == nil {
		t.Fatal("want error when restic fails")
	}
}
