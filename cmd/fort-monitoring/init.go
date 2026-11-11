package main

import (
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/template"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

//go:embed templates
var templates embed.FS

// render executes one embedded template.
func render(name string, data any) ([]byte, error) {
	t, err := template.ParseFS(templates, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out.Bytes(), nil
}

// `fort monitoring init` renders layers/monitoring/estate.toml: the
// [push] table, both streams on unless a flag turns one off. The hub is
// the core's to name; init says so when estate.toml names none yet.
func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	noMetrics := fs.Bool("no-metrics", false, "the edge Prometheus of a cluster fort does not remote-write to the hub")
	noLogs := fs.Bool("no-logs", false, "no fluent-bit: a cluster fort ships no logs to the hub's Loki")
	write := fs.Bool("write", false, "lay the file down and stage it (default: print the plan)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: fort monitoring init [--no-metrics] [--no-logs] [--write]")
	}
	root, err := kit.Root()
	if err != nil {
		return err
	}
	view := struct{ Metrics, Logs bool }{!*noMetrics, !*noLogs}
	content, err := render("estate.toml.gotpl", view)
	if err != nil {
		return err
	}
	est := Estate{Push: &Push{Metrics: &view.Metrics, Logs: &view.Logs}}
	p := &kit.Plan{Root: root}
	p.New(role.EstatePath(), est.Summary(), content)
	if *write {
		if err := p.Apply(); err != nil {
			return err
		}
		if err := p.Stage(); err != nil {
			return err
		}
	}
	p.Report(os.Stdout, "fort monitoring init", *write)
	if core, err := loadCore(root); err == nil {
		if _, ok := core.hub(); !ok {
			fmt.Fprintln(os.Stdout, "\nestate.toml names no [hubs.monitoring]: nothing pushes and no fort has a file for a hub until it does (forts' docs/hub.md). The edge stack renders either way: each cluster watches itself.")
		}
	}
	if *write && len(p.Actions) > 0 {
		fmt.Fprintln(os.Stdout, "\nThen: fort monitoring scaffold <fort> for each fort (docs/monitoring.md).")
	}
	return nil
}
