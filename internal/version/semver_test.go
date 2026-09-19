package version

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.3.0", "v0.3.0", 0},
		{"v0.4.0", "v0.3.0", 1},
		{"v0.3.0", "v0.4.0", -1},
		{"v1.0.0", "v0.9.9", 1},
		{"v0.3.10", "v0.3.9", 1},
		{"v0.3.0-rc1", "v0.3.0", -1},
		{"v0.3.0", "v0.3.0-rc1", 1},
		{"v0.3.0-rc2", "v0.3.0-rc1", 1},
		{"v0.3.0-beta", "v0.3.0-rc1", -1}, // alphanumeric lexical: beta < rc1
		{"v0.3.0-rc1", "v0.3.0-beta", 1},
		{"v0.3.0+build", "v0.3.0", 0},
		{"v0.3.0", "0.3.0", 0}, // bare numeric accepted
	}
	for _, tc := range cases {
		got, ok := Compare(tc.a, tc.b)
		if !ok || got != tc.want {
			t.Fatalf("Compare(%q,%q) = %d,%v want %d,true", tc.a, tc.b, got, ok, tc.want)
		}
	}

	for _, bad := range []string{"dev", "", "0.3", "v1.2.x", "v1.2.3.4", "version"} {
		if _, ok := Compare(bad, "v1.0.0"); ok {
			t.Fatalf("Compare must reject %q", bad)
		}
	}
}

func TestIsValidAndCanonical(t *testing.T) {
	valid := map[string]string{
		"v0.3.0":     "v0.3.0",
		"0.3.0":      "v0.3.0",
		"v1.2.3-rc1": "v1.2.3-rc1",
	}
	for in, canon := range valid {
		if !IsValid(in) {
			t.Fatalf("%q should be valid", in)
		}
		if got := Canonical(in); got != canon {
			t.Fatalf("Canonical(%q) = %q want %q", in, got, canon)
		}
	}
	for _, bad := range []string{"dev", "", "v0.3", "v1.2.x"} {
		if IsValid(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
	if got := Canonical("dev"); got != "dev" {
		t.Fatalf("Canonical(dev) = %q", got)
	}
}
