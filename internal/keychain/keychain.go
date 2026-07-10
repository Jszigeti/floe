package keychain

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

const (
	service = "floe"
	account = "restic"
)

var ErrNotFound = errors.New("no floe passphrase in keychain — run `floe init` first")

func Generate() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// security(1) only takes the secret via argv; ps exposure is momentary and same-user only.
func Set(ctx context.Context, r run.Runner, pass string) error {
	_, err := r.Run(ctx, run.Cmd{Name: "security", Args: []string{
		"add-generic-password", "-U", "-s", service, "-a", account, "-w", pass,
	}})
	if err != nil {
		// The runner's error embeds argv, which would leak pass into logs — never wrap it.
		return errors.New("storing passphrase in keychain failed — is the keychain locked?")
	}
	return nil
}

func Get(ctx context.Context, r run.Runner) (string, error) {
	out, err := r.Run(ctx, run.Cmd{Name: "security", Args: []string{
		"find-generic-password", "-s", service, "-a", account, "-w",
	}})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "could not be found") {
			return "", fmt.Errorf("%w: %v", ErrNotFound, err)
		}
		return "", fmt.Errorf("reading keychain: %w", err)
	}
	return strings.TrimSpace(out), nil
}
