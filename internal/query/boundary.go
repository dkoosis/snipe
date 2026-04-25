package query

import "strings"

// MatchPackagePatterns returns the subset of `universe` matching any pattern.
// A pattern is either an exact suffix (e.g. "internal/store") or a recursive
// suffix ending in "/..." (e.g. "internal/store/..."), which matches the
// package itself and any descendant.
//
// Matching is suffix-based against the full pkg_path so callers can use short
// forms — `internal/store` matches `github.com/x/proj/internal/store`.
func MatchPackagePatterns(universe, patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(universe))
	var out []string
	for _, pkg := range universe {
		for _, pat := range patterns {
			if matches(pkg, pat) {
				if _, dup := seen[pkg]; !dup {
					seen[pkg] = struct{}{}
					out = append(out, pkg)
				}
				break
			}
		}
	}
	return out
}

func matches(pkg, pattern string) bool {
	if strings.HasSuffix(pattern, "/...") {
		base := strings.TrimSuffix(pattern, "/...")
		return strings.HasSuffix(pkg, "/"+base) || pkg == base ||
			strings.Contains(pkg, "/"+base+"/")
	}
	return pkg == pattern || strings.HasSuffix(pkg, "/"+pattern)
}
