package kit

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Finding is one line of a check: a level, the path it is about, and
// what it says. An Info finding is a fact, not a check: Dir is its
// label and Msg its value.
type Finding struct {
	Level string // Error | Warning | OK | Info
	Dir   string
	Msg   string
}

const (
	Info    = "info"
	OK      = "ok"
	Warning = "warning"
	Error   = "error"
)

// Annotate says whether the check runs under Gitea Actions, where
// errors and warnings also go out as workflow commands.
func Annotate() bool { return os.Getenv("GITEA_ACTIONS") == "true" }

// Report prints one heading's findings and returns its error count.
// The shape is the core's `fort check` (reportFindings): facts first,
// as `label  value` with no mark; then ✓ for ok, ! for a warning, ✗ for
// an error. Success is terse, failure is verbose. A heading with no
// findings prints nothing. An unknown level is an error that says so,
// because a finding the switch loses quietly is the one failure a gate
// must not have.
func Report(w io.Writer, heading string, findings []Finding, annotate bool) int {
	if len(findings) == 0 {
		return 0
	}
	fmt.Fprintf(w, "== %s\n", heading)
	width := 0
	for _, f := range findings {
		if f.Level == Info && len(f.Dir) > width {
			width = len(f.Dir)
		}
	}
	for _, f := range findings {
		if f.Level == Info {
			fmt.Fprintf(w, "  %-*s  %s\n", width, f.Dir, f.Msg)
		}
	}
	errors := 0
	for _, f := range findings {
		switch f.Level {
		case Info:
		case OK:
			fmt.Fprintf(w, "  ✓ %s\n", f.Msg)
		case Warning:
			fmt.Fprintf(w, "  ! %s: %s\n", f.Dir, f.Msg)
			if annotate {
				fmt.Fprintf(w, "::warning::%s: %s\n", f.Dir, f.Msg)
			}
		case Error:
			errors++
			fmt.Fprintf(w, "  ✗ %s: %s\n", f.Dir, f.Msg)
			if annotate {
				fmt.Fprintf(w, "::error::%s: %s\n", f.Dir, f.Msg)
			}
		default:
			errors++
			fmt.Fprintf(w, "  ✗ %s: %s [unknown finding level %q — this is a bug in the check]\n", f.Dir, f.Msg, f.Level)
		}
	}
	return errors
}

// LastLine is the last non-empty line of a tool's output: the one a
// sops or git error is on.
func LastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
