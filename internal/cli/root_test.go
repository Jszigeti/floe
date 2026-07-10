package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootPrintsVersion(t *testing.T) {
	cmd := Root(Deps{Version: "1.2.3"})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1.2.3") {
		t.Fatalf("version output = %q, want it to contain 1.2.3", out.String())
	}
}
