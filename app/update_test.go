package main

import "testing"

// The update check offers a release only when its tag is newer than this build's version.
func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.1.0", "1.0.0", 1}, {"1.0.0", "1.0.0", 0}, {"1.0.0", "1.1.0", -1},
		{"1.10.0", "1.9.0", 1}, {"2.0.0", "1.99.99", 1}, {"1.0.1", "1.0.0", 1},
	} {
		if got := compareVersions(c.a, c.b); (got > 0) != (c.want > 0) || (got < 0) != (c.want < 0) {
			t.Errorf("compareVersions(%q, %q) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}
}
