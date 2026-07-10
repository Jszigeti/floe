package notify

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

func Send(ctx context.Context, r run.Runner, title, msg string) error {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		return strings.ReplaceAll(s, `"`, `\"`)
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, esc(msg), esc(title))
	_, err := r.Run(ctx, run.Cmd{Name: "osascript", Args: []string{"-e", script}})
	return err
}
