//go:build linux

package keyring

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const supported = true

// defaultSecurityBin is the ABSOLUTE path to secret-tool, the libsecret CLI —
// the Linux analogue of the Darwin backend's /usr/bin/security. It is the
// standard FHS install location on Debian/Ubuntu/Fedora (package
// libsecret-tools / libsecret). Unlike macOS, where /usr/bin/security is
// guaranteed by the OS, Linux distros can and do install elsewhere — use
// WithSecurityBin to point at a different path in production, not just
// tests, if this default misses.
const defaultSecurityBin = "/usr/bin/secret-tool"

// secretToolProbePath is the path backendReachable stats to decide whether
// secret-tool is installed. A var, not the defaultSecurityBin const
// directly, so tests can point it at a fixture without needing a real
// install at /usr/bin/secret-tool.
var secretToolProbePath = defaultSecurityBin

// backendReachable reports whether a real Secret Service is likely usable:
// the secret-tool binary exists at the default path AND a D-Bus session bus
// address is published. Both are best-effort signals, not proof — see
// Supported's doc comment and the README's Linux section for why this
// matters on a headless box.
func backendReachable() bool {
	if _, err := os.Stat(secretToolProbePath); err != nil {
		return false
	}
	return os.Getenv("DBUS_SESSION_BUS_ADDRESS") != ""
}

// Supported reports whether THIS store's configured backend is usable: the
// store's secret-tool binary (default, or a WithSecurityBin override) exists
// and a D-Bus session bus address is published. The package-level Supported()
// probes only the default install path — a store built with WithSecurityBin
// must use this method for its override to be honored.
func (s *Store) Supported() bool {
	if disabled() {
		return false
	}
	if _, err := os.Stat(s.securityBin); err != nil {
		return false
	}
	return os.Getenv("DBUS_SESSION_BUS_ADDRESS") != ""
}

// notFoundExitStderrEmpty reports whether a secret-tool failure is a
// CONFIRMED absence rather than any other error.
//
// secret-tool has no dedicated not-found exit code — unlike Darwin's
// `security` (exit 44), libsecret's CLI (tool/secret-tool.c) exits 1 for
// BOTH "no such secret" and any real failure (D-Bus unreachable, locked
// collection, denied access). The only distinguishing signal is stderr:
// the lookup/clear paths print nothing on confirmed absence and print an
// error message ("%s: %s\n", prog, error->message) on every other failure.
// So: exit-1-with-empty-stderr is treated as confirmed absence, and
// anything else (non-empty stderr, or a killed/timed-out process handled
// separately by the caller) is ErrUnreadable. This is a weaker signal than
// the Darwin backend's dedicated exit code — documented as such, here and
// in the README.
func notFoundExitStderrEmpty(err error, stderr string) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	// Exit 1 specifically: any other status (usage errors, killed-by-signal's
	// -1) is not the documented confirmed-absence shape even with empty
	// stderr, and must classify as ErrUnreadable, never ErrNotFound.
	if exitErr.ExitCode() != 1 {
		return false
	}
	return strings.TrimSpace(stderr) == ""
}

// get reads one secret via `secret-tool lookup`. See notFoundExitStderrEmpty
// for how a confirmed-absent lookup is told apart from any other failure.
func (s *Store) get(account string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.securityBin, "lookup", "service", s.service, "account", account)
	cmd.WaitDelay = time.Second
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		// A timeout kills the process before it can classify itself as
		// absent-vs-error; treat it as unreadable regardless of what (if
		// anything) landed in stderr — never mistake a wedged D-Bus call for
		// confirmed absence.
		if ctx.Err() != nil {
			return "", fmt.Errorf("keyring: reading %q under service %q: %w", account, s.service, ErrUnreadable)
		}
		if notFoundExitStderrEmpty(err, errBuf.String()) {
			return "", fmt.Errorf("keyring: %q %w under service %q", account, ErrNotFound, s.service)
		}
		return "", fmt.Errorf("keyring: reading %q under service %q: %w", account, s.service, ErrUnreadable)
	}
	// secret-tool writes the raw secret bytes to stdout with no trailing
	// newline when stdout is not a tty (tool/secret-tool.c
	// write_password_stdout only appends "\n" for an interactive terminal) —
	// unlike Darwin's `-w`, there is nothing to trim here.
	return string(out), nil
}

// write stores value under account via `secret-tool store`, which replaces
// any existing item under the same attributes (libsecret's simple-API
// store always overwrites — there is no secret-tool flag to fail instead).
// The secret is piped on stdin, never placed on argv: secret-tool detects a
// non-tty stdin and reads raw bytes to EOF (tool/secret-tool.c
// read_password_stdin), so it must not be given a trailing newline either.
func (s *Store) write(account, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	label := s.service + "/" + account
	cmd := exec.CommandContext(ctx, s.securityBin, "store", "--label", label, "service", s.service, "account", account)
	cmd.WaitDelay = time.Second
	cmd.Stdin = strings.NewReader(value)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if e := strings.TrimSpace(errBuf.String()); e != "" {
			return fmt.Errorf("keyring: storing %q: %w: %s", account, ErrUnreadable, e)
		}
		return fmt.Errorf("keyring: storing %q: %w", account, ErrUnreadable)
	}
	return nil
}

// writeIfAbsent stores value under account only if get first reports
// ErrNotFound. Unlike the Darwin backend's writeIfAbsent (which relies on
// `security`'s own confirmed duplicate-item failure), secret-tool's store
// always overwrites — there is no CLI primitive to fail on an existing
// item. This is therefore check-then-act, not atomic: a writer landing in
// the gap between the lookup and the store can still race this one. It
// closes the common case (bootstrap, token refresh) but is a weaker
// guarantee than the Darwin backend's — documented here and in the README.
func (s *Store) writeIfAbsent(account, value string) error {
	_, err := s.get(account)
	switch {
	case err == nil:
		return fmt.Errorf("keyring: %q %w under service %q", account, ErrExists, s.service)
	case errors.Is(err, ErrNotFound):
		return s.write(account, value)
	default:
		return fmt.Errorf("keyring: storing %q: presence check failed: %w", account, err)
	}
}

// delete removes one item via `secret-tool clear`. Classification mirrors
// get: exit 1 with empty stderr is a CONFIRMED absence (ErrNotFound);
// anything else, including a timeout, is ErrUnreadable.
func (s *Store) delete(account string) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.securityBin, "clear", "service", s.service, "account", account)
	cmd.WaitDelay = time.Second
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("keyring: deleting %q under service %q: %w", account, s.service, ErrUnreadable)
		}
		if notFoundExitStderrEmpty(err, errBuf.String()) {
			return fmt.Errorf("keyring: %q %w under service %q", account, ErrNotFound, s.service)
		}
		return fmt.Errorf("keyring: deleting %q under service %q: %w", account, s.service, ErrUnreadable)
	}
	return nil
}

// List returns ErrUnsupported on Linux: secret-tool's `search` has no clean
// "every item under one service" enumeration the way Darwin's
// `dump-keychain` does without a schema — deliberately out of scope for
// this backend. See DumpItems, DumpDuplicates.
func (s *Store) List(_ context.Context) ([]Item, error) {
	return nil, fmt.Errorf("keyring: listing service %q: %w", s.service, ErrUnsupported)
}

// DumpItems returns ErrUnsupported on Linux, matching List — see List's
// doc comment for why enumeration is out of scope for the secret-tool
// backend.
func DumpItems(_ context.Context, opts ...Option) ([]ServiceItem, error) {
	if _, err := New("keyring-dump", opts...); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("keyring: listing items: %w", ErrUnsupported)
}

// DumpDuplicates returns ErrUnsupported on Linux, matching List. It still
// validates service/opts via New so a caller sees the same argument errors
// on every platform.
func DumpDuplicates(_ context.Context, service string, opts ...Option) ([]DuplicateGroup, error) {
	if _, err := New(service, opts...); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("keyring: listing service %q: %w", service, ErrUnsupported)
}
