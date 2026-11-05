package imap

import (
	"context"
	"errors"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
)

const commandTimeout = 30 * time.Second

// command bounds the whole exchange, including waiting for the first response
// byte. Closing the transport also interrupts library reads and writes.
func (x *connection) command(ctx context.Context, code string, run func(context.Context) error) error {
	commandCtx, cancel := context.WithTimeout(ctx, x.timeout)
	defer cancel()
	closed := make(chan struct{})
	stop := context.AfterFunc(commandCtx, func() {
		x.conn.Close()
		close(closed)
	})
	err := run(commandCtx)
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
		// The IMAP parser wraps decoder failures as text. Preserve the history
		// transport's size failure without inspecting provider-controlled text.
		if limited, ok := x.conn.(*readLimitConn); ok && limited.exceeded.Load() {
			return fault.New("sync_page_too_large")
		}
		var rejected *protocol.Error
		if errors.As(err, &rejected) && rejected.Code != "" {
			logging.RequestResponseCode(ctx, string(rejected.Code))
		}
		return fault.From(err, code)
	}
	return nil
}
