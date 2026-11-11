#!/bin/sh
# The lab's three containers, as .github/workflows/wsl.yml stops and starts
# them, run as root inside the lab:
#   slow    no restart policy, and 20 seconds to end after TERM: only
#           wsl-spawner brings it back, and only a stop that waits lets it end
#   policy  restart policy unless-stopped: Docker brings it back itself
#   manual  stopped by hand before the stop: nothing may start it again
set -eu
# Docker answers once it has started with the distribution.
i=0
until docker info >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -le 60 ] || { echo "Docker did not start"; exit 1; }
  sleep 1
done
# The image, from the root file system the workflow checked.
docker image inspect lab:1 >/dev/null 2>&1 || docker import /opt/lab/alpine.tar.gz lab:1 >/dev/null
# The slow one writes the time its TERM handler ended into the lab's folder.
docker run -d --network none --name slow --stop-timeout 60 -v /var/lib/lab:/out lab:1 \
  sh -c 'trap "sleep 20; date +%s >> /out/slow-ended; exit 0" TERM; while :; do sleep 1; done' >/dev/null
docker run -d --network none --name policy --restart unless-stopped lab:1 \
  sh -c 'trap "exit 0" TERM; while :; do sleep 1; done' >/dev/null
docker run -d --network none --name manual lab:1 \
  sh -c 'trap "exit 0" TERM; while :; do sleep 1; done' >/dev/null
docker stop -t 5 manual >/dev/null
docker ps -a --format '{{.Names}} {{.State}}'
