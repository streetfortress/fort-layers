package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

// `fort monitoring mint <hub>` writes the stack's one
// credential, after scaffold rendered the stack
// (forts.inventory.yaml in the stack directory is the record):
//
//	grafana-admin.sops.yaml   admin / a generated password
//
// A file that exists is kept, never rewritten: rotation is a deliberate
// act (sops set on the file, or rm and mint again). The Alertmanager's
// config is NOT minted: it is committed in the clear
// (alertmanager-config.yaml), and the opt-in webhook secret is the
// estate's own edit, documented there.

type mintPlan struct {
	root, fort string
	present    bool
	password   string
}

func mintCmd(args []string) error {
	fs := flag.NewFlagSet("mint", flag.ContinueOnError)
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos = append(pos, args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(pos) != 1 || fs.NArg() > 0 {
		return errors.New("usage: fort monitoring mint <hub>")
	}
	root, err := kit.Root()
	if err != nil {
		return err
	}
	tool := kit.Sops{Root: root}
	p, err := planMint(root, pos[0])
	if err != nil {
		return err
	}
	if err := p.apply(tool); err != nil {
		return err
	}
	p.report(os.Stdout)
	return nil
}

func planMint(root, fort string) (*mintPlan, error) {
	if !kit.ValidFortName(fort) {
		return nil, fmt.Errorf("%q is not a fort name", fort)
	}
	f, err := loadFort(root, fort)
	if err != nil {
		return nil, err
	}
	if f.Monitoring.Hub == nil {
		return nil, fmt.Errorf("%s has no [monitoring.hub] — mint writes the hub's stack credential, and %s is a watched fort with nothing to mint", role.FortPath(fort), fort)
	}
	if !kit.Exists(root, filepath.Join(stackDir(fort), "kustomization.yaml")) {
		return nil, fmt.Errorf("no %s/ — fort monitoring scaffold %s renders the stack this writes into", stackDir(fort), fort)
	}
	p := &mintPlan{root: root, fort: fort, password: hex24()}
	p.present = kit.Exists(root, filepath.Join(stackDir(fort), stackSecrets[0]))
	return p, nil
}

func (p *mintPlan) apply(tool kit.Secrets) error {
	if p.present {
		return nil
	}
	rel := filepath.Join(stackDir(p.fort), stackSecrets[0])
	if err := os.WriteFile(filepath.Join(p.root, rel), []byte(grafanaAdmin(p.password)), 0o600); err != nil {
		return err
	}
	if err := tool.EncryptInPlace(rel); err != nil {
		return err
	}
	return tool.GitAdd(rel)
}

func (p *mintPlan) report(w io.Writer) {
	fmt.Fprintf(w, "fort monitoring mint %s   (the hub's credential)\n", p.fort)
	rel := filepath.Join(stackDir(p.fort), stackSecrets[0])
	if p.present {
		fmt.Fprintf(w, "· %-52s exists — kept\n", rel)
	} else {
		fmt.Fprintf(w, "● %-52s admin / a generated password\n", rel)
	}
	fmt.Fprintln(w, "\nThen: the PR. The gate's kustomize build goes green with the file present.")
}

// hex24 is the shape the contracts call hex24: 24 random bytes as hex.
func hex24() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func grafanaAdmin(password string) string {
	return `# Grafana's admin login (forts.inventory.yaml; fort monitoring mint
# wrote it). The chart reads admin-user and admin-password.
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin
  namespace: monitoring-central
type: Opaque
stringData:
  admin-user: admin
  admin-password: ` + password + `
`
}
