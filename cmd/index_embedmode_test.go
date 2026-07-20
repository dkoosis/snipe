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

// TestResolveEmbedMode pins two behaviors of the probe gate:
//   - the off / legacy-embed=false short-circuits do ZERO keychain work — the
//     probe (which can exec /usr/bin/security with a 10s timeout on a locked
//     keychain) must never run for a mode that cannot use embeddings anyway,
//     or heal's 15s backstop and any headless index run would eat the delay;
//   - the non-off modes still gate on the probe — no credentials forces off,
//     after exactly one probe call.
func TestResolveEmbedMode(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		legacyEmbed  bool
		hasCreds     bool
		wantMode     string
		wantProbeNum int
	}{
		{"off_skips_probe_legacy_true", embedModeOff, true, true, embedModeOff, 0},
		{"off_skips_probe_legacy_false", embedModeOff, false, true, embedModeOff, 0},
		{"legacy_embed_false_auto_skips_probe", embedModeAuto, false, true, embedModeOff, 0},
		{"no_credentials_forces_off", embedModeBatch, true, false, embedModeOff, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := swapCredentialsProbe(t, tt.hasCreds)
			if got := resolveEmbedMode(tt.mode, tt.legacyEmbed, nil); got != tt.wantMode {
				t.Errorf("resolveEmbedMode(%q, legacy=%v) = %q, want %q",
					tt.mode, tt.legacyEmbed, got, tt.wantMode)
			}
			if *calls != tt.wantProbeNum {
				t.Errorf("credentials probe called %d times, want %d", *calls, tt.wantProbeNum)
			}
		})
	}
}
