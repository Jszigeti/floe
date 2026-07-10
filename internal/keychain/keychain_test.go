package keychain

import (
	"context"
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

func TestGetTrimsOutput(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"security find-generic-password -s floe -a restic -w": {Out: "s3cret\n"},
	}}
	got, err := Get(context.Background(), f)
	if err != nil || got != "s3cret" {
		t.Fatalf("got %q, %v", got, err)
	}
}
