package keychain

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

const (
	service = "floe"
	account = "restic"
)

func Generate() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func Set(ctx context.Context, r run.Runner, pass string) error {
	_, err := r.Run(ctx, run.Cmd{Name: "security", Args: []string{
		"add-generic-password", "-U", "-s", service, "-a", account, "-w", pass,
	}})
	return err
}

func Get(ctx context.Context, r run.Runner) (string, error) {
	out, err := r.Run(ctx, run.Cmd{Name: "security", Args: []string{
		"find-generic-password", "-s", service, "-a", account, "-w",
	}})
	if err != nil {
		return "", fmt.Errorf("no floe passphrase in keychain — run `floe init` first: %w", err)
	}
	return strings.TrimSpace(out), nil
}
