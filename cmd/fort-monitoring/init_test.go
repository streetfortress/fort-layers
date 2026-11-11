package main

import (
	"os/exec"
	"strings"
	"testing"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

func TestInitRendersWhatTheLoaderReads(t *testing.T) {
	root := t.TempDir()
	write(t, root, "estate.toml", "name = \"x\"\n")
	write(t, root, "forts/hub.toml", "mesh_ip = \"10.9.0.2\"\n")
	t.Setenv(kit.RootEnv, root)
	for _, c := range []struct {
		metrics, logs bool
		summary       string
	}{{true, true, "push: metrics, logs"}, {false, true, "push: logs"}, {false, false, "push: nothing"}} {
		content, err := render("estate.toml.gotpl", struct{ Metrics, Logs bool }{c.metrics, c.logs})
		if err != nil {
			t.Fatal(err)
		}
		write(t, root, role.EstatePath(), string(content))
		est, err := loadEstate(root)
		if err != nil || est.PushMetrics() != c.metrics || est.PushLogs() != c.logs || !strings.HasPrefix(est.Summary(), c.summary) {
			t.Errorf("%+v: %v %+v %q\n%s", c, err, est, est.Summary(), content)
		}
	}
	// A file with no [push] at all: both on.
	write(t, root, role.EstatePath(), "# nothing\n")
	if est, err := loadEstate(root); err != nil || !est.PushMetrics() || !est.PushLogs() {
		t.Errorf("empty file: %v %+v", err, est)
	}
	// A key the struct lacks is a refusal.
	write(t, root, role.EstatePath(), "[push]\nmetrics = true\ntraces = true\n")
	if _, err := loadEstate(root); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("unknown key: %v", err)
	}
}

func TestInitFlags(t *testing.T) {
	root := t.TempDir()
	write(t, root, "estate.toml", "name = \"x\"\n")
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	t.Setenv(kit.RootEnv, root)
	if err := initCmd([]string{"extra"}); err == nil {
		t.Error("want an error")
	}
	if err := initCmd([]string{"--no-logs", "--write"}); err != nil {
		t.Fatal(err)
	}
	est, err := loadEstate(root)
	if err != nil || !est.PushMetrics() || est.PushLogs() {
		t.Errorf("written: %v %+v", err, est)
	}
	// A second init never overwrites.
	if err := initCmd([]string{"--write"}); err != nil {
		t.Fatal(err)
	}
	if est, _ := loadEstate(root); est.PushLogs() {
		t.Error("init overwrote the estate's file")
	}
}
