package kit

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Finding is one line of a check: a level, the path it is about, and
// what it says.
type Finding struct {
	Level string // Error | Warning | OK
	Dir   string
	Msg   string
}

const (
	OK      = "ok"
	Warning = "warning"
	Error   = "error"
)

// Annotate says whether the check runs under Gitea Actions, where
// errors and warnings also go out as workflow commands.
func Annotate() bool { return os.Getenv("GITEA_ACTIONS") == "true" }

// Report prints one heading's findings and returns its error count.
// The shape is the core's `fort check` (reportFindings): ✓ for ok, !
// for a warning, ✗ for an error; and an unknown level is an error that
// says so, because a finding the switch loses quietly is the one
// failure a gate must not have.
func Report(w io.Writer, heading string, findings []Finding, annotate bool) int {
	fmt.Fprintf(w, "== %s\n", heading)
	errors := 0
	for _, f := range findings {
		switch f.Level {
		case OK:
			fmt.Fprintf(w, "  ✓ %-40s %s\n", f.Dir, f.Msg)
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
