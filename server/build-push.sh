#!/usr/bin/env bash
# Build + push the subsystem-store server image, so a deployment can pin a tag.
#
# The image is built from THIS repo (see the Dockerfile's header for why), with
# the repo root as the build context — the modules live under `lib/`.
#
# 🔴 THE REGISTRY HAS NO DEFAULT, AND THAT IS DELIBERATE IN A PUBLIC REPO. The
# script this was extracted from hardcoded one operator's internal registry
# hostname. Baking a site's infrastructure name into a public repository is
# exactly the class the leak gate exists to stop, and it is also simply wrong
# for anyone else who runs this. Pass it, or export it:
#
#   CAIRN_REGISTRY=registry.example.internal server/build-push.sh 0.8.0
#   CAIRN_REGISTRY=... server/build-push.sh 0.8.0 --no-push   # build only
#
# 🔴 THE TAG IS AN ARGUMENT AND HAS NO DEFAULT EITHER. A `:latest` default is
# how a mutable tag gets clobbered by a concurrent build and a pod silently
# restarts on somebody else's code. Pin an immutable version in the deployment.

set -euo pipefail

VERSION="${1:?usage: [CAIRN_REGISTRY=<host>] build-push.sh <version> [--no-push]}"
shift || true
PUSH=1
[[ "${1:-}" == "--no-push" ]] && PUSH=0

# 🔴 REQUIRED, and checked BEFORE the build so a 3-minute docker build does not
# run only to fail at the push. `:?` gives the operator the variable name.
REGISTRY="${CAIRN_REGISTRY:?CAIRN_REGISTRY is required — the registry host to build for, e.g. registry.example.internal. There is no default: this is a public repo and a hardcoded registry would be both a leak and wrong for every other operator.}"
IMAGE="$REGISTRY/library/subsystem-store-api:$VERSION"
# CDPATH= : a set CDPATH makes `cd` ECHO its destination, which would be
# captured into ROOT alongside the real path. Measured, not theorised.
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

echo "==> building $IMAGE from $ROOT"
docker build -f "$ROOT/server/Dockerfile" -t "$IMAGE" "$ROOT"

# 🔴 A CONTROL, NOT A COURTESY. The image must contain the code and NOT the
# store: this repo is public and a store holds private entries, so a layer that
# picked up a stray entry file would be pushed to a registry. `/data` must be
# EMPTY in the image — the store arrives at runtime on a volume.
leaked=$(docker run --rm --entrypoint sh "$IMAGE" -c 'ls -A /data | wc -l')
if [[ "$leaked" != "0" ]]; then
  echo "build-push: REFUSING TO PUSH — /data in the image is not empty ($leaked entries)." >&2
  exit 1
fi
echo "==> control: /data in the image holds $leaked files (must be 0) — OK"

# And the positive half: the code IS there and imports. A zero above from an
# image with no filesystem at all would look identical — that is the
# reassuring-zero shape, and one control alone cannot tell them apart.
docker run --rm "$IMAGE" python3 -c \
  'import sys; sys.path.insert(0, "/app/lib"); import subsystem_recall as r; print("==> control: subsystem_recall imported,", len(r.RECALL_MODES), "modes")'

# 🔴 THE RELOAD CONTROL, AND IT IS WHY THIS IMAGE EXISTS RATHER THAN THE OLD
# ONE. The deployment carries a paragraph saying the server CANNOT reload its
# token without a pod replace; that is true of an image built before the SIGHUP
# work and false of this one. Assert the capability is actually IN the image, so
# "we published the new server" and "the new server does the thing" are not the
# same unchecked claim. Retiring that paragraph is gated on this passing.
if ! docker run --rm --entrypoint sh "$IMAGE" -c 'grep -q "SIGHUP" /app/server/server.py'; then
  echo "build-push: REFUSING TO PUSH — no SIGHUP handling in the image's server.py." >&2
  echo "            This image predates the token hot-reload; a deployment that" >&2
  echo "            retires its no-reload paragraph against it would be wrong." >&2
  exit 1
fi
echo "==> control: the image's server.py carries SIGHUP token reload — OK"

if [[ $PUSH -eq 0 ]]; then
  echo "==> --no-push: built only. NOTHING was pushed."
  exit 0
fi

docker push "$IMAGE"
docker inspect --format '{{index .RepoDigests 0}}' "$IMAGE" 2>/dev/null || true
echo "==> pushed $IMAGE"
