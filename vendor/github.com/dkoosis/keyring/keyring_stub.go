//go:build !darwin && !linux

package keyring

import (
	"context"
	"fmt"
)

const supported = false

// backendReachable is irrelevant here: Supported() short-circuits on
// supported=false before this would matter. Defined for symmetry with the
// darwin and linux backends, each of which supplies its own.
func backendReachable() bool { return false }

// Supported on a stub build is always false — no backend is compiled in,
// regardless of the store's configuration. Per-store sibling of the
// package-level Supported(), defined for cross-platform API symmetry.
func (s *Store) Supported() bool { return false }

// defaultSecurityBin is unused on non-darwin, non-linux builds; present so
// Store construction is platform-independent.
const defaultSecurityBin = ""

func (s *Store) get(account string) (string, error) {
	return "", fmt.Errorf("keyring: reading %q under service %q: %w", account, s.service, ErrUnsupported)
}

func (s *Store) write(account, _ string) error {
	return fmt.Errorf("keyring: storing %q under service %q: %w", account, s.service, ErrUnsupported)
}

func (s *Store) writeIfAbsent(account, _ string) error {
	return fmt.Errorf("keyring: storing %q under service %q: %w", account, s.service, ErrUnsupported)
}

func (s *Store) delete(account string) error {
	return fmt.Errorf("keyring: deleting %q under service %q: %w", account, s.service, ErrUnsupported)
}

// List returns ErrUnsupported on every non-darwin build — no keychain
// backend is compiled in. LoadManifest, by contrast, has no build tag and
// works everywhere; only the enumeration calls that hit `security` are
// platform-gated.
func (s *Store) List(_ context.Context) ([]Item, error) {
	return nil, fmt.Errorf("keyring: listing service %q: %w", s.service, ErrUnsupported)
}

// DumpItems returns ErrUnsupported on every non-darwin build, matching List.
func DumpItems(_ context.Context, opts ...Option) ([]ServiceItem, error) {
	if _, err := New("keyring-dump", opts...); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("keyring: listing items: %w", ErrUnsupported)
}

// DumpDuplicates returns ErrUnsupported on every non-darwin build. It still
// validates service/opts via New so a caller sees the same argument errors
// on every platform, not just darwin.
func DumpDuplicates(_ context.Context, service string, opts ...Option) ([]DuplicateGroup, error) {
	if _, err := New(service, opts...); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("keyring: listing service %q: %w", service, ErrUnsupported)
}
