// Package tools is the layers' tool dependencies as one declared set.
// Copied from sfi/forts internal/tools at 56ee5cd (the commit before
// forts#143 removed the layers from the core); the design is forts#25.
//
// tools.json is the set: every binary a role binary — and every child
// it execs, which inherits its PATH — runs by name, with the nixpkgs
// attribute that carries it. Two readers share that file, which is the
// point of it being a file:
//
//   - flake.nix wraps every packages.fort-<role> with the whole
//     packaged set on PATH, from the flake's own pinned nixpkgs, and
//     puts the same list in devShells.default.
//   - this package checks for one tool at run time, before the verb
//     does any work, and names the missing one with every way to get
//     it. It never fetches a tool itself.
//
// Declare a tool here before you exec it: Command refuses a name the
// set does not carry, and TestDeclaredSetCoversEveryExec reads the
// source for the calls that went around Command.
package tools

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
)

//go:embed tools.json
var declaredJSON []byte

// set is tools.json. Packaged maps a binary to its nixpkgs attribute
// path; Ambient maps a binary the host must own to the attribute it
// comes from and the reason the closure must not carry it.
type set struct {
	Packaged map[string]string  `json:"packaged"`
	Ambient  map[string]ambient `json:"ambient"`
}

type ambient struct {
	Attr string `json:"attr"`
	Why  string `json:"why"`
}

var declared = load()

func load() set {
	var s set
	if err := json.Unmarshal(declaredJSON, &s); err != nil {
		panic("internal/tools/tools.json: " + err.Error())
	}
	return s
}

// Ensure checks that a declared tool is on PATH — the wrapper's copy,
// the dev shell's, the operator's own — and otherwise says how to get
// it.
func Ensure(name string) error {
	if a, ok := declared.Ambient[name]; ok {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%s is not on PATH — the layer takes it from the host (%s). Install it on the host (nixpkgs attribute: %s)", name, a.Why, a.Attr)
		}
		return nil
	}
	attr, ok := declared.Packaged[name]
	if !ok {
		return fmt.Errorf("%s is not in the layers' tool set — declare it in internal/tools/tools.json", name)
	}
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s is not on PATH. Install it with your package manager, or take the whole tool set: nix run <estate>/nixos#fort-<role> (or nix develop in a checkout of fort-layers). nixpkgs attribute: %s", name, attr)
	}
	return nil
}

// Command is the one way a role starts a declared tool: Ensure, then
// the command itself, unstarted — the caller owns the stdio, the
// working directory and the error text.
func Command(name string, args ...string) (*exec.Cmd, error) {
	run, err := Runner(name)
	if err != nil {
		return nil, err
	}
	return run(args...), nil
}

// Runner is Command for a caller that runs the same tool more than
// once: the tool is checked here, and every command after that is one
// call.
func Runner(name string) (func(args ...string) *exec.Cmd, error) {
	if err := Ensure(name); err != nil {
		return nil, err
	}
	return func(args ...string) *exec.Cmd { return exec.Command(name, args...) }, nil
}
