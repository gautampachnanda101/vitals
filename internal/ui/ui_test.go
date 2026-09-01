package ui

import "testing"

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{1099511627776, "1.00 TB"},
		{-1, "-1 B"},
	}
	for _, c := range cases {
		if got := HumanBytes(c.in); got != c.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"shorter than limit", "hello", 10, "hello"},
		{"exactly at limit", "hello", 5, "hello"},
		{"longer ascii", "hello world", 5, "hell…"},
		{"multibyte stays valid", "héllo wörld", 5, "héll…"},
		{"cjk", "日本語テスト", 3, "日本…"},
		{"n is one", "abc", 1, "a"},
		{"n is zero", "abc", 0, ""},
		{"empty input", "", 5, ""},
		{"negative n", "abc", -2, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Truncate(c.s, c.n); got != c.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
			}
			for _, r := range Truncate(c.s, c.n) {
				if r == '�' {
					t.Errorf("Truncate(%q, %d) produced an invalid rune", c.s, c.n)
				}
			}
		})
	}
}

func TestDisableColor(t *testing.T) {
	// Save and restore package styling state around the test.
	saved := []*string{&Red, &Green, &Yellow, &Cyan, &Bold, &Dim, &Reset}
	orig := make([]string, len(saved))
	for i, p := range saved {
		orig[i] = *p
	}
	origEnabled := enabled
	t.Cleanup(func() {
		for i, p := range saved {
			*p = orig[i]
		}
		enabled = origEnabled
	})

	Red = "\033[1;31m"
	enabled = true
	DisableColor()

	if enabled {
		t.Error("enabled should be false after DisableColor")
	}
	for _, p := range saved {
		if *p != "" {
			t.Errorf("style code %q not cleared", *p)
		}
	}
	if got := Actionf("x"); got != "x" {
		t.Errorf("Actionf still wraps in color: %q", got)
	}
}

func TestStripANSI(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain text", "plain text"},
		{"\033[1;31mred\033[0m", "red"},
		{"\033[H\033[2Jcleared", "cleared"},
		{"a\033[32mb\033[0mc", "abc"},
		{"", ""},
	}
	for _, c := range cases {
		if got := StripANSI(c.in); got != c.want {
			t.Errorf("StripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
