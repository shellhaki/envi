#!/bin/sh
# Runs the API and the install-script server together, because Coolify deploys
# one container per application and these two are one backend.
#
# If either process exits the container exits too, so the platform restarts the
# whole thing rather than leaving half the backend up and healthy-looking.
set -eu

envi-api &
api=$!
envi-install &
install=$!

# Distinguishes "the platform asked us to stop" from "a process fell over",
# so a normal restart does not look like a crash in the deploy log.
stopping=0
stop() { kill -TERM "$api" "$install" 2>/dev/null || true; }
shutdown() { stopping=1; stop; }
trap shutdown TERM INT

while kill -0 "$api" 2>/dev/null && kill -0 "$install" 2>/dev/null; do
  sleep 1
done

status=0
if [ "$stopping" -eq 1 ]; then
  wait "$api" 2>/dev/null || true
  wait "$install" 2>/dev/null || true
  exit 0
fi

if kill -0 "$api" 2>/dev/null; then
  wait "$install" || status=$?
  echo "envi-install exited with status ${status}; stopping the container" >&2
else
  wait "$api" || status=$?
  echo "envi-api exited with status ${status}; stopping the container" >&2
fi

stop
# A clean exit from one half is still a failure for the pair.
[ "$status" -eq 0 ] && status=1
exit "$status"
