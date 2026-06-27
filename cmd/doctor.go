package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/embed"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/util"
)

// DoctorCheck represents a single diagnostic check.
type DoctorCheck struct {
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	Code        string `json:"code,omitempty"` // e.g. RG_MISSING, INDEX_CORRUPT, INDEX_STALE
	Message     string `json:"message,omitempty"`
	Remediation string `json:"remediation,omitempty"` // executable command, not prose
	Details     string `json:"details,omitempty"`
}

// Doctor error codes.
const (
	DoctorRGMissing        = "RG_MISSING"
	DoctorGOMissing        = "GO_MISSING"
	DoctorIndexMissing     = "INDEX_MISSING"
	DoctorIndexCorrupt     = "INDEX_CORRUPT"
	DoctorIndexStale       = "INDEX_STALE"
	DoctorOrphanedRefs     = "ORPHANED_REFS"
	DoctorEmbedAuthMissing = "EMBED_AUTH_MISSING"
)

// Common remediation commands.
const remediationReindex = "snipe index"

func runDoctor() error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	allOK := true
	var checks []DoctorCheck

	// Check ripgrep
	rgCheck := checkRipgrep()
	checks = append(checks, rgCheck)
	if !rgCheck.OK {
		allOK = false
	}

	// Check index
	indexCheck := checkIndex()
	checks = append(checks, indexCheck)
	if !indexCheck.OK {
		allOK = false
	}

	// Check Go toolchain
	goCheck := checkGoToolchain()
	checks = append(checks, goCheck)
	if !goCheck.OK {
		allOK = false
	}

	// Check embeddings credentials
	embedCheck := checkEmbeddings()
	checks = append(checks, embedCheck)

	// Check orphaned references (only if index exists)
	if indexCheck.OK {
		orphanCheck := checkOrphans()
		checks = append(checks, orphanCheck)
	}

	// Check index staleness
	if indexCheck.OK {
		staleCheck := checkStaleness()
		checks = append(checks, staleCheck)
	}

	// Check for root mismatch
	mismatchCheck := checkRootMismatch()
	checks = append(checks, mismatchCheck)
	if !mismatchCheck.OK {
		allOK = false
	}

	// Find repo root for meta (best-effort)
	cwd, _ := os.Getwd()
	repoRoot := util.FindProjectRoot(cwd)

	resp := output.Response[DoctorCheck]{
		Protocol: output.ProtocolVersion,
		Ok:       allOK,
		Results:  checks,
		Meta: output.Meta{
			Command:  cmdNameDoctor,
			RepoRoot: repoRoot,
			Ms:       w.Elapsed(),
			Total:    len(checks),
		},
	}

	return w.WriteResponse(resp)
}

func checkRipgrep() DoctorCheck {
	check := DoctorCheck{
		Name: "ripgrep",
	}

	path, err := exec.LookPath("rg")
	if err != nil {
		check.OK = false
		check.Code = DoctorRGMissing
		check.Message = "ripgrep (rg) not found"
		check.Remediation = "brew install ripgrep"
		check.Details = "Install from https://github.com/BurntSushi/ripgrep\n" +
			"  macOS: brew install ripgrep\n" +
			"  Ubuntu/Debian: apt install ripgrep\n" +
			"  Windows: choco install ripgrep"
		return check
	}

	// Get version
	out, err := exec.Command("rg", "--version").Output()
	if err != nil {
		check.OK = true
		check.Message = fmt.Sprintf("ripgrep found at %s (version unknown)", path)
		return check
	}

	version := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	check.OK = true
	check.Message = version
	check.Details = fmt.Sprintf("Path: %s", path)

	return check
}

func checkIndex() DoctorCheck {
	check := DoctorCheck{
		Name: cmdNameIndex,
	}

	// Find project root (look for .git directory)
	cwd, err := os.Getwd()
	if err != nil {
		check.OK = false
		check.Message = "could not determine working directory"
		return check
	}

	projectRoot := util.FindProjectRoot(cwd)
	if projectRoot == "" {
		check.OK = false
		check.Message = "not in a git repository or Go module"
		check.Details = "Run 'snipe index' from a directory containing .git or go.mod"
		return check
	}

	indexPath := store.DefaultIndexPath(projectRoot)
	if !store.Exists(indexPath) {
		check.OK = false
		check.Code = DoctorIndexMissing
		check.Message = "index not found"
		check.Remediation = remediationReindex
		check.Details = fmt.Sprintf("Expected at: %s", indexPath)
		return check
	}

	// Check integrity
	s, err := store.Open(indexPath)
	if err != nil {
		check.OK = false
		check.Code = DoctorIndexCorrupt
		check.Message = "could not open index"
		check.Remediation = remediationReindex
		check.Details = fmt.Sprintf("Path: %s\nError: %v", indexPath, err)
		return check
	}

	var integrityResult string
	if err := s.DB().QueryRow("PRAGMA integrity_check").Scan(&integrityResult); err != nil {
		s.Close()
		check.OK = false
		check.Code = DoctorIndexCorrupt
		check.Message = "integrity check failed"
		check.Remediation = remediationReindex
		check.Details = fmt.Sprintf("Path: %s\nError: %v", indexPath, err)
		return check
	}
	s.Close()

	if integrityResult != "ok" {
		check.OK = false
		check.Code = DoctorIndexCorrupt
		check.Message = "index is corrupt"
		check.Remediation = remediationReindex
		check.Details = fmt.Sprintf("Path: %s\nPRAGMA integrity_check: %s", indexPath, integrityResult)
		return check
	}

	// Check freshness
	info, err := os.Stat(indexPath)
	if err != nil {
		check.OK = false
		check.Message = "could not read index file"
		return check
	}

	age := time.Since(info.ModTime())
	check.OK = true
	check.Message = "index found"

	if age > 24*time.Hour {
		check.Details = fmt.Sprintf("Path: %s\nLast updated: %s ago (consider running 'snipe index' to refresh)",
			indexPath, formatDuration(age))
	} else {
		check.Details = fmt.Sprintf("Path: %s\nLast updated: %s ago",
			indexPath, formatDuration(age))
	}

	return check
}

func checkGoToolchain() DoctorCheck {
	check := DoctorCheck{
		Name: "go_toolchain",
	}

	goPath, err := exec.LookPath("go")
	if err != nil {
		check.OK = false
		check.Code = DoctorGOMissing
		check.Message = "Go toolchain not found"
		check.Remediation = "https://go.dev/dl/"
		return check
	}

	out, err := exec.Command("go", "version").Output()
	if err != nil {
		check.OK = true
		check.Message = fmt.Sprintf("go found at %s (version unknown)", goPath)
		return check
	}

	version := strings.TrimSpace(string(out))
	check.OK = true
	check.Message = version
	check.Details = fmt.Sprintf("Path: %s", goPath)
	return check
}

func checkEmbeddings() DoctorCheck {
	check := DoctorCheck{
		Name: "embeddings",
	}

	if embed.HasCredentials() {
		check.OK = true
		check.Message = "embedding credentials available"
	} else {
		check.OK = true // Not a failure, just informational
		check.Code = DoctorEmbedAuthMissing
		check.Message = "no embedding credentials (embeddings disabled)"
		check.Remediation = "add 'export SNIPE_VOYAGE_API_KEY=your-key' to ~/.trixi/secrets.env (or export directly)"
	}

	return check
}

func checkOrphans() DoctorCheck {
	check := DoctorCheck{
		Name: "orphaned_refs",
	}

	cwd, err := os.Getwd()
	if err != nil {
		check.OK = true
		check.Message = "skipped (no working directory)"
		return check
	}

	projectRoot := util.FindProjectRoot(cwd)
	if projectRoot == "" {
		check.OK = true
		check.Message = "skipped (not in git repo)"
		return check
	}

	indexPath := store.DefaultIndexPath(projectRoot)
	s, err := store.Open(indexPath)
	if err != nil {
		check.OK = true
		check.Message = "skipped (could not open index)"
		return check
	}
	defer s.Close()

	var orphanCount int
	err = s.DB().QueryRow(`SELECT COUNT(*) FROM refs WHERE symbol_id NOT IN (SELECT id FROM symbols)`).Scan(&orphanCount)
	if err != nil {
		check.OK = true
		check.Message = "skipped (query failed)"
		return check
	}

	if orphanCount > 0 {
		check.OK = false
		check.Code = DoctorOrphanedRefs
		check.Message = fmt.Sprintf("%d orphaned references found", orphanCount)
		check.Remediation = "snipe index --force"
	} else {
		check.OK = true
		check.Message = "no orphaned references"
	}

	return check
}

func checkStaleness() DoctorCheck {
	check := DoctorCheck{
		Name: "staleness",
	}

	cwd, err := os.Getwd()
	if err != nil {
		check.OK = true
		check.Message = "skipped (no working directory)"
		return check
	}

	projectRoot := util.FindProjectRoot(cwd)
	if projectRoot == "" {
		check.OK = true
		check.Message = "skipped (not in git repo)"
		return check
	}

	indexPath := store.DefaultIndexPath(projectRoot)
	s, err := store.Open(indexPath)
	if err != nil {
		check.OK = true
		check.Message = "skipped (could not open index)"
		return check
	}
	defer s.Close()

	state := query.CheckIndexState(s.DB(), projectRoot, Version)

	switch state {
	case output.IndexFresh:
		check.OK = true
		check.Message = "index is fresh"
	case output.IndexStale:
		check.OK = true // Degraded but not broken
		check.Code = DoctorIndexStale
		check.Message = "index is stale"
		check.Remediation = remediationReindex
	case output.IndexMissing, output.IndexNotUsed:
		check.OK = true
		check.Message = fmt.Sprintf("index state: %s", state)
	}

	return check
}

func checkRootMismatch() DoctorCheck {
	check := DoctorCheck{Name: "root-mismatch"}

	cwd, err := os.Getwd()
	if err != nil {
		check.OK = true // can't check, skip
		return check
	}

	detectedRoot := util.FindProjectRoot(cwd)
	if detectedRoot == "" || detectedRoot == cwd {
		check.OK = true
		check.Message = "no root mismatch detected"
		return check
	}

	// Index should be at detected root
	rootIndex := store.DefaultIndexPath(detectedRoot)
	cwdIndex := store.DefaultIndexPath(cwd)

	rootExists := store.Exists(rootIndex)
	cwdExists := store.Exists(cwdIndex)

	if cwdExists && !rootExists {
		check.OK = false
		check.Code = "ROOT_MISMATCH"
		check.Message = fmt.Sprintf("index at %s but project root is %s", cwd, detectedRoot)
		check.Remediation = "cd " + detectedRoot + " && snipe index"
		check.Details = fmt.Sprintf(
			"Running snipe commands from %s will fail with MISSING_INDEX.\n"+
				"Index found at: %s\n"+
				"Expected at:    %s",
			cwd, cwdIndex, rootIndex,
		)
		return check
	}

	// Dual-index: both exist — the CWD index is a stale orphan (wastes disk, no functional impact)
	if cwdExists && rootExists {
		check.OK = true // not an error; snipe uses root index correctly
		check.Message = fmt.Sprintf("stale orphan index at %s (root index at %s is used)", cwdIndex, rootIndex)
		check.Details = "Safe to remove: rm -rf " + filepath.Join(cwd, ".snipe")
		return check
	}

	check.OK = true
	check.Message = "index root matches project root"
	return check
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.1f days", d.Hours()/24)
}
