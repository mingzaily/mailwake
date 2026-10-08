package imap

import (
	"context"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

const commandTimeout = 30 * time.Second

// command bounds the whole exchange, including waiting for the first response
// byte. Closing the transport also interrupts library reads and writes.
func (x *connection) command(ctx context.Context, code string, run func() error) error {
	commandCtx, cancel := context.WithTimeout(ctx, x.timeout)
	defer cancel()
	closed := make(chan struct{})
	stop := context.AfterFunc(commandCtx, func() {
		x.conn.Close()
		close(closed)
	})
	err := run()
	// Join an expired callback before another command can use this connection.
	if !stop() {
		<-closed
	}
	if commandCtx.Err() == context.DeadlineExceeded {
		return fault.New("imap_command_timeout")
	}
	if commandCtx.Err() != nil {
		return commandCtx.Err()
	}
	if err != nil {
		return fault.From(err, code)
	}
	return nil
}
