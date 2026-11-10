{
  description = "fort-layers — layers on top of forts: one binary per role, fort-<role>, and the NixOS modules each role ships";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      lib = nixpkgs.lib;
      systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" "x86_64-darwin" ];
      forAll = f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # The roles are the directories under cmd/: cmd/fort-<role> is the
      # binary the estate's flake re-exports as fort-<role> (forts'
      # docs/layers.md). A new role is a directory, not a flake edit;
      # no cmd/ directory is no roles, not an error.
      roles = lib.filter (n: lib.hasPrefix "fort-" n)
        (lib.attrNames (lib.filterAttrs (_: t: t == "directory")
          (if builtins.pathExists ./cmd then builtins.readDir ./cmd else { })));

      # The layers' tool dependencies as one declared set, the shape of
      # sfi/forts#25: internal/tools/tools.json names every binary a
      # role execs and the nixpkgs attribute that carries it; this flake
      # is its second reader, so the wrapper's PATH and the Go code's
      # check can never name different tools.
      toolAttrs = lib.attrValues (lib.importJSON ./internal/tools/tools.json).packaged;
      toolset = pkgs:
        lib.filter (p: lib.meta.availableOn pkgs.stdenv.hostPlatform p)
          (map (attr: lib.getAttrFromPath (lib.splitString "." attr) pkgs) toolAttrs);

      # Every module under modules/<role>/ is an output, by role and file
      # name: modules/backups/pv-backup.nix → nixosModules.backups-pv-backup.
      # A host file imports inputs.fort-layers.nixosModules.<that>. No
      # modules/ directory is no modules, not an error.
      modulesIn = dir: prefix:
        lib.concatMapAttrs (name: type:
          if type == "regular" && lib.hasSuffix ".nix" name then
            { "${prefix}${lib.removeSuffix ".nix" name}" = dir + "/${name}"; }
          else if type == "directory" then
            modulesIn (dir + "/${name}") "${prefix}${name}-"
          else
            { }) (builtins.readDir dir);
    in {
      nixosModules = if builtins.pathExists ./modules then modulesIn ./modules "" else { };

      packages = forAll (pkgs:
        let
          # One Go build for every role: the binaries share the kit and
          # the module, so one vendorHash covers them all.
          unwrapped = pkgs.buildGoModule {
            pname = "fort-layers-unwrapped";
            version = self.shortRev or self.dirtyShortRev or "dirty";
            src = ./.;
            subPackages = map (r: "cmd/${r}") roles;
            vendorHash = "sha256-2SXAu1fxiRbuMOKOoB8OVzTmtR3Os423j80En+SHnzU=";
            doCheck = false;
            meta.description = "the role binaries of fort-layers, without their tools on PATH";
          };
          # What `nix run <estate>/nixos#fort-<role>` runs: the binary
          # with the packaged tool set ahead of it on PATH, from this
          # flake's nixpkgs — a prefix, not a suffix, so the version this
          # flake pins wins over whatever the machine carries.
          wrap = role: pkgs.runCommand "${role}-${unwrapped.version}"
            {
              nativeBuildInputs = [ pkgs.makeWrapper ];
              passthru = { inherit unwrapped; };
              meta = {
                description = "${role}: a layer of fort-layers (init, scaffold, mint, check, dismantle)";
                mainProgram = role;
              };
            }
            ''
              mkdir -p $out/bin
              makeWrapper ${unwrapped}/bin/${role} $out/bin/${role} \
                --prefix PATH : ${lib.makeBinPath (toolset pkgs)}
            '';
        in
        lib.genAttrs roles wrap // { inherit unwrapped; });

      checks = forAll (pkgs:
        lib.genAttrs roles (r: self.packages.${pkgs.system}.${r}));

      # `nix develop` in this repo: the Go toolchain and the same
      # declared tools, so `go run ./cmd/fort-<role> <verb>` finds every
      # one.
      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go ] ++ toolset pkgs;
        };
      });
    };
}
