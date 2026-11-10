#!/usr/bin/env bash
# A local 3-node cluster on 127.0.0.1:8201-8203; binaries, WALs, logs and pid
# files live in .cluster/.
#
#   scripts/cluster.sh up           build and start all three nodes
#   scripts/cluster.sh kill <id>    SIGKILL one node: a crash, no clean shutdown
#   scripts/cluster.sh start <id>   start it again; it recovers from its WAL
#   scripts/cluster.sh down         stop everything
set -euo pipefail
cd "$(dirname "$0")/.."

dir=.cluster
peers=1=127.0.0.1:8201,2=127.0.0.1:8202,3=127.0.0.1:8203

start() {
  "$dir/raftkv" -id "$1" -peers "$peers" -data "$dir/data/$1" >>"$dir/node$1.log" 2>&1 </dev/null &
  echo $! >"$dir/node$1.pid"
  echo "node $1 started (pid $!, http://127.0.0.1:820$1)"
}

stop() { # stop <id> <signal>
  if [ -f "$dir/node$1.pid" ]; then
    kill "-$2" "$(cat "$dir/node$1.pid")" 2>/dev/null || true
    rm -f "$dir/node$1.pid"
    echo "node $1 stopped"
  fi
}

case "${1:-}" in
  up)
    mkdir -p "$dir"
    go build -o "$dir/raftkv" ./cmd/raftkv
    for id in 1 2 3; do start "$id"; done
    ;;
  start) start "${2:?node id}" ;;
  kill) stop "${2:?node id}" KILL ;;
  down) for id in 1 2 3; do stop "$id" TERM; done ;;
  *) echo "usage: $0 up | kill <id> | start <id> | down" >&2; exit 2 ;;
esac
