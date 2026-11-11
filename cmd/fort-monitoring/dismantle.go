package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

// `fort monitoring dismantle <fort> [--write]` runs BEFORE the core's
// dismantle (forts' docs/layers.md), while the fort's manifest still
// exists: it removes the fort's file for the hub and its line in the
// directory's kustomization, so the hub drops the fort's rules by prune
// and a retired fort cannot keep paging for a host that is gone; and the
// fort's [monitoring] file. What the core's dismantle removes after it
// (flux/clusters/<fort>/ with HUB_MESH_IP; a hub's apps tree with the
// stack and the subscription) is named, not touched. The estate's shared
// renders — the edge stack, the push, fluent-bit — are the estate's,
// not one fort's, and stay.
func dismantleCmd(args []string) error {
	fs := flag.NewFlagSet("dismantle", flag.ContinueOnError)
	write := fs.Bool("write", false, "remove the files and stage the removal (default: print the plan)")
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos = append(pos, args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(pos) != 1 || fs.NArg() > 0 {
		return errors.New("usage: fort monitoring dismantle <fort> [--write]")
	}
	fort := pos[0]
	root, err := kit.Root()
	if err != nil {
		return err
	}
	p, notes, err := planDismantle(root, fort)
	if err != nil {
		return err
	}
	if *write {
		if err := p.Apply(); err != nil {
			return err
		}
		if err := p.Stage(); err != nil {
			return err
		}
	}
	p.Report(os.Stdout, "fort monitoring dismantle "+fort, *write)
	for _, n := range notes {
		fmt.Fprintln(os.Stdout, "\n"+n)
	}
	return nil
}

func planDismantle(root, fort string) (*kit.Plan, []string, error) {
	if !kit.ValidFortName(fort) {
		return nil, nil, fmt.Errorf("%q is not a fort name", fort)
	}
	if !role.Installed(root) {
		return nil, nil, fmt.Errorf("no %s — the layer is not installed here", role.EstatePath())
	}
	core, err := loadCore(root)
	if err != nil {
		return nil, nil, err
	}
	p := &kit.Plan{Root: root}
	var notes []string
	if h, ok := core.hub(); ok {
		p.Remove(hubFilePath(h.Fort, fort), "the hub drops the fort's rules by prune")
		item := fort + ".yaml"
		if kit.Exists(root, hubKustomization(h.Fort)) {
			// The last fort's line leaves the list empty: `resources: []`,
			// because kustomize refuses a null list and the hub's
			// subscription still builds this directory (fort-layers#26).
			if err := p.Append(hubKustomization(h.Fort), "- "+item, func(s string) (string, error) {
				return kit.CollapseEmptyList(removeListItem(s, item), "resources"), nil
			}); err != nil {
				return nil, nil, err
			}
			if raw, err := os.ReadFile(filepath.Join(root, hubKustomization(h.Fort))); err == nil {
				trimmed := removeListItem(string(raw), item)
				if kit.CollapseEmptyList(trimmed, "resources") != trimmed {
					notes = append(notes, fmt.Sprintf("%s lists no fort after this (resources: []): the hub applies nothing for this estate until a fort is scaffolded.", hubKustomization(h.Fort)))
				}
			}
		}
		if core.hubLocal(h) && fort == h.Fort {
			notes = append(notes, fmt.Sprintf("%s is this estate's monitoring hub ([hubs.monitoring]): every other fort's file in %s/ points at it and every cluster pushes to it. Re-home the role before the core's dismantle, or those forts go unwatched; the stack and the subscription in %s/ go with the core's dismantle of the apps tree.", fort, hubDir(h.Fort), appsDir(fort)))
		}
		if kit.Exists(root, clusterVarsPath(fort)) {
			notes = append(notes, fmt.Sprintf("%s (%s) goes with the core's dismantle of the cluster's files.", clusterVarsPath(fort), hubVar))
		}
	} else {
		notes = append(notes, "estate.toml names no [hubs.monitoring]: no file for a hub to remove.")
	}
	p.Remove(role.FortPath(fort), "the fort's [monitoring] declaration")
	return p, notes, nil
}

// removeListItem drops every `- <item>` line of a kustomization's text.
func removeListItem(text, item string) string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "- "+item {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
