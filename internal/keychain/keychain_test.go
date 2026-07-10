package keychain

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestGenerateIsLongAndRandom(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Generate()
	if len(a) < 32 || a == b {
		t.Fatalf("a = %q, b = %q", a, b)
	}
}

func TestSetCallsSecurity(t *testing.T) {
	f := &run.Fake{}
	if err := Set(context.Background(), f, "s3cret"); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("security", "add-generic-password", "-s", "floe", "s3cret") {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestSetScrubsPassphraseFromErrors(t *testing.T) {
	pass := "s3cret-passphrase"
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		return "", fmt.Errorf("exit status 1: security add-generic-password -U -s floe -a restic -w %s", pass)
	}}
	err := Set(context.Background(), f, pass)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), pass) {
		t.Fatalf("err leaks passphrase: %v", err)
	}
}

func TestGetTrimsOutput(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"security find-generic-password -s floe -a restic -w": {Out: "s3cret\n"},
	}}
	got, err := Get(context.Background(), f)
	if err != nil || got != "s3cret" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestGetClassifiesErrors(t *testing.T) {
	notFoundText := errors.New("security find-generic-password ...: exit status 44: The specified item could not be found.")
	cases := []struct {
		name         string
		err          error
		wantNotFound bool
		wantMsg      string
	}{
		{"security not found message", notFoundText, true, ""},
		{"fs.ErrNotExist", fs.ErrNotExist, true, ""},
		{"other error", errors.New("keychain locked"), false, "reading keychain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &run.Fake{Handler: func(run.Cmd) (string, error) { return "", tc.err }}
			_, err := Get(context.Background(), f)
			if err == nil {
				t.Fatal("want error")
			}
			if got := errors.Is(err, ErrNotFound); got != tc.wantNotFound {
				t.Fatalf("errors.Is(err, ErrNotFound) = %v, want %v (err = %v)", got, tc.wantNotFound, err)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want to contain %q", err, tc.wantMsg)
			}
		})
	}
}
