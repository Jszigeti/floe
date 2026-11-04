package icloud

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func forcePolicyOff(t *testing.T) {
	t.Helper()
	orig := datalessPolicy()
	t.Cleanup(func() {
		if err := setDatalessPolicy(orig); err != nil {
			t.Errorf("restoring policy %d: %v", orig, err)
		}
	})
	if err := setDatalessPolicy(datalessOff); err != nil {
		t.Fatal(err)
	}
}

func TestAllowDatalessDownloadsTurnsPolicyOn(t *testing.T) {
	forcePolicyOff(t)
	if err := AllowDatalessDownloads(); err != nil {
		t.Fatal(err)
	}
	if got := datalessPolicy(); got != datalessOn {
		t.Fatalf("policy = %d, want %d", got, datalessOn)
	}
}

// restic only sees the policy if a child process inherits it: the test re-runs itself as that child.
func TestChildProcessInheritsDatalessPolicy(t *testing.T) {
	if os.Getenv("FLOE_POLICY_CHILD") == "1" {
		fmt.Print(datalessPolicy())
		os.Exit(0)
	}
	forcePolicyOff(t)
	if err := AllowDatalessDownloads(); err != nil {
		t.Fatal(err)
	}
	out, err := run.ExecRunner{}.Run(context.Background(), run.Cmd{
		Name: os.Args[0],
		Args: []string{"-test.run=^TestChildProcessInheritsDatalessPolicy$"},
		Env:  []string{"FLOE_POLICY_CHILD=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != strconv.Itoa(datalessOn) {
		t.Fatalf("child policy = %q, want %d", out, datalessOn)
	}
}

func TestSetDatalessPolicyRejectsUnknownPolicy(t *testing.T) {
	if err := setDatalessPolicy(99); err == nil {
		t.Fatal("want error for unknown policy")
	}
}
