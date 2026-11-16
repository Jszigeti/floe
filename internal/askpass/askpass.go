// Package askpass is the helper sudo runs as SUDO_ASKPASS during a floe run: it prints the
// administrator password the running floe serves on a socket only the user's account can open.
package askpass

import (
	"errors"
	"io"
	"net"
	"time"
)

// SocketEnv names the socket a floe started as askpass helper reads the password from.
const SocketEnv = "FLOE_ASKPASS_SOCKET"

// timeout bounds the exchange, so a helper whose floe is gone never holds sudo.
const timeout = 5 * time.Second

// Answer prints what socket serves followed by a newline, as sudo expects from an askpass program,
// and nothing when it serves nothing.
func Answer(socket string, w io.Writer) error {
	conn, err := net.DialTimeout("unix", socket, timeout)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	pass, err := io.ReadAll(conn)
	if err != nil {
		return err
	}
	// Printing nothing, not an empty line, makes sudo stop asking instead of trying an empty password.
	if len(pass) == 0 {
		return errors.New("no password served")
	}
	_, err = w.Write(append(pass, '\n'))
	return err
}
