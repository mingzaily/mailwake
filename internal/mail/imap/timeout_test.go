package imap

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

type serverOptions struct {
	silent      string
	caps        string
	loginCaps   bool
	requireID   bool
	rejectID    bool
	openFailure string
}

// silentServer answers ordinary commands and deliberately withholds one response.
func silentServer(t *testing.T, silent string) (*Source, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	source, _, seen, ended := scriptedServer(t, serverOptions{silent: silent, caps: "IMAP4rev1 IDLE ID"})
	return source, seen, ended
}

func scriptedServer(t *testing.T, options serverOptions) (*Source, <-chan string, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	cert := httptest.NewTLSServer(nil)
	pool := x509.NewCertPool()
	pool.AddCert(cert.Certificate())
	listener, err := tls.Listen("tcp", "127.0.0.1:0", cert.TLS.Clone())
	cert.Close()
	if err != nil {
		t.Fatal(err)
	}
	seen, ended := make(chan struct{}, 1), make(chan struct{})
	commands := make(chan string, 64)
	silent := options.silent
	accepted := make(chan net.Conn, 1)
	go func() {
		defer close(ended)
		defer close(commands)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		defer conn.Close()
		if silent != "GREETING" {
			fmt.Fprint(conn, "* OK [CAPABILITY IMAP4rev1] ready\r\n")
		}
		reader := bufio.NewReader(conn)
		idleTag := ""
		identified, selected := false, false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			commands <- strings.TrimSpace(line)
			parts := strings.Fields(line)
			command, tag := parts[0], idleTag
			if len(parts) > 1 {
				tag, command = parts[0], parts[1]
			}
			if command == "UID" && len(parts) > 2 {
				command = parts[2]
			}
			if (command == silent && (command != "NOOP" || selected)) || silent == "GREETING" {
				select {
				case seen <- struct{}{}:
				default:
				}
				continue
			}
			switch command {
			case "LOGIN":
				if options.loginCaps {
					fmt.Fprintf(conn, "%s OK [CAPABILITY %s] logged in\r\n", tag, options.caps)
				} else {
					fmt.Fprintf(conn, "%s OK logged in\r\n", tag)
				}
			case "ID":
				if options.rejectID {
					fmt.Fprintf(conn, "%s BAD private server explanation\r\n", tag)
				} else {
					identified = true
					fmt.Fprintf(conn, "* ID NIL\r\n%s OK identified\r\n", tag)
				}
			case "LIST":
				if options.requireID && !identified {
					fmt.Fprintf(conn, "%s NO Unsafe Login\r\n", tag)
					continue
				}
				fmt.Fprintf(conn, "* LIST () \"/\" \"Clients\"\r\n%s OK listed\r\n", tag)
			case "EXAMINE":
				if options.requireID && !identified {
					fmt.Fprintf(conn, "%s NO SELECT Unsafe Login\r\n", tag)
					continue
				}
				if options.openFailure != "" {
					fmt.Fprintf(conn, "%s NO %s\r\n", tag, options.openFailure)
					continue
				}
				selected = true
				fmt.Fprintf(conn, "* 0 EXISTS\r\n* OK [UIDVALIDITY 1] epoch\r\n* OK [UIDNEXT 1] next\r\n%s OK [READ-ONLY] selected\r\n", tag)
			case "CAPABILITY":
				fmt.Fprintf(conn, "* CAPABILITY %s\r\n%s OK capabilities\r\n", options.caps, tag)
			case "SEARCH":
				fmt.Fprintf(conn, "* SEARCH 1\r\n%s OK search\r\n", tag)
			case "IDLE":
				idleTag = tag
				fmt.Fprint(conn, "+ idling\r\n")
			case "DONE":
				fmt.Fprintf(conn, "%s OK idle ended\r\n", tag)
			default:
				fmt.Fprintf(conn, "%s OK completed\r\n", tag)
			}
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case conn := <-accepted:
			conn.Close()
		default:
		}
		select {
		case <-ended:
		case <-time.After(2 * time.Second):
			t.Error("server connection did not close")
		}
	})
	source := New("127.0.0.1", listener.Addr().(*net.TCPAddr).Port, "test", "test", slog.Default())
	source.tlsConfig.RootCAs = pool
	source.timeout = 150 * time.Millisecond
	return source, commands, seen, ended
}

func TestSilentCommandsTimeOutAndCloseTransport(t *testing.T) {
	if commandTimeout != 30*time.Second {
		t.Fatal("production IMAP command timeout must be 30 seconds")
	}
	for _, command := range []string{"GREETING", "LOGIN", "EXAMINE", "CAPABILITY", "ID", "LIST", "NOOP", "SEARCH", "FETCH", "IDLE", "DONE"} {
		t.Run(command, func(t *testing.T) {
			source, _, ended := silentServer(t, command)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var err error
			if command == "LIST" {
				_, err = source.Folders(ctx)
			} else {
				sess, openErr := source.Open(ctx, "Clients")
				switch command {
				case "NOOP", "SEARCH", "FETCH", "IDLE", "DONE":
					if openErr != nil {
						t.Fatalf("opening session before %s: %v", command, openErr)
					}
				}
				err = openErr
				if err == nil {
					defer sess.Close()
					switch command {
					case "NOOP", "SEARCH", "FETCH":
						_, err = sess.Poll(ctx, `{"validity":1,"next":1}`, false)
					case "IDLE", "DONE":
						err = sess.Wait(ctx, 5*time.Millisecond)
					default:
						t.Fatal("expected opening the session to time out")
					}
				}
			}
			if ctx.Err() != nil {
				t.Fatal("operation exceeded the adapter timeout")
			}
			if err == nil || fault.From(err, "unknown_error").Code != "imap_command_timeout" {
				t.Fatalf("expected command timeout, got %v", err)
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("timeout left the transport open")
			}
		})
	}
}

func TestOperationCancellationClosesTransport(t *testing.T) {
	for _, command := range []string{"NOOP", "IDLE", "DONE"} {
		t.Run(command, func(t *testing.T) {
			source, seen, ended := silentServer(t, command)
			source.timeout = time.Minute
			sess, err := source.Open(t.Context(), "Clients")
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if command == "NOOP" {
					_, err := sess.Poll(ctx, `{"validity":1,"next":1}`, false)
					done <- err
				} else {
					done <- sess.Wait(ctx, time.Millisecond)
				}
			}()
			select {
			case <-seen:
			case <-time.After(time.Second):
				t.Fatal("command was not sent")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation did not unblock the command")
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("cancellation left transport open")
			}
		})
	}
}

func TestCommandTimeoutAllowsHealthyIdleWait(t *testing.T) {
	source, _, _ := silentServer(t, "")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	sess, err := source.Open(ctx, "Clients")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	// Drain SELECT updates before waiting longer than the command timeout.
	if _, err := sess.Poll(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := sess.Wait(ctx, 2*source.timeout); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 2*source.timeout {
		t.Fatal("IDLE ended before the reconciliation interval")
	}
	if _, err := sess.Poll(ctx, "", false); err != nil {
		t.Fatalf("completed command deadline closed a healthy connection: %v", err)
	}
}
