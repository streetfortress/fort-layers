# The monitoring hub's ingest ports on the mesh interface (fort-layers,
# layers/monitoring/forts/<fort>.toml [monitoring.hub]): the central
# Prometheus's remote_write receiver (NodePort 30990) and Loki's push
# (31100), which flux/apps/<fort>/monitoring-central/mesh-services.yaml
# exposes. Mesh-only: the ingress path (Gateway hostnames, TLS) means
# nothing to a remote_write client, and the forts of this estate reach
# the hub at its mesh address. `fort monitoring scaffold <hub>` imports
# it and sets the one option.
{ config, lib, ... }:
let
  cfg = config.sfi.monitoring-hub;
in
{
  options.sfi.monitoring-hub.enable = lib.mkEnableOption "the monitoring hub's remote_write and Loki push ports on the mesh interface";

  config = lib.mkIf cfg.enable {
    networking.firewall.interfaces.${config.sfi.mesh.interface}.allowedTCPPorts = [ 30990 31100 ];
  };
}
