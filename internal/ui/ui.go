// Package ui provides small terminal formatting helpers shared by every subcommand.
package ui

import (
	"fmt"
	"os"
	"strings"
)

// ANSI color codes. Disabled automatically when stdout is not a terminal or
// when NO_COLOR is set (https://no-color.org).
var (
	enabled = colorEnabled()

	Red    = code("\033[1;31m")
	Green  = code("\033[1;32m")
	Yellow = code("\033[1;33m")
	Cyan   = code("\033[1;36m")
	Bold   = code("\033[1m")
	Dim    = code("\033[2m")
	Reset  = code("\033[0m")
)

func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func code(c string) string {
	if enabled {
		return c
	}
	return ""
}

// Header prints a boxed section title.
func Header(title string) {
	line := strings.Repeat("=", 80)
	fmt.Printf("\n%s%s%s\n", Bold+Cyan, line, Reset)
	fmt.Printf("%s  %s%s\n", Bold+Cyan, title, Reset)
	fmt.Printf("%s%s%s\n\n", Bold+Cyan, line, Reset)
}

// Rule prints a thin divider.
func Rule() { fmt.Println(strings.Repeat("-", 80)) }

// Infof / Warnf / Errf / Okf are colored line printers.
func Infof(format string, a ...any) { fmt.Printf(Cyan+"[*] "+Reset+format+"\n", a...) }
func Okf(format string, a ...any)   { fmt.Printf(Green+"[+] "+Reset+format+"\n", a...) }
func Warnf(format string, a ...any) { fmt.Printf(Yellow+"[!] "+Reset+format+"\n", a...) }
func Errf(format string, a ...any)  { fmt.Fprintf(os.Stderr, Red+"[x] "+Reset+format+"\n", a...) }
func Actionf(format string, a ...any) string {
	return Yellow + fmt.Sprintf(format, a...) + Reset
}

// HumanBytes renders a byte count as a human-readable string.
func HumanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
