#!/bin/sh
set -eu
SBD_APP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$SBD_APP_DIR"
if [ -n "${STORYBOARD_GO_EXECUTABLE:-}" ]; then
  exec "$STORYBOARD_GO_EXECUTABLE" "$@"
fi
if [ -x "$SBD_APP_DIR/build/gosx/storyboard-desk-go" ]; then
  exec "$SBD_APP_DIR/build/gosx/storyboard-desk-go" "$@"
fi
if [ -x "$SBD_APP_DIR/gosx-host/storyboard-desk-go" ]; then
  exec "$SBD_APP_DIR/gosx-host/storyboard-desk-go" "$@"
fi
exec go -C "$SBD_APP_DIR/gosx-host" run . "$@"
