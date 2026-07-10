#!/bin/sh
# floe pre-hook example: export named Docker volumes as uncompressed tars
# (uncompressed so restic dedup works across days). Add to config.toml:
#   [hooks]
#   pre = ["~/.config/floe/hooks/export-docker-volumes.sh"]
set -eu
STAGING="$HOME/.local/state/floe/docker-volumes"
mkdir -p "$STAGING"
docker info >/dev/null 2>&1 || { echo "docker not running, skipping volume export"; exit 0; }
for v in $(docker volume ls -q); do
  docker run --rm -v "$v":/data:ro alpine tar cC /data . > "$STAGING/$v.tar"
done
echo "exported $(docker volume ls -q | wc -l | tr -d ' ') volumes to $STAGING"
