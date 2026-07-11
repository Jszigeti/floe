package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestPassphrasePrintsFromKeychain(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"security find-generic-password -s floe -a restic -w": {Out: "s3cret\n"},
	}}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"passphrase"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "s3cret") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestPassphraseFailsWithoutKeychainEntry(t *testing.T) {
	f := &run.Fake{Default: run.Response{Err: errors.New("not found")}}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"passphrase"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when no keychain entry exists")
	}
}
