package agentplugin

import "strings"

// ValidName reports whether name satisfies §5.5: 1-64 characters of
// lowercase ASCII letters, digits, hyphens and periods, alphanumeric at both
// ends, with no "--" or "..".
func ValidName(name string) bool {
	if name == "" || len(name) > 64 || strings.Contains(name, "--") || strings.Contains(name, "..") {
		return false
	}
	for i, r := range name {
		lower := r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'
		if lower || digit {
			continue
		}
		if (r == '-' || r == '.') && i != 0 && i != len(name)-1 {
			continue
		}
		return false
	}
	return true
}

// NormalizeName derives a §5.5 plugin name from a ynh harness name, which
// may carry uppercase letters and underscores. Letters are lowercased,
// underscores become hyphens, runs of separators collapse to one, and
// separators are trimmed from the ends. The result is "" when nothing
// usable remains, and ok reports whether the input was already valid.
func NormalizeName(name string) (normalized string, ok bool) {
	if ValidName(name) {
		return name, true
	}
	var b strings.Builder
	lastSep := true // suppress a leading separator
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastSep = false
		case r == '-' || r == '_' || r == '.':
			if !lastSep {
				if r == '.' {
					b.WriteRune('.')
				} else {
					b.WriteRune('-')
				}
				lastSep = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-.")
	if len(out) > 64 {
		out = strings.TrimRight(out[:64], "-.")
	}
	return out, false
}
