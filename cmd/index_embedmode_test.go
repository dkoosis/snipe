package cmd

import "testing"

// swapCredentialsProbe replaces the credentials probe with a counting stub and
// restores it on cleanup.
func swapCredentialsProbe(t *testing.T, result bool) *int {
	t.Helper()
	calls := 0
	orig := credentialsProbe
	credentialsProbe = func() bool {
		calls++
		return result
	}
	t.Cleanup(func() { credentialsProbe = orig })
	return &calls
}

// TestResolveEmbedMode_OffSkipsCredentialsProbe proves --embed-mode=off does
// zero keychain work: the probe (which can exec /usr/bin/security with a 10s
// timeout on a locked keychain) must never run, or heal's 15s backstop and any
// headless index run would eat the delay for a mode that cannot use
// embeddings anyway.
func TestResolveEmbedMode_OffSkipsCredentialsProbe(t *testing.T) {
	calls := swapCredentialsProbe(t, true)

	if got := resolveEmbedMode(embedModeOff, true, nil); got != embedModeOff {
		t.Fatalf("resolveEmbedMode(off) = %q, want %q", got, embedModeOff)
	}
	if got := resolveEmbedMode(embedModeOff, false, nil); got != embedModeOff {
		t.Fatalf("resolveEmbedMode(off, legacy=false) = %q, want %q", got, embedModeOff)
	}
	if *calls != 0 {
		t.Fatalf("credentials probe called %d times for mode=off, want 0", *calls)
	}
}

// TestResolveEmbedMode_LegacyEmbedFalseSkipsCredentialsProbe covers the
// --embed=false + auto short-circuit: also zero probe calls.
func TestResolveEmbedMode_LegacyEmbedFalseSkipsCredentialsProbe(t *testing.T) {
	calls := swapCredentialsProbe(t, true)

	if got := resolveEmbedMode(embedModeAuto, false, nil); got != embedModeOff {
		t.Fatalf("resolveEmbedMode(auto, legacy=false) = %q, want %q", got, embedModeOff)
	}
	if *calls != 0 {
		t.Fatalf("credentials probe called %d times, want 0", *calls)
	}
}

// TestResolveEmbedMode_NoCredentialsForcesOff proves the probe still gates the
// non-off modes: no credentials means off, after exactly one probe call.
func TestResolveEmbedMode_NoCredentialsForcesOff(t *testing.T) {
	calls := swapCredentialsProbe(t, false)

	if got := resolveEmbedMode(embedModeBatch, true, nil); got != embedModeOff {
		t.Fatalf("resolveEmbedMode(batch, no creds) = %q, want %q", got, embedModeOff)
	}
	if *calls != 1 {
		t.Fatalf("credentials probe called %d times, want 1", *calls)
	}
}
