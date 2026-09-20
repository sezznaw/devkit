package workspace

import "testing"

func TestParseRepo(t *testing.T) {
	cases := []struct{ ref, host, path string }{
		{"sezznaw/idl", "github.com", "sezznaw/idl"},
		{"https://gitlab.corp.com/indie-game/sub/idl.git", "gitlab.corp.com", "indie-game/sub/idl"},
		{"git@gitlab.corp.com:indie-game/idl.git", "gitlab.corp.com", "indie-game/idl"},
		{"ssh://git@gitlab.corp.com:2222/indie-game/idl.git", "gitlab.corp.com", "indie-game/idl"},
		{"/local/idl.git", "", ""},
		{"./idl", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		h, p := ParseRepo(c.ref, "")
		if h != c.host || p != c.path {
			t.Errorf("ParseRepo(%q) = %q, %q; want %q, %q", c.ref, h, p, c.host, c.path)
		}
	}
	if h, _ := ParseRepo("a/b", "https://ghe.corp.com/"); h != "ghe.corp.com" {
		t.Errorf("default host not honoured: %q", h)
	}
}

func TestDetectCI(t *testing.T) {
	cases := []struct{ module, idl, want string }{
		{"github.com/sezznaw/order", "", CIGitHub},
		{"gitlab.corp.com/indie-game/user", "", CIGitLab},
		{"indie-game/user", "", CIBoth},                                         // nothing known yet
		{"indie-game/user", "git@gitlab.corp.com:indie-game/idl.git", CIGitLab}, // IDL host decides
		{"indie-game/user", "sezznaw/idl", CIGitHub},
		{"git.corp.com/x/user", "", CIBoth},                 // unknown platform
		{"gitlab.corp.com/x/user", "sezznaw/idl", CIGitLab}, // module wins over IDL
	}
	for _, c := range cases {
		if got := DetectCI(c.module, c.idl, ""); got != c.want {
			t.Errorf("DetectCI(%q, %q) = %q, want %q", c.module, c.idl, got, c.want)
		}
	}
}
