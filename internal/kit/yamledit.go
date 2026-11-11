package kit

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Text-level YAML edits: the "append a line to an existing file" cases
// an author verb is allowed (new files and appended list entries,
// never a rewrite). Working on text rather than a YAML tree
// keeps every comment and the author's formatting byte-for-byte; the
// files are ours, so the shapes assumed here are the shapes we render.

// InsertIntoBlock inserts <entry> — one line, or several already
// indented — as the last item of the top-level block under <key> (e.g.
// resources:, patches:). The block runs from the key line through every
// following indented or comment line, so the entry lands inside the
// block even when another top-level block follows it.
func InsertIntoBlock(text, key, entry string) (string, error) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		if l == key+":" {
			start = i
			break
		}
		// `<key>: []` is what a dismantle leaves when the block's last
		// item goes (fort-layers#26): reopen the list for the new entry.
		if strings.TrimRight(l, " ") == key+": []" {
			lines[i] = key + ":"
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no top-level %s: block", key)
	}
	// end: the last line the block owns. A blank line or a top-level
	// comment between two blocks belongs to neither, so the scan passes
	// over it and the entry lands against the block's own last item.
	end := start
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if l == "" || strings.HasPrefix(l, " ") || strings.HasPrefix(l, "#") {
			if strings.TrimSpace(l) != "" && strings.HasPrefix(l, " ") {
				end = i
			}
			continue
		}
		break
	}
	out := append([]string{}, lines[:end+1]...)
	out = append(out, strings.Split(strings.TrimRight(entry, "\n"), "\n")...)
	out = append(out, lines[end+1:]...)
	return strings.Join(out, "\n"), nil
}

// AppendListItem inserts `  - <item>` as the last entry of the top-level
// block sequence under <key> (e.g. resources:).
func AppendListItem(text, key, item string) (string, error) {
	return InsertIntoBlock(text, key, "  - "+item)
}

// CollapseEmptyList rewrites `<key>:` to `<key>: []` when no item is
// left under it: a dismantle that removes the block's last item would
// otherwise leave a null list, which kustomize refuses — it accepts an
// empty one (fort-layers#26). Text whose block still holds an item, or
// whose key is already `<key>: []` or absent, comes back unchanged.
func CollapseEmptyList(text, key string) string {
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		if l == key+":" {
			start = i
			break
		}
	}
	if start < 0 {
		return text
	}
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if !strings.HasPrefix(lines[i], " ") {
			break // the next top-level block: this one is empty
		}
		return text // the block still holds an item
	}
	lines[start] = key + ": []"
	return strings.Join(lines, "\n")
}

// AppendBlock adds a top-level block at the end of the document (a new
// `patches:` list, a new entry under a trailing `secrets:` map). The
// block text is inserted verbatim before the final newline.
func AppendBlock(text, block string) string {
	text = strings.TrimRight(text, "\n") + "\n"
	return text + strings.TrimRight(block, "\n") + "\n"
}

// AppendPatch registers a kustomize JSON6902 patch file against a target,
// creating the `patches:` block (with the given comment) on first use.
func AppendPatch(text, comment, path, group, version, kind, name, namespace string) string {
	entry := fmt.Sprintf(`  - path: %s
    target:
      group: %s
      version: %s
      kind: %s
      name: %s
      namespace: %s
`, path, group, version, kind, name, namespace)
	// The entry goes inside the existing patches: list. AppendBlock would
	// put it at the end of the file, under whatever block follows —
	// `kustomize build` then reads `path` as a field of a trailing
	// configMapGenerator and fails the whole apps layer with
	// `unknown field "path"` (deployments#696, the staging sites-db).
	if out, err := InsertIntoBlock(text, "patches", entry); err == nil {
		return out
	}
	// No patches: block — create one at the end of the file.
	return AppendBlock(text, comment+"patches:\n"+entry)
}

// AppendMapEntry inserts `<indent><key>: <value>` as the last entry of the
// mapping whose key line is exactly <anchor> (e.g. "    keys:"), i.e.
// after the last line indented deeper than the anchor.
func AppendMapEntry(text, anchor, entry string) (string, error) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		if l == anchor {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no %q line", anchor)
	}
	depth := len(anchor) - len(strings.TrimLeft(anchor, " "))
	end := start
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " ")) <= depth {
			break
		}
		end = i
	}
	out := append([]string{}, lines[:end+1]...)
	out = append(out, entry)
	out = append(out, lines[end+1:]...)
	return strings.Join(out, "\n"), nil
}

// AppendMapEntryUnder is AppendMapEntry scoped to one section: the anchor
// is searched only after the line that is exactly <section>, for files
// that carry the same anchor twice (the rustfs contract's two `keys:`).
func AppendMapEntryUnder(text, section, anchor, entry string) (string, error) {
	lines := strings.Split(text, "\n")
	at := -1
	for i, l := range lines {
		if l == section {
			at = i
			break
		}
	}
	if at < 0 {
		return "", fmt.Errorf("no %q line", section)
	}
	head := strings.Join(lines[:at+1], "\n")
	tail, err := AppendMapEntry(strings.Join(lines[at+1:], "\n"), anchor, entry)
	if err != nil {
		return "", err
	}
	return head + "\n" + tail, nil
}

// AppendAfterLast inserts <line> after the last line that starts with
// <prefix> (whitespace included) — the bucket Job's `rc bucket create`
// run, where order is meaningless and the shape is one line per app.
func AppendAfterLast(text, prefix, line string) (string, error) {
	lines := strings.Split(text, "\n")
	at := -1
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			at = i
		}
	}
	if at < 0 {
		return "", fmt.Errorf("no line starting with %q", prefix)
	}
	out := append([]string{}, lines[:at+1]...)
	out = append(out, line)
	out = append(out, lines[at+1:]...)
	return strings.Join(out, "\n"), nil
}

// EnsureClusterVar adds `  <key>: <value>` to a cluster-vars ConfigMap's
// data: map when no line sets <key>, and returns the text unchanged when
// one does — whatever its value: the file is the estate's, and `fort
// check` is what judges the value (fleet.go). The one case scaffold
// edits an existing cluster-vars.yaml (forts#50).
func EnsureClusterVar(text, key, value string) (string, error) {
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), key+":") && !strings.HasPrefix(strings.TrimSpace(l), "#") {
			return text, nil
		}
	}
	return AppendMapEntry(text, "data:", "  "+key+": "+value)
}

// ListsResource says whether a kustomization's text already lists an
// item — by line, so a comment that mentions the name does not count.
func ListsResource(text, item string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "- "+item {
			return true
		}
	}
	return false
}

// KustomizationLists says whether <dir>/kustomization.yaml lists item.
func KustomizationLists(root, dir, item string) bool {
	raw, err := os.ReadFile(filepath.Join(root, dir, "kustomization.yaml"))
	if err != nil {
		return false
	}
	return ListsResource(string(raw), item)
}

// KustomizationReaches says whether <dir>/kustomization.yaml reaches
// item: lists it, or lists an entry whose own kustomization includes it
// — the overlay shape: a second layer's directory includes the base
// through kustomize and patches it, and the swap point is this entry.
// One hop, by design: what else the overlay holds is the second layer's
// own check's work.
func KustomizationReaches(root, dir, item string) bool {
	raw, err := os.ReadFile(filepath.Join(root, dir, "kustomization.yaml"))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		entry := path.Clean(strings.TrimPrefix(t, "- "))
		if entry == item {
			return true
		}
		sub, err := os.ReadFile(filepath.Join(root, dir, filepath.FromSlash(entry), "kustomization.yaml"))
		if err != nil {
			continue
		}
		for _, sl := range strings.Split(string(sub), "\n") {
			st := strings.TrimSpace(sl)
			if !strings.HasPrefix(st, "- ") {
				continue
			}
			if path.Clean(path.Join(entry, strings.TrimPrefix(st, "- "))) == item {
				return true
			}
		}
	}
	return false
}
