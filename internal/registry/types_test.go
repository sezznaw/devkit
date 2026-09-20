package registry

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.2.1", 1}, {"0.10.0", "0.9.9", 1}, {"1.0", "1.0.0", 0},
		{"v0.2.0", "0.2.0", 0}, {"0.2.0", "0.2.1", -1}, {"0.3.0-rc1", "0.3.0", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestChangesBetween(t *testing.T) {
	c := &Component{Changelog: []ChangeEntry{
		{Version: "0.3.0"}, {Version: "0.2.0"}, {Version: "0.10.0"}, {Version: "0.2.1"}, {Version: "0.1.0"},
	}}
	got := c.ChangesBetween("0.2.0", "0.3.0")
	if len(got) != 2 || got[0].Version != "0.2.1" || got[1].Version != "0.3.0" {
		t.Fatalf("got %+v; want 0.2.1 then 0.3.0 (exclusive of from, inclusive of to, oldest first)", got)
	}
	if n := len(c.ChangesBetween("0.3.0", "0.3.0")); n != 0 {
		t.Errorf("same version must yield nothing, got %d", n)
	}
	if n := len(c.ChangesBetween("0.1.0", "0.10.0")); n != 4 {
		t.Errorf("numeric compare: 0.10.0 is above 0.3.0, want 4 entries, got %d", n)
	}
}
