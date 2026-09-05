# The subsystem-store API pod. Phase 1.
#
# 🔴 BUILT FROM devrc, NOT FROM homelab-talos, AND THAT IS THE WHOLE POINT.
# The proposal's premise is "the pod is the existing code, unmodified"
# (claudedocs/proposal-subsystem-store-homelab.md §1). `subsystem_recall.py`
# and its two dependencies live HERE, are gated by this repo's own suite
# (`tests/test_subsystem_{recall,resolver}.py`, thousands of
# assertions), and are what the workbench CLI runs. Vendoring a copy into
# homelab-talos would create a second tree that passes its own tests while
# drifting from the CLI — which is exactly the disagreement between local and
# remote output this design exists to make impossible.
#
# homelab-talos therefore holds MANIFESTS ONLY, plus a pinned image tag — the
# same split `clusters/homelab/apps/mailbox` already uses for mail-receiver.
#
# Build context is the REPO ROOT (the modules are under lib/), so:
#     docker build -f server/Dockerfile -t <tag> .
# `build-push.sh` next to this file does that and nothing else.
#
# 🔴 THE IMAGE CONTAINS CODE, NEVER STORE CONTENT. Every .py file is copied BY
# NAME — no `COPY . .`, so a stray entry file in a working tree cannot be baked
# into a layer and pushed to a registry. The store arrives at runtime, on a PVC,
# via `seed.sh`.
#
# 🔴 DO NOT TRIM A `COPY` BY EYE, AND DO NOT COUNT THEM HERE. This comment used
# to say "only four .py files" and was wrong; a first correction replaced the
# number with a different number, which is the same artefact one module later.
# There is no count in this file any more, on purpose. The authority is
# `test_subsystem_store_api.py::test_the_image_copies_every_module_it_needs`,
# which computes the TRANSITIVE closure of local `lib/` imports from
# `server.py`'s entrypoint and asserts BOTH this file and the (allowlist-shaped)
# `Dockerfile.dockerignore` cover it. Add an import, run that test.

FROM python:3.12-slim

# Stdlib only: `subsystem_recall` imports argparse/difflib/json/re/sys/pathlib
# and the local `subsystem_read_store`/`subsystem_resolver`/`entry_shape`,
# and shells out to nothing. There is no requirements.txt to install and there
# must not be one.
COPY lib/subsystem_recall.py     /app/lib/
COPY lib/subsystem_read_store.py /app/lib/
COPY lib/subsystem_resolver.py   /app/lib/
COPY lib/entry_shape.py          /app/lib/
COPY lib/host_identity.py        /app/lib/
COPY server/server.py            /app/server/

# `subsystem_touch` evaluates `DEFAULT_STORE_ROOT = Path.home() / ".claude" / …`
# at IMPORT time, and `Path.home()` raises when it can resolve neither $HOME nor
# a passwd entry — which is the ordinary state of a numeric-UID container. The
# value is never used here (every call passes `--store`), but the import would
# fail before anything could say so.
ENV HOME=/home/nonroot \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    SUBSYSTEM_STORE_ROOT=/data \
    SUBSYSTEM_STORE_PORT=8102 \
    SUBSYSTEM_STORE_TOKEN_FILE=/run/secrets/subsystem-store/token

RUN mkdir -p /home/nonroot /data && chown -R 65532:65532 /home/nonroot /data

# 65532 matches the clawgate precedent's runAsUser, so the same fsGroup story
# applies to the PVC.
USER 65532:65532

EXPOSE 8102

CMD ["python3", "/app/server/server.py"]
