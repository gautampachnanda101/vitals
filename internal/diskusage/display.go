package diskusage

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DisplayPath escapes terminal controls and Unicode format controls. It is
// display-only: keep raw Result paths for filesystem operations and URLs.
func DisplayPath(path string) string {
	var out strings.Builder
	for len(path) > 0 {
		r, n := utf8.DecodeRuneInString(path)
		if r == utf8.RuneError && n == 1 {
			fmt.Fprintf(&out, "\\x%02x", path[0])
		} else if r < 0x20 || r >= 0x7f && r <= 0x9f || unicode.Is(unicode.Cf, r) {
			if r <= 0xff {
				fmt.Fprintf(&out, "\\x%02x", r)
			} else if r <= 0xffff {
				fmt.Fprintf(&out, "\\u%04x", r)
			} else {
				fmt.Fprintf(&out, "\\U%08x", r)
			}
		} else {
			out.WriteRune(r)
		}
		path = path[n:]
	}
	return out.String()
}
