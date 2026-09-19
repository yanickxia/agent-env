package version

import (
	"strconv"
	"strings"
)

// semver is a parsed subset of semantic versioning sufficient for release tags
// of the form vMAJOR.MINOR.PATCH[-PRERELEASE][+BUILD].
type semver struct {
	major, minor, patch int
	pre                 []string
	hasPre              bool
}

// IsValid reports whether v parses as a semver tag (with or without a leading
// "v").
func IsValid(v string) bool {
	_, ok := parseSemver(v)
	return ok
}

// Canonical returns v with a leading "v" when it parses as a bare numeric
// version; otherwise it returns v unchanged.
func Canonical(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "v") {
		return v
	}
	if _, ok := parseSemver(v); ok {
		return "v" + v
	}
	return v
}

// Compare compares a and b as semver. It returns -1, 0 or 1, and ok=false when
// either input is not a valid semver tag ("dev" is not).
func Compare(a, b string) (int, bool) {
	av, aok := parseSemver(a)
	bv, bok := parseSemver(b)
	if !aok || !bok {
		return 0, false
	}
	if c := compareInts(av.major, bv.major); c != 0 {
		return c, true
	}
	if c := compareInts(av.minor, bv.minor); c != 0 {
		return c, true
	}
	if c := compareInts(av.patch, bv.patch); c != 0 {
		return c, true
	}
	return comparePre(av, bv), true
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func comparePre(a, b semver) int {
	// A version without a pre-release has higher precedence.
	if !a.hasPre && !b.hasPre {
		return 0
	}
	if !a.hasPre {
		return 1
	}
	if !b.hasPre {
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		ai, aNum := numeric(a.pre[i])
		bi, bNum := numeric(b.pre[i])
		switch {
		case aNum && bNum:
			if c := compareInts(ai, bi); c != 0 {
				return c
			}
		case aNum && !bNum:
			return -1 // numeric identifiers have lower precedence
		case !aNum && bNum:
			return 1
		default:
			if c := strings.Compare(a.pre[i], b.pre[i]); c != 0 {
				return c
			}
		}
	}
	return compareInts(len(a.pre), len(b.pre))
}

func numeric(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	// Strip build metadata, then pre-release.
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	out := semver{}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		out.hasPre = true
		out.pre = strings.Split(v[i+1:], ".")
		v = v[:i]
		for _, p := range out.pre {
			if p == "" {
				return semver{}, false
			}
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	for i, p := range parts {
		if p == "" {
			return semver{}, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		switch i {
		case 0:
			out.major = n
		case 1:
			out.minor = n
		case 2:
			out.patch = n
		}
	}
	return out, true
}
