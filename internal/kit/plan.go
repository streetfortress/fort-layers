package kit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"forge.streetfortress.cloud/oss/fort-layers/internal/tools"
)

// The marks every author verb reports in, the shape `fort scaffold`
// and `fort add` print and the deployments' golden-tree tests compare.
const (
	MarkNew      = '●' // a file this change creates
	MarkAppended = '○' // an existing file this change adds lines to; never a rewrite
	MarkSeeded   = '◆' // generated output a seeder will produce; never hand-edited
	MarkRemoved  = '✗' // a file or directory this change deletes
)

// Action is one planned change: a new file (Content), an append to an
// existing file (Edit), a seeded file the seeder will produce
// (neither), or a removal (MarkRemoved).
type Action struct {
	Path    string // repo-relative
	Mark    rune
	Note    string
	Content []byte
	Edit    func(string) (string, error)
}

// Plan is what an author verb applies, stages and prints: the actions, and the paths already as planned (Present), so
// a re-run is a no-op there and says so.
type Plan struct {
	Root    string
	Actions []Action
	Present []string
}

// New plans a file, unless it exists: an author verb never overwrites
// a file that is there (the manifest seeds it, the estate refines it),
// so an existing path is Present, whatever it holds.
func (p *Plan) New(path, note string, content []byte) {
	if _, err := os.Stat(filepath.Join(p.Root, path)); err == nil {
		p.present(path)
		return
	}
	p.Actions = append(p.Actions, Action{Path: path, Mark: MarkNew, Note: note, Content: content})
}

// Append plans an edit of an existing file, dry-run now: a file the
// edit leaves unchanged is Present, and an edit that fails on the file
// as it is fails here, before anything is written.
func (p *Plan) Append(path, note string, edit func(string) (string, error)) error {
	raw, err := os.ReadFile(filepath.Join(p.Root, path))
	if err != nil {
		return err
	}
	out, err := edit(string(raw))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if out == string(raw) {
		p.present(path)
		return nil
	}
	p.Actions = append(p.Actions, Action{Path: path, Mark: MarkAppended, Note: note, Edit: edit})
	return nil
}

// Remove plans a removal; a path that is not there is Present.
func (p *Plan) Remove(path, note string) {
	if _, err := os.Stat(filepath.Join(p.Root, path)); err != nil {
		p.present(path)
		return
	}
	p.Actions = append(p.Actions, Action{Path: path, Mark: MarkRemoved, Note: note})
}

// present records a path as already as planned, once: two edits of one
// file that both find nothing to do are one line in the report.
func (p *Plan) present(path string) {
	if !slices.Contains(p.Present, path) {
		p.Present = append(p.Present, path)
	}
}

// Apply writes every action under Root, in order.
func (p *Plan) Apply() error {
	for _, a := range p.Actions {
		full := filepath.Join(p.Root, a.Path)
		switch a.Mark {
		case MarkNew:
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(full, a.Content, 0o644); err != nil {
				return err
			}
		case MarkAppended:
			raw, err := os.ReadFile(full)
			if err != nil {
				return err
			}
			out, err := a.Edit(string(raw))
			if err != nil {
				return fmt.Errorf("%s: %w", a.Path, err)
			}
			if err := os.WriteFile(full, []byte(out), 0o644); err != nil {
				return err
			}
		case MarkRemoved:
			if err := os.RemoveAll(full); err != nil {
				return err
			}
		}
	}
	return nil
}

// Stage runs git add -A on every path of the plan, so the operator's
// next step is the commit.
func (p *Plan) Stage() error {
	if len(p.Actions) == 0 {
		return nil
	}
	git, err := tools.Runner("git")
	if err != nil {
		return err
	}
	for _, a := range p.Actions {
		cmd := git("-C", p.Root, "add", "-A", "--", a.Path)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git add %s: %s", a.Path, LastLine(string(out)))
		}
	}
	return nil
}

// Report prints the plan in the marked-tree shape: the title, then one
// line per action — mark and path, no note — and the present paths; with
// no action, it says so. The notes stay in the plan for the reader of
// the code: the report is the tree.
func (p *Plan) Report(w io.Writer, title string) {
	fmt.Fprintln(w, title)
	for _, a := range p.Actions {
		fmt.Fprintf(w, "%c %s\n", a.Mark, a.Path)
	}
	for _, f := range p.Present {
		fmt.Fprintf(w, "· %s  (already as planned)\n", f)
	}
	if len(p.Actions) == 0 {
		fmt.Fprintln(w, "\nNothing to write.")
	}
}
