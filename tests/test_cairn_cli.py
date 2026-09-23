#!/usr/bin/env python3
"""`scripts/cairn` — the read-through client's four states.

🔴 WHAT THIS FILE IS ACTUALLY GUARDING. Three of the four states print no
entries, and one of those three is a lie: `scope-empty` means the store was
reached and holds nothing, while `store-unreachable, no cache` means nothing was
read at all. If those render alike, `/resume` shows an empty screen for an
outage and the reader believes it. Every test below exists to keep them apart.

🔴 WHY THERE IS A PROXY IN THE FIXTURE. The server requires `CF-Connecting-IP`
and refuses an absent, forged or duplicated one — it is the rate limiter's key
and it fails closed. In production **Cloudflare** sets that header; the client
never does, and must never, or a real deployment would send a duplicate and be
refused. So the fixture puts a shim in front of the server that adds the header,
standing in for exactly the hop that adds it in production. A test that instead
taught the client to send it would be testing a client we must not ship.
"""

from __future__ import annotations

import http.server
import importlib.util
import io
import ipaddress
import os
import re
import socket
import subprocess
import sys
import time
import tarfile
import threading
import urllib.error
import urllib.request
from pathlib import Path
from types import SimpleNamespace

import pytest

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "scripts"))
from testlib import env_pin, store_siting  # noqa: E402
CAIRN_CLI = REPO / "cairn"
SERVER_PY = REPO / "server" / "server.py"
GOOD_TOKEN = "a" * 20 + "B" * 20 + "c" * 8

#: The host label BOTH sides of a byte-identity comparison must print. 🔴 IT IS
#: SET IN THIS PROCESS *AND* PASSED TO EVERY CHILD, AND ONE SIDE ALONE IS NOT A
#: FIX — that was measured, not reasoned about. `run_cairn` clears the client's
#: configuration from the child (`env_pin`), which includes the host label; a
#: test that renders the expected bytes IN-PROCESS then reads the operator's
#: label while the child reads `socket.gethostname()`, and the two `host:` lines
#: diverge. Pinning only the child swaps one divergence for another — applied and
#: watched still failing before this fixture was written.
#:
#: ⚠ AND IT IS STRICTLY BETTER THAN WHAT IT REPLACES. Before, the two sides
#: agreed because BOTH read the operator's real `$CAIRN_HOST` — agreement bought
#: by putting a real machine name through a test in a PUBLIC repository. Now both
#: read a synthetic one, which is the convention every other harness here already
#: follows (`CAPTURE_HOST`, `PARITY_HOST`, `DUALRUN_HOST`).
CLI_HOST = "cli-harness"


@pytest.fixture(autouse=True)
def _pin_the_host_label(monkeypatch):
    """Both sides of every comparison in this file read `CLI_HOST`.

    The names come from `env_pin.EXTRA_CONFIG_ENV`, which derives them from
    `host_identity.HOST_LABEL_ENV` — so a fourth host-label variable is cleared
    here on the day it is added, without anybody editing this file. They are
    cleared before the pin because `host_label()` returns the FIRST one set, so
    leaving `$ASIB_HOST` behind would decide the answer on some hosts and not
    others.
    """
    for name in env_pin.EXTRA_CONFIG_ENV:
        monkeypatch.delenv(name, raising=False)
    monkeypatch.setenv("CAIRN_HOST", CLI_HOST)

LOOPBACK = ipaddress.ip_network("127.0.0.1/32")


def _load_api():
    """Import `server.py` by path — its directory name has a hyphen in it.

    🔴 `sys.modules[spec.name] = module` BEFORE `exec_module`, and it is not
    bookkeeping. A module executed while absent from `sys.modules` is only
    MOSTLY imported, and CPython dereferences that entry without a `None` guard
    in places you do not go looking: `dataclasses._is_type` does
    `sys.modules.get(cls.__module__).__dict__` to decide whether a STRING
    annotation names `ClassVar`/`KW_ONLY`, so under `from __future__ import
    annotations` the first `@dataclass` in the file raised

        AttributeError: 'NoneType' object has no attribute '__dict__'

    at import — 43 collection errors in this file, none of them near the change
    that triggered them. `test_subsystem_store_api._load_server` has always
    registered; this loader is the second copy of that predicate and was the
    one that was wrong, which is the shape a duplicated predicate always takes.
    """
    sys.path.insert(0, str(REPO / "lib"))
    spec = importlib.util.spec_from_file_location("srv", SERVER_PY)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _entry(service: str, scope: str, nuance: str) -> str:
    return "\n".join(
        [
            "---",
            f"service: {service}",
            f"scope: {scope}",
            "sensitivity: internal",
            "---",
            "",
            "## What it is",
            f"The {service} component, described durably.",
            "",
            "## Pointers",
            f"- ops skill `manage-{service}` — invoke it for restarts",
            "",
            "## Nuance / work-history",
            nuance,
            "",
        ]
    )


@pytest.fixture
def source_store(tmp_path: Path):
    # 🔴 Sited via `testlib.store_siting`, not `tmp_path` directly: this file
    # stands up the real store server, so its writes fsync INSIDE the request
    # and a contended disk fails the gate on unrelated PRs. Falls back to
    # `tmp_path` where no tmpfs is usable, so it is never worse than before.
    with store_siting.store_root(tmp_path, "src") as root:
        yield _populate_source_store(root)


def _populate_source_store(root: Path) -> Path:
    (root / "widget-cfg").mkdir(parents=True)
    (root / "hollow-area").mkdir(parents=True)
    # 🔴 A SECOND *POPULATED* SCOPE, and it is load-bearing. The scope-filter
    # regression test asserts that `sync --scope X` does not narrow the shared
    # cache — but with `hollow-area` empty, a filtered cache and a complete one
    # hold the SAME `*/*.md` set, so the test passed against the broken code and
    # both re-threading mutants survived the whole suite. The measured original
    # failure was 305 entries -> 2 and needs a second scope with content in it.
    (root / "gizmo-notes").mkdir(parents=True)
    (root / "widget-cfg" / "thing-alpha.md").write_text(
        _entry("thing-alpha", "widget-cfg", "- 2026-01-02: probe lies for 40s.")
    )
    (root / "widget-cfg" / "thing-beta.md").write_text(
        _entry("thing-beta", "widget-cfg", "- 2026-01-03: sidecar drops its lease.")
    )
    (root / "gizmo-notes" / "other-thing.md").write_text(
        _entry("other-thing", "gizmo-notes", "- 2026-01-04: a different scope.")
    )
    return root


class _CloudflareShim(http.server.BaseHTTPRequestHandler):
    """Forwards to the real server, adding the header the edge adds."""

    upstream = ""

    def do_GET(self):  # noqa: N802
        req = urllib.request.Request(self.upstream + self.path, method="GET")
        for key, value in self.headers.items():
            if key.lower() not in ("host", "cf-connecting-ip"):
                req.add_header(key, value)
        req.add_header("CF-Connecting-IP", "203.0.113.7")
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                body, code, headers = resp.read(), resp.status, dict(resp.headers)
        except urllib.error.HTTPError as exc:
            body, code, headers = exc.read(), exc.code, dict(exc.headers)
        self.send_response(code)
        for key, value in headers.items():
            if key.lower() not in ("transfer-encoding", "connection", "content-length"):
                # 🔴 LOWERCASED, LIKE THE REAL EDGE. This shim used to forward
                # header names in whatever case the in-process server sent, so
                # it reproduced production's TOPOLOGY but not its
                # NORMALISATION — and HTTP/2 (which Cloudflare speaks) sends
                # every header name lowercased. That gap let a real defect ship:
                # the client did `dict(resp.headers).get("X-Store-Entries")`,
                # which returns None against a lowercase wire, so the freshness
                # stamp, the revision and — worst — the truncated-transfer count
                # check were all silently inert in production while 358 tests
                # passed. Found by the first live call after deploy, not here.
                self.send_header(key.lower(), value)
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


@pytest.fixture
def live_store(source_store: Path):
    """Yields `(base_url, stop)` — a real server behind a header-adding shim."""
    api = _load_api()
    httpd = api.build_server(
        host="127.0.0.1",
        port=0,
        store_root=str(source_store),
        tokens=(GOOD_TOKEN,),
        trusted_proxies=(LOOPBACK,),
        limiter=None,
        audit=None,
    )
    upstream = f"http://127.0.0.1:{httpd.server_address[1]}"
    threading.Thread(target=httpd.serve_forever, daemon=True).start()

    handler = type("Shim", (_CloudflareShim,), {"upstream": upstream})
    shim = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
    threading.Thread(target=shim.serve_forever, daemon=True).start()
    # Both URLs are yielded so a control can prove the shim is what makes the
    # difference. The previous fixture exposed only `base`, which is why the
    # "direct call is refused" control below asserted nothing of the kind.
    base = f"http://127.0.0.1:{shim.server_address[1]}"
    try:
        yield SimpleNamespace(base=base, upstream=upstream)
    finally:
        shim.shutdown()
        shim.server_close()
        httpd.shutdown()
        httpd.server_close()


def run_cairn(*args: str, url: str | None, cache: Path, token: str = GOOD_TOKEN):
    # Point config resolution at a path that does not exist, so a real
    # `~/.config/subsystem-store/env` on the developer's box can never make a
    # test pass. A test that reads the operator's live credentials is not a test.
    #
    # 🔴 THIS USED TO CLEAR EXACTLY ONE NAME AND WAS THE NARROWEST COPY IN THE
    # TREE. Measured with `CAIRN_ROUTES=/nonexistent/routes.json` exported and
    # this helper unconverted: **80 failed, 20 passed** across this file and
    # `test_cairn_write.py` — an explicit routing table that does not exist makes
    # the table MANDATORY, so every subprocess refuses before doing anything.
    # `env_pin` clears the whole configuration surface.
    env = env_pin.sanitized_env(
        CAIRN_HOST=CLI_HOST,
        SUBSYSTEM_STORE_TOKEN=token,
        SUBSYSTEM_STORE_CONFIG=str(cache.parent / "no-such-config"),
        SUBSYSTEM_STORE_URL=url or f"http://127.0.0.1:{_dead_port()}",
    )
    proc = subprocess.run(
        [sys.executable, str(CAIRN_CLI), "--cache", str(cache), "--timeout", "5", *args],
        capture_output=True,
        text=True,
        env=env,
        timeout=120,
    )
    return proc



ORPHAN_GRACE = 3600  # mirrors scripts/cairn::ORPHAN_GRACE_SECONDS


def _fetch_snapshot_bytes(base: str) -> bytes:
    """The real snapshot body, so truncation fixtures are built from real bytes."""
    req = urllib.request.Request(base + "/api/v1/snapshot")
    req.add_header("Authorization", f"Bearer {GOOD_TOKEN}")
    req.add_header("User-Agent", "subsystem-store-client/1")
    with urllib.request.urlopen(req, timeout=15) as resp:
        return resp.read()


def _dead_port() -> int:
    """A port nothing is listening on — bound then released, so it is real."""
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class TestTheFourStates:
    def test_live_says_live_and_exits_0(self, live_store, tmp_path: Path):
        proc = run_cairn("recall", "--scope", "widget-cfg", url=live_store.base,
                         cache=tmp_path / "cache")
        assert proc.returncode == 0, proc.stderr
        assert "cairn: live" in proc.stdout
        assert "thing-alpha" in proc.stdout

    def test_scope_empty_is_exit_0_and_says_it_REACHED_the_store(
        self, live_store, tmp_path: Path
    ):
        proc = run_cairn("recall", "--scope", "hollow-area", url=live_store.base,
                         cache=tmp_path / "cache")
        assert proc.returncode == 0, proc.stderr
        assert "scope-empty" in proc.stdout
        assert "reached the store" in proc.stdout

    def test_unreachable_WITH_a_cache_serves_it_and_says_STALE(
        self, live_store, tmp_path: Path
    ):
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        proc = run_cairn("recall", "--scope", "widget-cfg", url=None, cache=cache)
        assert proc.returncode == 0, proc.stderr
        assert "cairn: cached" in proc.stdout
        assert "SERVED FROM CACHE" in proc.stdout
        # The content still arrives — a stale answer is still an answer.
        assert "thing-alpha" in proc.stdout

    def test_unreachable_with_NO_cache_is_NONZERO_and_names_the_host(
        self, tmp_path: Path
    ):
        """🔴 The state that must never look like `scope-empty`."""
        proc = run_cairn("recall", "--scope", "widget-cfg", url=None,
                         cache=tmp_path / "cache")
        assert proc.returncode != 0
        assert "store-unreachable, no cache" in proc.stderr
        assert "127.0.0.1" in proc.stderr, "the reason must NAME the host"

    def test_the_two_empty_looking_states_do_NOT_render_alike(
        self, live_store, tmp_path: Path
    ):
        """🔴 The whole point, asserted as a RELATIONSHIP rather than two
        separate string checks: same shape of request, two different states,
        and they must differ in both text and exit code."""
        empty = run_cairn("recall", "--scope", "hollow-area", url=live_store.base,
                          cache=tmp_path / "a")
        outage = run_cairn("recall", "--scope", "hollow-area", url=None,
                           cache=tmp_path / "b")
        assert empty.returncode == 0 and outage.returncode != 0
        assert (empty.stdout + empty.stderr) != (outage.stdout + outage.stderr)


class TestIdempotence:
    def test_a_second_sync_transfers_the_same_set_and_exits_0(
        self, live_store, tmp_path: Path
    ):
        cache = tmp_path / "cache"
        first = run_cairn("sync", url=live_store.base, cache=cache)
        second = run_cairn("sync", url=live_store.base, cache=cache)
        assert first.returncode == 0 and second.returncode == 0
        listing = run_cairn("ls-entries", "--no-sync", url=None, cache=cache)
        assert listing.returncode == 0
        assert sorted(listing.stdout.split()) == [
            "gizmo-notes/other-thing.md",
            "widget-cfg/thing-alpha.md",
            "widget-cfg/thing-beta.md",
        ]

    def test_sync_is_atomic_enough_that_a_failure_keeps_the_OLD_cache(
        self, live_store, tmp_path: Path
    ):
        """A failed refresh must not leave a half-tree that still claims
        'none omitted'. The previous cache survives instead."""
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        before = sorted(p.name for p in cache.glob("*/*.md"))
        assert run_cairn("sync", url=None, cache=cache).returncode != 0
        after = sorted(p.name for p in cache.glob("*/*.md"))
        assert after == before, "a failed sync damaged the existing cache"


class TestByteIdentityWithTheLocalReader:
    def test_the_cached_digest_equals_the_SOURCE_digest(
        self, live_store, source_store: Path, tmp_path: Path
    ):
        """🔴 Criterion 2. The client must add its state line and NOTHING else:
        the report itself has to be what the unmodified reader produces from the
        source. Only the `store:` root line legitimately differs, for the reason
        `verify-byte-identity.sh` documents."""
        import re

        cache = tmp_path / "cache"
        proc = run_cairn("recall", "--scope", "widget-cfg", url=live_store.base, cache=cache)
        assert proc.returncode == 0, proc.stderr
        # Drop the client's banner + its blank line; compare the report only.
        got = proc.stdout.split("\n", 2)[2]

        api = _load_api()
        report = api.rc.recall(str(source_store), "widget-cfg", mode=api.rc.DEFAULT_MODE)
        want = api.rc.render_text(report)

        canon = lambda t: re.sub(r"^(\s*store:) .*$", r"\1 X", t, flags=re.M).strip()
        assert canon(got) == canon(want)


class TestInterpretersWithoutTarExtractionFilters:
    """The client is FETCHED BY SHA AND RUN ON SOMEBODY ELSE'S INTERPRETER, so
    "works on the interpreter I develop on" is not a property it may assume.

    `TarFile.extract(..., filter=...)` arrived in 3.12 and was backported to
    3.11.4 / 3.10.12 / 3.9.17 / 3.8.17. On anything older the kwarg is a
    TypeError — which on the sync path is a CRASH WITH A TRACEBACK, not one of
    this client's exit codes, so every `EXIT_*` contract the rest of this file
    pins is simply bypassed.

    🔴 THE SHIM BELOW IS THE INSTRUMENT, SO IT IS CONTROLLED BEFORE IT IS READ.
    `test_the_shim_really_removes_the_kwarg` is its negative control: it proves
    the shim makes `filter=` raise, because a shim that quietly did nothing would
    leave both tests below passing on the strength of the interpreter actually
    running them — the classic harness wired to nothing.
    """

    @staticmethod
    def _shim_dir(tmp_path: Path) -> Path:
        """A `sitecustomize` that makes this interpreter look pre-backport.

        Two halves, because the production failure has two halves: the module
        attribute `hasattr(tarfile, "data_filter")` branches on, and the method
        signature that raises. Removing only the attribute would exercise the
        fallback while leaving the real TypeError unreproduced — green for a
        reason that has nothing to do with the bug.
        """
        d = tmp_path / "py-shim"
        d.mkdir()
        (d / "sitecustomize.py").write_text(
            "import tarfile\n"
            "if hasattr(tarfile, 'data_filter'):\n"
            "    del tarfile.data_filter\n"
            "_real = tarfile.TarFile.extract\n"
            "def extract(self, member, path='', set_attrs=True, *,"
            " numeric_owner=False):\n"
            "    return _real(self, member, path, set_attrs,"
            " numeric_owner=numeric_owner)\n"
            "tarfile.TarFile.extract = extract\n"
        )
        return d

    def _run_pre_backport(self, *args, url, cache, tmp_path):
        shim = self._shim_dir(tmp_path)
        existing = os.environ.get("PYTHONPATH", "")
        env = env_pin.sanitized_env(
            CAIRN_HOST=CLI_HOST,
            SUBSYSTEM_STORE_TOKEN=GOOD_TOKEN,
            SUBSYSTEM_STORE_CONFIG=str(cache.parent / "no-such-config"),
            SUBSYSTEM_STORE_URL=url,
            PYTHONPATH=os.pathsep.join(p for p in (str(shim), existing) if p),
        )
        return subprocess.run(
            [sys.executable, str(CAIRN_CLI), "--cache", str(cache),
             "--timeout", "5", *args],
            capture_output=True, text=True, env=env, timeout=120,
        )

    def test_the_shim_really_removes_the_kwarg(self, tmp_path: Path):
        """NEGATIVE CONTROL for the two tests below — without this, a shim that
        silently did nothing would make them pass on a 3.12 interpreter and
        prove nothing at all."""
        shim = self._shim_dir(tmp_path)
        probe = (
            "import tarfile, io, sys\n"
            "assert not hasattr(tarfile, 'data_filter'), 'attribute survived'\n"
            "buf = io.BytesIO()\n"
            "with tarfile.open(fileobj=buf, mode='w') as t:\n"
            "    i = tarfile.TarInfo('a.md'); i.size = 1\n"
            "    t.addfile(i, io.BytesIO(b'x'))\n"
            "buf.seek(0)\n"
            "with tarfile.open(fileobj=buf) as t:\n"
            "    m = t.getmembers()[0]\n"
            "    try:\n"
            "        t.extract(m, sys.argv[1], filter='data')\n"
            "    except TypeError:\n"
            "        print('RAISED_TYPEERROR')\n"
            "    else:\n"
            "        print('ACCEPTED_THE_KWARG')\n"
        )
        # `sanitized_env`, not `dict(os.environ)` — this probe touches only
        # `tarfile` and would be correct either way, but a raw copy beside an
        # `env_pin` consumer is the shape `test_env_pin` refuses, and it refuses
        # it because that is how seven of them accumulated.
        env = env_pin.sanitized_env(
            PYTHONPATH=os.pathsep.join(
                p for p in (str(shim), os.environ.get("PYTHONPATH", "")) if p
            )
        )
        out = subprocess.run(
            [sys.executable, "-c", probe, str(tmp_path / "out")],
            capture_output=True, text=True, env=env, timeout=60,
        )
        assert "RAISED_TYPEERROR" in out.stdout, (out.stdout, out.stderr)

    def test_sync_SUCCEEDS_without_tar_extraction_filters(
        self, live_store, tmp_path: Path
    ):
        """🔴 THE REGRESSION TEST. Red before the fix with
        `TypeError: TarFile.extract() got an unexpected keyword argument
        'filter'` and a non-zero exit; green after.
        """
        cache = tmp_path / "cache"
        proc = self._run_pre_backport(
            "sync", url=live_store.base, cache=cache, tmp_path=tmp_path
        )
        assert proc.returncode == 0, (proc.returncode, proc.stderr)
        assert "Traceback" not in proc.stderr, proc.stderr
        assert "filter" not in proc.stderr, proc.stderr
        # …and it actually installed a store, rather than succeeding vacuously.
        assert list(cache.rglob("*.md")), "no entries landed in the cache"

    def test_the_fallback_still_strips_a_setuid_bit(
        self, live_store, tmp_path: Path
    ):
        """The one thing `filter="data"` still contributes on this path is MODE
        — the four guards above it already refuse links, non-regular members,
        traversal names and duplicates. So the fallback must not simply drop it.

        An INVARIANT guard, not a regression one: no measured bug ever landed a
        setuid file in the cache. It exists so the fallback cannot be
        "simplified" into a bare `tar.extract(member, staging)` later.
        """
        cache = tmp_path / "cache"
        proc = self._run_pre_backport(
            "sync", url=live_store.base, cache=cache, tmp_path=tmp_path
        )
        assert proc.returncode == 0, proc.stderr
        entries = list(cache.rglob("*.md"))
        assert entries, "no entries landed in the cache"
        for p in entries:
            mode = p.stat().st_mode & 0o7777
            assert mode == 0o644, f"{p} landed with mode {oct(mode)}"


class TestHostileOrBrokenArchives:
    """🔴 Every guard here was previously UNTESTED — an audit had to hand-build
    hostile tars to prove they fired at all. A guard nobody has watched work is
    a claim.

    They all assert the same relationship: a bad ARCHIVE is loud and distinct
    from an OUTAGE. An outage is benign and gets absorbed into "served from
    cache" at exit 0; a server shipping a link or a traversal member must never
    be.
    """

    @staticmethod
    def _serve_once(body: bytes, content_type: str, headers: dict | None = None):
        """A one-shot server returning exactly `body` with a 200."""
        class _H(http.server.BaseHTTPRequestHandler):
            def do_GET(self):  # noqa: N802
                self.send_response(200)
                self.send_header("content-type", content_type)
                self.send_header("content-length", str(len(body)))
                for k, v in (headers or {}).items():
                    # Lowercased like the real edge — see `_CloudflareShim`.
                    # Without this, a test can pass against a client whose
                    # header lookup only works for the case the FIXTURE happens
                    # to send, which is exactly how the production defect hid.
                    self.send_header(k.lower(), v)
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_a):
                pass

        srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _H)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        return srv, f"http://127.0.0.1:{srv.server_address[1]}"

    @staticmethod
    def _tar_with(member: tarfile.TarInfo, payload: bytes = b"x") -> bytes:
        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode="w:gz", format=tarfile.PAX_FORMAT) as tar:
            member.size = len(payload)
            tar.addfile(member, io.BytesIO(payload))
        return buf.getvalue()

    def _run_against(self, body, ctype, cache, headers=None):
        srv, url = self._serve_once(body, ctype, headers)
        try:
            return run_cairn("sync", url=url, cache=cache)
        finally:
            srv.shutdown()
            srv.server_close()

    def test_a_traversal_member_is_REFUSED_not_treated_as_an_outage(
        self, tmp_path: Path
    ):
        info = tarfile.TarInfo("../escaped.md")
        proc = self._run_against(
            self._tar_with(info), "application/gzip", tmp_path / "cache"
        )
        assert proc.returncode != 0
        assert "REFUSED" in proc.stderr, proc.stderr
        assert "cached" not in proc.stdout.lower()

    def test_a_symlink_member_is_REFUSED(self, tmp_path: Path):
        info = tarfile.TarInfo("widget-cfg/leak.md")
        info.type = tarfile.SYMTYPE
        info.linkname = "/etc/passwd"
        proc = self._run_against(
            self._tar_with(info, b""), "application/gzip", tmp_path / "cache"
        )
        assert proc.returncode != 0
        assert "REFUSED" in proc.stderr, proc.stderr

    def test_a_count_disagreeing_with_the_header_is_REFUSED(self, tmp_path: Path):
        """The server's comment claims a truncated transfer is 'visible as a
        disagreement'. It is only visible if somebody compares — this is the
        test that the comparison exists.

        🔴 AND IT DID NOT, IN PRODUCTION, FOR THE WHOLE OF #863. The client read
        the header with a case-SENSITIVE lookup, so behind Cloudflare (which
        lowercases every name over HTTP/2) `declared` was always None and this
        guard skipped the comparison entirely — while this very test passed
        locally, because the fixture sent the case the broken code wanted. Both
        test servers now lowercase, so this test finally exercises the shape
        production actually delivers. Found by a live call, not by 358 tests.
        """
        info = tarfile.TarInfo("widget-cfg/one.md")
        proc = self._run_against(
            self._tar_with(info),
            "application/gzip",
            tmp_path / "cache",
            headers={"X-Store-Entries": "99"},
        )
        assert proc.returncode != 0
        assert "99" in proc.stderr and "REFUSED" in proc.stderr, proc.stderr

    def test_a_200_that_is_NOT_a_tar_serves_the_cache_instead_of_crashing(
        self, live_store, tmp_path: Path
    ):
        """🔴 Realistic BECAUSE of this PR's own Cloudflare finding: the edge can
        answer 200 with an HTML interstitial. This previously escaped every
        handler as a traceback at exit 1 with a healthy cache sitting unused."""
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        proc = self._run_against(
            b"<html>Just a moment...</html>", "text/html", cache
        )
        assert "Traceback" not in proc.stderr, proc.stderr
        assert proc.returncode != 0                      # sync could not refresh
        assert "did not return an archive" in proc.stderr
        # and the good cache survived
        assert sorted(p.name for p in cache.glob("*/*.md")) == [
            "other-thing.md",
            "thing-alpha.md",
            "thing-beta.md",
        ]

    def test_a_legitimate_filename_containing_dots_is_NOT_refused(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """`".." in name` was over-broad: an ordinary entry called `a..b.md`
        aborted the whole sync and rendered as an outage. The server puts no
        constraint on entry filenames, so this is reachable with a real file."""
        (source_store / "widget-cfg" / "a..b.md").write_text(
            _entry("a..b", "widget-cfg", "- 2026-01-04: dots are legal.")
        )
        proc = run_cairn("sync", url=live_store.base, cache=tmp_path / "cache")
        assert proc.returncode == 0, proc.stderr + proc.stdout
        assert (tmp_path / "cache" / "widget-cfg" / "a..b.md").is_file()


class TestConcurrentSync:
    def test_ten_concurrent_syncs_never_leave_a_SHORT_cache(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 MEASURED FAILURE BEFORE THE FIX: 3 of 10 trials left 183/305,
        256/305 and 292/305 entries with no error, and others died with
        `FileExistsError`. The staging dir had a fixed name and was rmtree'd at
        the top of every run, so concurrent runs shared it. The reader then
        rendered that partial tree under its own 'none omitted' header.
        """
        # 🔴 THE SIZE IS THE TEST. A first version used 40 entries and PASSED
        # against the broken code — the extract window was too small for two
        # runs to collide, so it asserted nothing. The audit reproduced the
        # failure at 305 entries, so that is the fixture size: a race test whose
        # window is smaller than the race is a green that means nothing.
        for i in range(303):
            (source_store / "widget-cfg" / f"bulk-{i:03d}.md").write_text(
                _entry(f"bulk-{i:03d}", "widget-cfg", f"- 2026-01-05: item {i}.")
            )
        expected = len(list(source_store.glob("*/*.md")))
        assert expected >= 300, f"fixture too small to race: {expected}"
        cache = tmp_path / "cache"

        results: list = []
        threads = [
            threading.Thread(
                target=lambda: results.append(
                    run_cairn("sync", url=live_store.base, cache=cache)
                )
            )
            for _ in range(10)
        ]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        assert all(r.returncode == 0 for r in results), [
            (r.returncode, r.stderr[-300:]) for r in results if r.returncode != 0
        ]
        assert not any("Traceback" in r.stderr for r in results)
        got = len(list(cache.glob("*/*.md")))
        assert got == expected, f"cache truncated: {got}/{expected} entries"


class TestScopeFilterNeverNarrowsTheSharedCache:
    """🔴 `sync --scope X` and `validate --scope X` used to REPLACE the whole
    cache with one scope — measured 305 entries down to 2 — after which an
    offline recall of any other scope reported 'nothing recorded yet' at exit 0,
    a claim about the STORE derived from a filtered cache."""

    @pytest.mark.parametrize("cmd", [("sync",), ("validate",), ("ls-entries",)])
    def test_the_cache_stays_complete(self, live_store, tmp_path: Path, cmd):
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        before = sorted(p.name for p in cache.glob("*/*.md"))
        run_cairn(*cmd, "--scope", "widget-cfg", url=live_store.base, cache=cache)
        after = sorted(p.name for p in cache.glob("*/*.md"))
        assert after == before, f"{cmd[0]} --scope narrowed the shared cache"

    @pytest.mark.parametrize("cmd", [("sync",), ("validate",)])
    def test_the_OTHER_scope_still_answers_afterwards(
        self, live_store, tmp_path: Path, cmd
    ):
        """🔴 The PROPERTY, not the artifact. `test_the_cache_stays_complete`
        counts files; this asserts the consequence the original defect actually
        had — after a scoped command, an offline recall of a DIFFERENT scope
        must still find it, rather than reporting "nothing recorded yet" at
        exit 0 from a cache that was quietly narrowed.
        """
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        run_cairn(*cmd, "--scope", "widget-cfg", url=live_store.base, cache=cache)
        proc = run_cairn(
            "recall", "--scope", "gizmo-notes", "--no-sync", url=None, cache=cache
        )
        assert proc.returncode == 0, proc.stderr
        assert "other-thing" in proc.stdout, proc.stdout

    def test_the_stamp_records_that_the_cache_is_COMPLETE(
        self, live_store, tmp_path: Path
    ):
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        assert "coverage=ALL" in (cache / ".sync-stamp").read_text()


class TestReaderExitCodePassesThrough:
    def test_an_all_malformed_scope_exits_NONZERO_like_the_reader(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """The module docstring promised the reader's codes pass through while
        the code hardcoded 0. `/resume` branches on the CODE, so a machine
        consumer was told 'fine' for a scope nothing could be read from."""
        broken = source_store / "rubble-pile"
        broken.mkdir()
        (broken / "junk.md").write_text("no front matter, no headings, nothing\n")
        proc = run_cairn(
            "recall", "--scope", "rubble-pile", url=live_store.base,
            cache=tmp_path / "cache",
        )
        api = _load_api()
        report = api.rc.recall(str(source_store), "rubble-pile", mode=api.rc.DEFAULT_MODE)
        want = api.rc._exit_for(report.status, f"{report.scope}/", report.malformed)
        assert proc.returncode == want, (
            f"store exited {proc.returncode}, reader would exit {want}"
        )


class TestUnreadableScope:
    def test_an_unreadable_scope_is_NOT_reported_as_scope_empty(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 THE HEADLINE DEFECT. `Path.glob` swallows PermissionError and
        returns [], so a chmod-000 scope answered 200 / exit 0 with the scope
        silently omitted, and the client printed 'reached the store; nothing
        recorded'. That is the exact lie this whole client exists to prevent.
        """
        if os.geteuid() == 0:
            pytest.skip("root ignores directory permissions; the guard is unreachable")
        locked = source_store / "hollow-area"
        locked.chmod(0o000)
        try:
            proc = run_cairn(
                "recall", "--scope", "hollow-area", url=live_store.base,
                cache=tmp_path / "cache",
            )
            combined = proc.stdout + proc.stderr
            assert "scope-empty" not in combined, combined
            assert proc.returncode != 0, combined
            assert "hollow-area" in combined, combined
        finally:
            locked.chmod(0o755)


class TestSymlinkedScopeIsRefusedNotDropped:
    def test_a_symlinked_scope_dir_is_NOT_reported_as_scope_empty(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 THE DEFECT THE PREVIOUS FIX ROUND INTRODUCED.

        The symlink guard added to stop the server following links FILTERED
        symlinked scope dirs out of the candidate list, so they never reached
        the `unreadable` report and never reached the tar. Measured: a symlinked
        scope holding 2 entries rendered as `scope-empty — nothing recorded` at
        exit 0, while the PREVIOUS commit served them. The headline defect of
        this whole client, reintroduced one level up by the guard added to close
        it at the entry level. A guard that SKIPS is a silent omission.
        """
        outside = tmp_path / "outside"
        outside.mkdir()
        (outside / "sneaky.md").write_text(
            _entry("sneaky", "linked-scope", "- 2026-01-09: lives elsewhere.")
        )
        (source_store / "linked-scope").symlink_to(outside, target_is_directory=True)

        proc = run_cairn(
            "recall", "--scope", "linked-scope", url=live_store.base,
            cache=tmp_path / "cache",
        )
        combined = proc.stdout + proc.stderr
        assert "scope-empty" not in combined, combined
        assert proc.returncode != 0, combined
        assert "linked-scope" in combined, combined


class TestValidateActuallyRuns:
    def test_validate_exits_0_on_a_clean_cache(self, live_store, tmp_path: Path):
        """🔴 `cairn validate` NEVER WORKED — it passed `--validate` to the
        READER, which has no such flag, so every invocation exited 2 with
        `unrecognized arguments`. The test written to close that gap asserted
        only that the cache directory was unchanged, so it passed green over a
        command that failed on every input. Assert the OUTCOME."""
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        proc = run_cairn("validate", "--no-sync", url=None, cache=cache)
        assert proc.returncode == 0, proc.stdout + proc.stderr
        assert "unrecognized arguments" not in (proc.stdout + proc.stderr)

    def test_validate_REPORTS_WHAT_IT_CHECKED_and_the_count_MOVES(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 A CLEAN SCOPE USED TO PRINT NOTHING, WHICH IS BYTE-IDENTICAL TO A
        VALIDATE THAT PARSED NO FILES.

        `cmd_validate` printed a line per MALFORMED entry and nothing else, so
        success was silent and exit 0. That is the reassuring zero its own
        docstring says this command was rewritten to close — committed on the
        success path instead of the empty one. It matters more than an ordinary
        cosmetic gap because this command is the post-write check the
        index-write protocol MANDATES, so the silence was being read as "the
        entry I just wrote is fine".

        The assertion is on a count that MOVES with the store, not on a fixed
        string: a hardcoded literal would satisfy the first half and could not
        satisfy the second.
        """
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        before = run_cairn("validate", "--no-sync", url=None, cache=cache)
        assert before.returncode == 0, before.stdout + before.stderr
        out = before.stdout + before.stderr
        # The fixture store holds widget-cfg (2 entries) and gizmo-notes (1).
        assert "widget-cfg: 2 of 2 entry file(s) parse, 0 malformed" in out, out
        assert "gizmo-notes: 1 of 1 entry file(s) parse, 0 malformed" in out, out

        # 🔴 THE CONTROL. Add an entry, re-sync, and the SAME command must
        # report a DIFFERENT number. Without this a constant string passes.
        (source_store / "widget-cfg" / "thing-gamma.md").write_text(
            _entry("thing-gamma", "widget-cfg", "- 2026-01-05: a third entry.")
        )
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        after = run_cairn("validate", "--no-sync", url=None, cache=cache)
        assert after.returncode == 0, after.stdout + after.stderr
        assert "widget-cfg: 3 of 3 entry file(s) parse, 0 malformed" in (
            after.stdout + after.stderr
        ), after.stdout + after.stderr

    def test_the_count_EXCLUDES_the_scopes_README(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 THE NUMERATOR AND THE DENOMINATOR CAME FROM TWO DIFFERENT WALKS.

        Every scope directory carries a `README.md` as its policy sheet; the
        loader skips it in every scope, and `/snapshot` ships it, so it IS in the
        cache. This command took its rejections from the loader and its count
        from a `*.md` glob that included the README — so `widget-cfg`, holding
        two entries beside one policy sheet, printed `3 of 3 entry file(s)
        parse`.

        A count is the only evidence this command produces that anything was
        checked at all. One inflated by a file nothing parsed is the reassuring
        zero it exists to close, one layer up.
        """
        (source_store / "widget-cfg" / "README.md").write_text(
            "# widget-cfg — the scope's own policy sheet, not an entry\n"
        )
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        # 🔴 THE REACHABILITY CONTROL. If the snapshot dropped the README the
        # assertion below would pass over a cache the defect cannot reach, and
        # read as coverage while providing none.
        assert (cache / "widget-cfg" / "README.md").is_file(), sorted(
            p.name for p in (cache / "widget-cfg").iterdir()
        )

        proc = run_cairn("validate", "--scope", "widget-cfg", "--no-sync",
                         url=None, cache=cache)

        assert proc.returncode == 0, proc.stdout + proc.stderr
        out = proc.stdout + proc.stderr
        assert "widget-cfg: 2 of 2 entry file(s) parse, 0 malformed" in out, out

    def test_a_scope_holding_ONLY_a_README_reports_ZERO_walked(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """The sharpest form of the same defect: a directory with no entries in
        it printed `1 of 1 entry file(s) parse` — a clean bill of health over a
        scope the reader will render as empty."""
        (source_store / "hollow-area" / "README.md").write_text(
            "# hollow-area — a policy sheet and nothing else\n"
        )
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        assert (cache / "hollow-area" / "README.md").is_file()

        proc = run_cairn("validate", "--scope", "hollow-area", "--no-sync",
                         url=None, cache=cache)

        assert proc.returncode == 0, proc.stdout + proc.stderr
        out = proc.stdout + proc.stderr
        assert "hollow-area: 0 of 0 entry file(s) parse, 0 malformed" in out, out

    def test_the_MALFORMED_count_is_not_softened_by_a_README(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 THE DIRECTION THAT MISLEADS. With one entry and one README in a
        scope, a broken entry printed `1 of 2 … 1 malformed` — asserting that a
        file parsed when NONE had. The honest line is `0 of 1`.
        """
        (source_store / "gizmo-notes" / "README.md").write_text(
            "# gizmo-notes — policy sheet\n"
        )
        (source_store / "gizmo-notes" / "other-thing.md").write_text(
            "aliases: [wrapped,\n  list]\nno front matter at all\n"
        )
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        assert (cache / "gizmo-notes" / "README.md").is_file()

        proc = run_cairn("validate", "--scope", "gizmo-notes", "--no-sync",
                         url=None, cache=cache)

        assert proc.returncode != 0, proc.stdout + proc.stderr
        out = proc.stdout + proc.stderr
        assert "gizmo-notes: 0 of 1 entry file(s) parse, 1 malformed" in out, out

    def test_a_scope_with_NO_README_still_counts_every_entry(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 THE CONTROL THAT SEPARATES "EXCLUDE README.md" FROM "SUBTRACT ONE".

        Each of the three rows above holds EXACTLY ONE `README.md`, so
        `sum(...) - 1` prints the same line as the rule it is meant to
        implement, and survives all three. The arithmetic can only be told
        apart by a scope with NO README in it, where the correct count
        subtracts nothing and `- 1` loses a real entry.

        `widget-cfg` is seeded with two entries; a third is added here so this
        row's numbers (3 of 3) are distinct from every other row's and from
        what `- 1` would print (2 of 2) — which is also the constant the
        README-bearing `widget-cfg` row asserts, so the two rows must not be
        allowed to agree by arithmetic.
        """
        (source_store / "widget-cfg" / "thing-gamma.md").write_text(
            _entry("thing-gamma", "widget-cfg", "- 2026-01-05: a third entry.")
        )
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        # 🔴 THE REACHABILITY CONTROL, IN THE DIRECTION THIS ROW NEEDS IT: the
        # scope must hold three entries and NO README at all, or the row is a
        # second sample of the README-bearing case and discriminates nothing.
        landed = sorted(p.name for p in (cache / "widget-cfg").iterdir())
        assert landed == ["thing-alpha.md", "thing-beta.md", "thing-gamma.md"], landed

        proc = run_cairn("validate", "--scope", "widget-cfg", "--no-sync",
                         url=None, cache=cache)

        assert proc.returncode == 0, proc.stdout + proc.stderr
        out = proc.stdout + proc.stderr
        assert "widget-cfg: 3 of 3 entry file(s) parse, 0 malformed" in out, out

    def test_a_README_LOOKALIKE_is_an_ordinary_entry_and_is_COUNTED(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 THE PREDICATE IS `== "README.md"` EXACTLY, AND THAT CLAIM IS ONLY
        A COMMENT UNTIL A ROW HOLDS A LOOKALIKE.

        Both clients say in prose that the spelling is the loader's — "not a
        prefix, not a fold". Nothing pinned it, and a case-folded prefix match
        passes every other row in this class: `readme.md` and `README-old.md`
        are ORDINARY ENTRIES to the loader, so excluding them from the count
        while the loader still walks them drives the printed numerator BELOW
        ZERO the moment one of them is malformed.

        This scope holds one real policy sheet (excluded), two lookalikes and
        two ordinary entries (four counted) — five files, four counted, three
        README-shaped names, two plain ones: no two of those numbers are equal,
        and none of them equals the `4 of 4` the assertion names.
        """
        (source_store / "gizmo-notes" / "README.md").write_text(
            "# gizmo-notes — the scope's own policy sheet, not an entry\n"
        )
        # ⚠ `service:` must normalize to the filename's own slug or the loader
        # rejects the entry as malformed ("a ref reaches the wrong file"), so a
        # genuine entry at `readme.md` is `service: readme`. That is the shape a
        # real store would carry, and it is what makes these two rows entries
        # rather than a second spelling of the policy sheet.
        (source_store / "gizmo-notes" / "readme.md").write_text(
            _entry("readme", "gizmo-notes", "- 2026-01-06: a real entry.")
        )
        (source_store / "gizmo-notes" / "README-old.md").write_text(
            _entry("readme-old", "gizmo-notes", "- 2026-01-07: also an entry.")
        )
        (source_store / "gizmo-notes" / "spare-thing.md").write_text(
            _entry("spare-thing", "gizmo-notes", "- 2026-01-08: a plain entry.")
        )
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        # 🔴 THE REACHABILITY CONTROL. `/snapshot` ships by name (`*.md`, no
        # dotfiles), so the lookalikes must actually be in the cache — a
        # snapshot that dropped them would leave this row asserting over the
        # ordinary case and reading as coverage while providing none.
        landed = sorted(p.name for p in (cache / "gizmo-notes").iterdir())
        assert landed == [
            "README-old.md",
            "README.md",
            "other-thing.md",
            "readme.md",
            "spare-thing.md",
        ], landed

        proc = run_cairn("validate", "--scope", "gizmo-notes", "--no-sync",
                         url=None, cache=cache)

        assert proc.returncode == 0, proc.stdout + proc.stderr
        out = proc.stdout + proc.stderr
        assert "gizmo-notes: 4 of 4 entry file(s) parse, 0 malformed" in out, out

    def test_validate_exits_NONZERO_on_a_malformed_cache(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """Negative control: a validator that never fails is not a validator."""
        (source_store / "widget-cfg" / "broken.md").write_text(
            "aliases: [wrapped,\n  list]\nno front matter at all\n"
        )
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        proc = run_cairn("validate", "--no-sync", url=None, cache=cache)
        assert proc.returncode != 0, proc.stdout + proc.stderr


class TestArchiveSizeAndTruncation:
    def test_a_truncated_GZIP_serves_the_cache_instead_of_a_traceback(
        self, live_store, tmp_path: Path
    ):
        """🔴 The gzip switch reopened the crash finding one exception type over.
        A short body used to raise `tarfile.ReadError` (handled); compressed it
        raises `EOFError`, which was not, so it escaped as a traceback at exit 1
        with a healthy cache unused."""
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        good = _fetch_snapshot_bytes(live_store.base)
        truncated = good[: len(good) // 2]
        proc = TestHostileOrBrokenArchives()._run_against(
            truncated, "application/gzip", cache
        )
        assert "Traceback" not in proc.stderr, proc.stderr
        assert proc.returncode != 0
        assert sorted(p.name for p in cache.glob("*/*.md")), "cache was destroyed"

    def test_a_decompression_BOMB_is_refused(self, tmp_path: Path):
        """🔴 Gzip removed the natural bound: before it, the response body
        limited what could be extracted. Measured on the previous commit: a
        203,934-byte body wrote 209,715,200 bytes to disk and reported
        `live … 1 entries`, exit 0."""
        buf = io.BytesIO()
        payload = b"\0" * (300 * 1024 * 1024)
        with tarfile.open(fileobj=buf, mode="w:gz", format=tarfile.PAX_FORMAT) as tar:
            info = tarfile.TarInfo("widget-cfg/bomb.md")
            info.size = len(payload)
            tar.addfile(info, io.BytesIO(payload))
        body = buf.getvalue()
        assert len(body) < 2 * 1024 * 1024, "fixture is not actually compressed"
        proc = TestHostileOrBrokenArchives()._run_against(
            body, "application/gzip", tmp_path / "cache"
        )
        assert proc.returncode != 0, proc.stdout
        assert "REFUSED" in proc.stderr and "ceiling" in proc.stderr, proc.stderr
        assert not list((tmp_path / "cache").glob("*/*.md"))


class TestNonRegularFilesInTheStore:
    def test_a_directory_named_md_is_REFUSED_by_name_not_by_errno(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 RENAMED — the old name claimed a property the code does not have.

        It was `test_a_directory_named_md_does_not_503_the_whole_store`, and it
        does 503 the whole store: the client always requests unfiltered, so one
        bad file in ANY scope denies every scope. (The SERVER isolates correctly
        — raw `?scope=healthy` returns 200 — but no client path reaches that.)
        The body only ever asserted the refusal WORDING, which is the real,
        deliberate property. A test whose name asserts availability while its
        body asserts classification is the "description wider than the
        implementation" pattern, in a test written to close a finding about it.

        What is pinned: the failure is a CLASSIFIED refusal naming the offender,
        not a raw `[Errno 21] Is a directory` escaping from an unguarded open.
        """
        (source_store / "widget-cfg" / "trap.md").mkdir()
        proc = run_cairn(
            "recall", "--scope", "gizmo-notes", url=live_store.base,
            cache=tmp_path / "cache",
        )
        combined = proc.stdout + proc.stderr
        # 🔴 ASSERT THE REFUSAL WORDING, not merely "it did not crash". At
        # 44eb5841 this produced `store unreadable: [Errno 21] Is a directory:
        # …/trap.md` — which also names the file and also avoids the literal
        # word "IsADirectoryError", so a test asserting only those two things
        # passed against the broken code. It has to pin the classified refusal.
        # The refusal now speaks the classifier's vocabulary (`directory
        # refused`) rather than an ad-hoc sentence. What is pinned is unchanged:
        # a CLASSIFIED refusal naming the offender, not a raw `[Errno 21] Is a
        # directory` escaping from an unguarded `open()`.
        assert "directory refused" in combined, combined
        assert "trap.md" in combined, combined
        assert "Errno 21" not in combined, combined


class TestEntryTableCellsHaveBehaviour:
    """🔴 Three `_ENTRY_ACTIONS` cells were pinned ONLY by the constants test.

    Measured: flipping `KIND_LINK_TO_FILE` REFUSE -> TAKE and deselecting
    `test_the_decision_table_is_pinned` left **342 passed, 0 failed**, while the
    server happily shipped a file from OUTSIDE the store:

        unmutated: 503 "widget-cfg/innocent.md: link-to-file refused"
        mutant:    200, member content "BEGIN OPENSSH PRIVATE KEY …"

    `entry_broken_link -> SKIP` and `entry_other -> SKIP` survived the same way,
    the latter being the FIFO whose own comment says it "blocked open() forever,
    leaking a handler thread permanently". A constants assertion is exactly the
    kind a future edit updates ALONGSIDE the code it was meant to stop, so each
    cell needs an observable of its own.
    """

    def _snapshot_raw(self, base: str) -> bytes | None:
        """The raw body on a 200, else None — so a leak assertion can DECOMPRESS
        rather than grep a gzip stream for plaintext it can never contain."""
        req = urllib.request.Request(base + "/api/v1/snapshot")
        req.add_header("Authorization", f"Bearer {GOOD_TOKEN}")
        req.add_header("User-Agent", "subsystem-store-client/1")
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                return resp.read()
        except urllib.error.HTTPError:
            return None

    def _snapshot_members(self, base: str) -> tuple[int, str]:
        req = urllib.request.Request(base + "/api/v1/snapshot")
        req.add_header("Authorization", f"Bearer {GOOD_TOKEN}")
        req.add_header("User-Agent", "subsystem-store-client/1")
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                return resp.status, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as exc:
            return exc.code, exc.read().decode("utf-8", "replace")

    def test_a_symlinked_entry_never_ships_an_OFF_STORE_file(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 The security-relevant cell. `/snapshot` walks EVERY scope in one
        authenticated GET, where `/recall` makes you name one."""
        secret = tmp_path / "id_ed25519"
        secret.write_text("BEGIN OPENSSH PRIVATE KEY — outside the store")
        (source_store / "widget-cfg" / "innocent.md").symlink_to(secret)

        # 🔴 THE LEAK CHECK RUNS FIRST, AND THE ORDER IS THE WHOLE FIX.
        #
        # v1 of this line was `assert "OPENSSH PRIVATE KEY" not in body` — which
        # CANNOT fail on a 200, because `/snapshot` ships `w:gz` and the
        # plaintext is never in the raw bytes. v2 decompressed correctly but sat
        # BELOW `assert code == 503`, which dominates it: with the mutant the
        # test dies on the status line and the leak block never executes.
        # Measured both ways — unmutated, `raw is None` so the block was skipped;
        # mutated, pytest never reached it. So v2 left the suite byte-for-byte
        # as it was before v1 was "fixed": an assertion that cannot fail became
        # an assertion that does not run.
        #
        # Hoisted above the status assert, and no `if raw is not None` guard —
        # that guard was a second inertia path (`_snapshot_raw` returns None on
        # ANY HTTPError, so a 429 from the limiter would silently skip it too).
        raw = self._snapshot_raw(live_store.base)
        if raw is not None:  # a 200 came back: it MUST NOT carry the secret
            with tarfile.open(fileobj=io.BytesIO(raw), mode="r") as tar:
                names = tar.getnames()
                for member in tar.getmembers():
                    handle = tar.extractfile(member)
                    content = handle.read() if handle is not None else b""
                    assert b"OPENSSH PRIVATE KEY" not in content, member.name
                assert "widget-cfg/innocent.md" not in names, names
            raise AssertionError(
                "/snapshot answered 200 for a store containing a symlinked "
                f"entry; it must refuse. members={names}"
            )

        code, body = self._snapshot_members(live_store.base)
        assert code == 503, body[:300]
        assert "innocent.md" in body and "link-to-file refused" in body, body[:300]

    def test_a_FIFO_named_md_is_refused_rather_than_opened(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """A FIFO blocks `open()` forever and leaks a handler thread on a
        threading server. Refused means the request RETURNS."""
        os.mkfifo(source_store / "widget-cfg" / "pipe.md")
        code, body = self._snapshot_members(live_store.base)
        assert code == 503, body[:300]
        assert "pipe.md" in body and "other refused" in body, body[:300]

    def test_a_dangling_ENTRY_link_is_refused_not_skipped(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """Non-dotfile, so the name rules do not filter it first — this reaches
        the type table, unlike the Emacs lock-file case."""
        (source_store / "widget-cfg" / "gone.md").symlink_to(tmp_path / "nowhere")
        code, body = self._snapshot_members(live_store.base)
        assert code == 503, body[:300]
        assert "gone.md" in body and "broken-link refused" in body, body[:300]


class TestOrphanStagingIsReaped:
    def test_an_old_staging_dir_is_removed_by_the_next_sync(
        self, live_store, tmp_path: Path
    ):
        """🔴 Per-run staging names fixed the truncation race but removed the
        self-healing the fixed name had: an audit SIGKILLed three syncs and the
        orphans survived every later clean run. With a sync timer they grow
        without bound in `~/.cache`."""
        cache = tmp_path / "cache"
        orphan = cache.parent / f"{cache.name}.new-deadbeef"
        orphan.mkdir(parents=True)
        (orphan / "junk.md").write_text("left behind by a SIGKILL")
        old = time.time() - (ORPHAN_GRACE + 60)
        os.utime(orphan, (old, old))

        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        assert not orphan.exists(), "orphan staging dir survived a clean sync"

    def test_an_old_DOT_OLD_orphan_is_also_removed(self, live_store, tmp_path: Path):
        """The `.old-*` half of the glob had no test: dropping that prefix
        SURVIVED the whole suite, so half the reaper was unguarded."""
        cache = tmp_path / "cache"
        orphan = cache.parent / f"{cache.name}.old-deadbeef"
        orphan.mkdir(parents=True)
        old = time.time() - (ORPHAN_GRACE + 60)
        os.utime(orphan, (old, old))
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        assert not orphan.exists(), ".old- orphan survived a clean sync"

    def test_a_symlinked_orphan_is_UNLINKED_not_counted_as_reaped(
        self, live_store, tmp_path: Path
    ):
        """🔴 The fix for this was exactly as invisible as the bug: BOTH mutants
        (`lstat`->`stat`, and dropping the unlink arm) survived all 333 tests.

        `rmtree` on a symlink is a silent no-op, so the old code counted the
        orphan as reaped while never removing it — and the count is the only
        evidence, which is what made the miscount invisible. Asserts the link is
        gone AND the target survives, because unlinking the wrong one is the
        obvious way to "fix" this and lose data.
        """
        cache = tmp_path / "cache"
        cache.parent.mkdir(parents=True, exist_ok=True)
        target = tmp_path / "precious"
        target.mkdir()
        (target / "keep.md").write_text("must survive")
        link = cache.parent / f"{cache.name}.new-symlinked"
        link.symlink_to(target, target_is_directory=True)
        old = time.time() - (ORPHAN_GRACE + 60)
        os.utime(link, (old, old), follow_symlinks=False)

        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        assert not link.exists() and not link.is_symlink(), "symlinked orphan survived"
        assert (target / "keep.md").read_text() == "must survive", "reaper ate the target"

    def test_a_RECENT_staging_dir_is_left_alone(self, live_store, tmp_path: Path):
        """🔴 Negative control, and it pins the grace period FROM BELOW — the
        direction the docstring's whole safety argument rests on. Shrinking the
        grace to 1 second SURVIVED the previous suite, because the fixture aged
        its "recent" dir ~0 s and so sat on its own boundary: a 1-second grace
        would delete a live concurrent sync's staging dir with the suite green.

        Aged to just INSIDE the window instead, so the assertion is about the
        grace period rather than about scheduling luck.
        """
        cache = tmp_path / "cache"
        fresh = cache.parent / f"{cache.name}.new-inflight"
        fresh.mkdir(parents=True)
        recent = time.time() - (ORPHAN_GRACE // 2)
        os.utime(fresh, (recent, recent))
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        assert fresh.exists(), "reaper deleted a possibly-live staging dir"


class TestNotAScopeIsSkippedNotRefused:
    """🔴 The predicate the `unreadable` list must encode: REFUSE a thing that
    is a scope/entry but cannot be served safely; SKIP a thing that is not one.

    Round 3 got the order wrong and tested `is_symlink()` before `is_dir()`, so
    a symlinked `README.md` at the store root — not a scope, never was — took
    the whole snapshot from exit 0 to exit 3 for every caller and every scope,
    while a plain `README.md` in the same place was still skipped silently. Two
    spellings of "not a scope", opposite outcomes.
    """

    def test_a_symlinked_FILE_at_the_store_root_is_skipped(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        target = tmp_path / "notes.md"
        target.write_text("not a scope, just a file")
        (source_store / "README.md").symlink_to(target)
        proc = run_cairn(
            "recall", "--scope", "widget-cfg", url=live_store.base,
            cache=tmp_path / "cache",
        )
        assert proc.returncode == 0, proc.stdout + proc.stderr
        assert "thing-alpha" in proc.stdout

    def test_a_plain_FILE_at_the_store_root_is_skipped_too(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """The other half of the relationship — asserted so the two cannot drift
        apart again. Either both skip or the guard is inconsistent."""
        (source_store / "README.md").write_text("not a scope")
        proc = run_cairn(
            "recall", "--scope", "widget-cfg", url=live_store.base,
            cache=tmp_path / "cache",
        )
        assert proc.returncode == 0, proc.stdout + proc.stderr

    def test_an_editor_lock_file_does_not_deny_the_SNAPSHOT_path(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 An Emacs lock file is `.#entry.md` — a DANGLING SYMLINK whose name
        ends in `.md`. The entry-level symlink refusal made one open buffer 503
        the entire store for every caller. Reproduced at two earlier commits, so
        it predates this fix; it shares the root cause above.

        ⚠ NAMED FOR THE PATH IT ACTUALLY COVERS. `/api/v1/recall/<scope>` is
        STILL affected: `load_index` uses `scope_dir.glob("*.md")`, and pathlib
        glob DOES match a leading dot, so a lock file still 503s that route. The
        fix lives in `lib/subsystem_resolver.py`, which this card's
        non-goals forbid touching ("the local store is alive and heavily used;
        this task moves files between hosts"). Calling this
        `..._does_not_deny_the_whole_store` would be the exact
        description-wider-than-implementation defect an earlier round was
        renamed to close. Closing condition: `cairn recall` and
        `/api/v1/recall/<scope>` both serve a scope containing `.#x.md`.
        """
        (source_store / "widget-cfg" / ".#thing-alpha.md").symlink_to(
            "zach@nixos.12345:1700000000"
        )
        proc = run_cairn(
            "recall", "--scope", "widget-cfg", url=live_store.base,
            cache=tmp_path / "cache",
        )
        assert proc.returncode == 0, proc.stdout + proc.stderr
        assert "thing-alpha" in proc.stdout

    @pytest.mark.parametrize("shape", ["dangling", "loop"])
    def test_a_BROKEN_scope_pointer_is_refused_not_silently_skipped(
        self, source_store: Path, live_store, tmp_path: Path, shape: str
    ):
        """🔴 THE ROUND-4 REGRESSION, pinned in both directions.

        `is_dir()` returns False for a dangling symlink AND for a symlink loop —
        `pathlib` swallows ENOENT and ELOOP — so reordering the guard to check
        `is_dir()` first made both vanish from the snapshot silently. Measured
        across the two commits, same fixture:

            bc2364eb: dangling scope link -> rc 3, "symlink refused"
            a17c5df7: dangling scope link -> rc 0, "scope-empty — nothing recorded"

        A directory entry named `<scope>` exists at the store root and nothing
        anywhere reported that its target was gone. That is this PR's headline
        defect, one state over, and the suite could not see it in EITHER
        direction — the mutant flipping it back survived all 333 tests.
        """
        if shape == "dangling":
            (source_store / "linked").symlink_to(tmp_path / "does-not-exist")
        else:
            (source_store / "linked").symlink_to(source_store / "linked")

        proc = run_cairn(
            "recall", "--scope", "linked", url=live_store.base,
            cache=tmp_path / "cache",
        )
        combined = proc.stdout + proc.stderr
        assert "scope-empty" not in combined, combined
        assert proc.returncode != 0, combined
        assert "linked/: broken-link refused" in combined, combined

    def test_a_symlinked_SCOPE_is_still_refused(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """Negative control for the reorder. Skipping non-scopes must not have
        also started skipping symlinked SCOPES — that was round 2's defect and
        this reorder is exactly the shape of change that could undo its fix."""
        outside = tmp_path / "outside"
        outside.mkdir()
        (outside / "sneaky.md").write_text(_entry("sneaky", "linked", "- x."))
        (source_store / "linked").symlink_to(outside, target_is_directory=True)
        proc = run_cairn(
            "recall", "--scope", "linked", url=live_store.base,
            cache=tmp_path / "cache",
        )
        combined = proc.stdout + proc.stderr
        assert proc.returncode != 0, combined
        assert "scope-empty" not in combined, combined


class TestInodeBomb:
    def test_an_archive_with_too_MANY_members_is_refused(self, tmp_path: Path):
        """🔴 The byte ceiling bounds nothing here: `m.size` is 0 for an empty
        member. Measured against the previous commit — a 282,282-byte body
        carrying 60,000 zero-length `*.md` members wrote 60,000 files into the
        cache and reported `live … 60000 entries`, exit 0."""
        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode="w:gz", format=tarfile.PAX_FORMAT) as tar:
            for i in range(60_000):
                tar.addfile(tarfile.TarInfo(f"widget-cfg/e{i:06d}.md"), io.BytesIO(b""))
        proc = TestHostileOrBrokenArchives()._run_against(
            buf.getvalue(), "application/gzip", tmp_path / "cache"
        )
        assert proc.returncode != 0, proc.stdout
        assert "REFUSED" in proc.stderr and "member" in proc.stderr, proc.stderr
        assert not list((tmp_path / "cache").glob("*/*.md"))

    def test_the_byte_ceiling_sums_rather_than_maxes(self, tmp_path: Path):
        """🔴 `sum` -> `max` SURVIVED the previous suite because the bomb fixture
        was a SINGLE member, which makes the two indistinguishable. Many
        moderate members is the case that tells them apart."""
        buf = io.BytesIO()
        chunk = b"\0" * (4 * 1024 * 1024)
        with tarfile.open(fileobj=buf, mode="w:gz", format=tarfile.PAX_FORMAT) as tar:
            for i in range(80):  # 320 MB total, no single member over the cap
                info = tarfile.TarInfo(f"widget-cfg/big{i:03d}.md")
                info.size = len(chunk)
                tar.addfile(info, io.BytesIO(chunk))
        proc = TestHostileOrBrokenArchives()._run_against(
            buf.getvalue(), "application/gzip", tmp_path / "cache"
        )
        assert proc.returncode != 0, proc.stdout[:400]
        assert "ceiling" in proc.stderr, proc.stderr


class TestValidateScopeGuard:
    def test_validate_on_a_scope_the_cache_does_NOT_hold_is_nonzero(
        self, live_store, tmp_path: Path
    ):
        """🔴 The silent zero `cmd_validate` was rewritten to close, still open
        on the explicit `--scope` branch: it printed the writer's own "NOTHING
        WAS CHECKED — a zero here is NOT a clean bill of health" and exited 0.
        The fix covered the case it was looking at, not the predicate."""
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        proc = run_cairn(
            "validate", "--scope", "no-such-scope", "--no-sync", url=None, cache=cache
        )
        assert proc.returncode != 0, proc.stdout + proc.stderr
        assert "no-such-scope" in proc.stderr, proc.stderr


class TestSearchOverTheClient:
    """🔴 `cairn search` had ZERO tests anywhere in the repo, so the fix that
    gave search its own `_exit_for` label had no regression test at all."""

    def test_search_finds_a_hunk_and_exits_0(self, live_store, tmp_path: Path):
        proc = run_cairn(
            "search", "lease", "--scope", "widget-cfg", url=live_store.base,
            cache=tmp_path / "cache",
        )
        assert proc.returncode == 0, proc.stdout + proc.stderr
        assert "thing-beta" in proc.stdout, proc.stdout

    def test_search_on_an_all_malformed_scope_names_the_SCOPE_not_the_query(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 `_exit_for` was handed the QUERY where the reader passes
        `SearchReport.label`, so the failure sentence read "`lease` holds 1
        entry file" — naming the search term as if it were a scope path."""
        broken = source_store / "rubble-pile"
        broken.mkdir()
        (broken / "junk.md").write_text("no front matter, nothing parseable\n")
        proc = run_cairn(
            "search", "lease", "--scope", "rubble-pile", url=live_store.base,
            cache=tmp_path / "cache",
        )
        combined = proc.stdout + proc.stderr
        assert "`lease`" not in combined, combined
        assert "rubble-pile" in combined, combined

    def test_the_search_label_is_the_READERS_label_not_a_lookalike(
        self, source_store: Path, live_store, tmp_path: Path
    ):
        """🔴 The previous version of the test above pinned only "the query is
        not named", which `label = f"{report.scope}/"` also satisfies — so that
        mutant SURVIVED all 333 tests. `SearchReport.label` is a PROPERTY over
        `scopes_searched`, and for `--all-scopes` it differs from any single
        scope, which is the case that tells the two apart.
        """
        # 🔴 EVERY scope must be unreadable, because the label only reaches the
        # output through `_exit_for`'s FAILURE sentence — a successful search
        # prints no label at all, so an all-scopes search that finds anything
        # cannot discriminate. This is the fixture the property actually needs.
        for existing in source_store.glob("*/*.md"):
            existing.unlink()
        for scope in ("widget-cfg", "gizmo-notes"):
            (source_store / scope / "junk.md").write_text("nothing parseable\n")
        broken = source_store / "rubble-pile"
        broken.mkdir()
        (broken / "junk.md").write_text("no front matter, nothing parseable\n")
        # 🔴 `--all-scopes` IS THE DISCRIMINATING CASE, and the previous version
        # of this test named it in its docstring and then did not use it:
        #     all_scopes=False -> report.label == f"{report.scope}/"  IDENTICAL
        #     all_scopes=True  -> "gizmo-notes/, hollow-area/, …" vs "(all scopes)/"
        # so the fixture could only ever produce the lookalike's own value, and
        # the `report.label -> f"{report.scope}/"` mutant passed 349/349. A
        # fixture that cannot distinguish the mutant from the fix is not a test.
        cache = tmp_path / "cache"
        proc = run_cairn(
            "search", "lease", "--scope", "rubble-pile", "--all-scopes",
            url=live_store.base, cache=cache,
        )
        # 🔴 EXPECTATION DERIVED FROM WHAT THE CLI ACTUALLY READ — the CACHE, not
        # the source. An EMPTY scope directory never ships in the tar (only
        # `*.md` members do), so `hollow-area` exists in the source and not in
        # the cache, and a label computed from the source names a scope the CLI
        # could not have searched. Comparing against the wrong store is how a
        # correct implementation fails a test.
        api = _load_api()
        report = api.rc.search(
            str(cache), "rubble-pile", "lease",
            context=api.rc.CONTEXT_BULLET,
            threshold=api.rc.DEFAULT_SEARCH_THRESHOLD,
            max_hits=api.rc.DEFAULT_MAX_HITS,
            all_scopes=True,
        )
        assert report.label != f"{report.scope}/", (
            "fixture bug: label and the lookalike are identical here, so this "
            "cannot see the mutant"
        )
        # 🔴 STDERR ONLY. `render_search`'s caveat ALSO prints the label to
        # stdout, so asserting over `stdout + stderr` passes whatever
        # `_exit_for` was handed — measured: the `label -> f"{scope}/"` mutant
        # survived that version of this test. The one-line sentence on stderr is
        # the only output `_exit_for` produces, so it is the only place that
        # discriminates. Third time this file has had to move an assertion off a
        # string that two different code paths can produce.
        assert report.label in proc.stderr, (report.label, proc.stderr)


class TestHeaderCaseInsensitivity:
    """🔴 THE DEFECT THAT SHIPPED. Found by the first live call after deploy,
    not by 358 tests and seven audit rounds.

    `dict(resp.headers)` discards the case-insensitivity of
    `email.message.Message`. HTTP/2 — which Cloudflare speaks — lowercases every
    header name, so in production `headers.get("X-Store-Entries")` returned
    None while the wire carried `x-store-entries: 75`. Measured against the live
    pod. Three things went silently wrong, and the third is the serious one:

        snapshot=UNSTAMPED      the provenance stamp the whole design rests on
        revision=unknown        on every cached banner
        the COUNT CROSS-CHECK NEVER FIRED — the guard against a truncated
        transfer was inert in the only environment that matters

    BOTH test servers (`_CloudflareShim` and the one-shot `_serve_once`) now
    lowercase, so every header-dependent test exercises the production shape. These pin the property directly, so a future refactor back
    to `dict(...)` fails here rather than in six months on a live call.
    """

    def test_the_stamp_records_the_servers_headers_not_placeholders(
        self, live_store, tmp_path: Path
    ):
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        stamp = (cache / ".sync-stamp").read_text()
        # The freshness stamp is the header that was silently lost. It must now
        # carry the server's own value, not the placeholder.
        assert "snapshot=UNSTAMPED" not in stamp, stamp
        assert "snapshot=seeded=" in stamp, stamp
        assert "entries=" in stamp, stamp
        # ⚠ `revision=unknown` is CORRECT, and it is DEAD OUTPUT — pinned here
        # so that is on the record rather than mistaken for information.
        # `/snapshot` sets `X-Store-Revision` on no path, and cairn always
        # fetches unscoped (deliberately — a scope-filtered cache is the
        # silent-zero this client exists to prevent), so this field can never
        # carry a value. My first version of this test asserted the opposite and
        # failed: the assertion was wrong, not the code.
        # CLOSING CONDITION for making it live: `_snapshot` sets
        # `scope_revision(root, scope)` when `?scope=` is present — a revision
        # IS well-defined there — AND some caller uses a scoped fetch. Neither
        # is true today, so the honest move is to say the field is inert rather
        # than leave a reader inferring freshness from "unknown".
        assert "revision=unknown" in stamp, stamp


class TestTheHarnessItself:
    def test_the_shim_is_what_makes_the_server_answer(self, live_store):
        """🔴 Positive control for the fixture, REWRITTEN.

        The previous version's docstring said "confirm a direct call is
        refused"; its body issued a request through the SHIM and asserted 200,
        never touching the upstream — the fixture did not even expose that URL.
        The property held, but the named control was inert, which is the
        "description claims coverage the body does not provide" failure.

        This version drives BOTH legs and asserts they differ: direct → 401,
        through the shim → 200. If the client could reach the server without the
        header-adding hop, every test in this file would be about a topology
        production does not have.
        """
        def _get(base: str) -> int:
            req = urllib.request.Request(base + "/api/v1/recall/widget-cfg")
            req.add_header("Authorization", f"Bearer {GOOD_TOKEN}")
            req.add_header("User-Agent", "subsystem-store-client/1")
            try:
                with urllib.request.urlopen(req, timeout=15) as resp:
                    return resp.status
            except urllib.error.HTTPError as exc:
                return exc.code

        direct = _get(live_store.upstream)
        shimmed = _get(live_store.base)
        assert direct == 401, f"direct call was NOT refused (got {direct})"
        assert shimmed == 200, f"shimmed call did not succeed (got {shimmed})"

    def test_a_wrong_token_is_refused_through_the_shim(
        self, live_store, tmp_path: Path
    ):
        """Negative control: the fixture can still say no, so a 200 above means
        the token was checked rather than the auth layer being absent."""
        proc = run_cairn("sync", url=live_store.base, cache=tmp_path / "cache",
                         token="z" * 48)
        assert proc.returncode != 0
        assert "401" in proc.stderr, proc.stdout


# ---------------------------------------------------------------------------
# 🔴 THE DEPLOY SEAM: `cairn` is on PATH, and HOW it is deployed is load-bearing.
#
# `scripts/cairn` reaches its siblings through `Path(__file__).resolve().parent
# / "lib"`. Deployed as a home-manager STORE COPY, `__file__` resolves into
# /nix/store — where `lib/` is deliberately NOT deployed — and every
# `import subsystem_recall` dies at startup. Deployed as an
# `mkOutOfStoreSymlink`, `.resolve()` follows the link back into the checkout
# and `lib/` is found.
#
# Neither half is enough on its own, which is why this is one guard over a
# RELATIONSHIP rather than two component checks:
#   - pinning only the nix spelling would keep asserting "must be out-of-store"
#     long after someone rewrote cairn to vendor its imports, i.e. it would
#     outlive its own reason and nobody could tell;
#   - pinning only the __file__ lookup would stay green while the deploy line
#     silently became a store copy and the shipped command stopped starting.
# So: assert the reason still exists, THEN assert the deploy mode it forces.
# ---------------------------------------------------------------------------

NIX_HOME = REPO / "nix" / "home.nix"


def test_cairn_still_resolves_its_lib_relative_to_its_own_file():
    """The REASON half of the seam. If this fails, the guard below is pinning a
    constraint that no longer applies — delete both, don't loosen one."""
    src = (REPO / "cairn").read_text()
    assert 'Path(__file__).resolve().parent / "lib"' in src, (
        "scripts/cairn no longer derives its lib/ path from __file__. The "
        "out-of-store requirement below may be obsolete — re-derive it rather "
        "than editing the assertion."
    )
    # …and that the path it builds is actually imported from, not dead code.
    assert "import subsystem_recall" in src, (
        "scripts/cairn no longer imports from its sibling lib/"
    )
    assert (REPO / "lib" / "subsystem_recall.py").exists()


class TestTheDigestFooterPrescribesFlagsTheClientMustHave:
    """🔴 THE DOCUMENTATION AND THE BINARY DISAGREED, AND THE DOCUMENTATION WON THE READER.

    The digest's own footer told readers to run `--ref <name>` and `--limit N`
    to drill into an entry. `cairn recall` accepted neither and exited 2 with
    `unrecognized arguments`, so a reader following the output it had just been
    shown hit a dead end and fell back to invoking the raw module — which is
    the thing the client exists to wrap.

    The second test is the one that closes the CLASS rather than the instance:
    it DERIVES the prescribed flags from the footer the run actually printed,
    so a future footer naming a fifth flag fails here instead of shipping.
    """

    def _synced_cache(self, live_store, tmp_path: Path) -> Path:
        cache = tmp_path / "cache"
        proc = run_cairn("sync", url=live_store.base, cache=cache)
        assert proc.returncode == 0, proc.stderr
        return cache

    def test_recall_ref_surfaces_ONE_entry_instead_of_the_whole_scope(
        self, live_store, tmp_path: Path
    ):
        cache = self._synced_cache(live_store, tmp_path)
        proc = run_cairn(
            "recall", "--scope", "widget-cfg", "--ref", "thing-beta", "--no-sync",
            url=None, cache=cache,
        )
        assert proc.returncode == 0, f"rc={proc.returncode} stderr={proc.stderr}"
        # The asked-for body is present and the SIBLING's body is not: an
        # assertion on presence alone would pass on the full digest, which
        # prints every entry and would make `--ref` look like it worked.
        assert "sidecar drops its lease" in proc.stdout, proc.stdout
        assert "probe lies for 40s" not in proc.stdout, (
            "`--ref` printed the sibling entry too, so it did not narrow anything"
        )

    def test_every_flag_the_footer_PRESCRIBES_is_accepted_by_the_client(
        self, live_store, tmp_path: Path
    ):
        cache = self._synced_cache(live_store, tmp_path)
        digest = run_cairn(
            "recall", "--scope", "widget-cfg", "--no-sync", url=None, cache=cache
        )
        assert digest.returncode == 0, digest.stderr

        prescribed = sorted(set(re.findall(r"`(--[a-z][a-z-]*)", digest.stdout)))
        # 🔴 POSITIVE CONTROL. A regex that matched nothing would make every
        # assertion below vacuous and the test would pass over a footer naming
        # flags the client lacks — the exact defect it is here to catch.
        assert len(prescribed) >= 2, (
            f"extracted {prescribed} from the digest; the footer names at least "
            f"--ref and --limit, so this regex is not reading the footer"
        )

        helptext = run_cairn("recall", "--help", url=None, cache=cache).stdout
        missing = [f for f in prescribed if f not in helptext]
        assert not missing, (
            f"the digest tells readers to run {missing}, and `cairn recall` does "
            f"not accept them. Either wire the flag through to `rc`, or stop the "
            f"footer prescribing it — a client that cannot run its own printed "
            f"advice sends the reader to the raw module."
        )

    def test_the_refusals_are_the_SHARED_ones_not_a_second_copy(
        self, live_store, tmp_path: Path
    ):
        cache = self._synced_cache(live_store, tmp_path)
        proc = run_cairn(
            "recall", "--scope", "widget-cfg", "--list", "--limit", "3", "--no-sync",
            url=None, cache=cache,
        )
        assert proc.returncode != 0, "an incoherent flag pair was accepted"
        # Drive the SHIPPED predicate, never a re-implementation of it: a copy
        # here would drift from the thing it is supposed to be checking, which
        # is the failure this repo already recorded for an exporter's control.
        sys.path.insert(0, str(REPO / "lib"))
        import subsystem_recall as rc  # noqa: PLC0415

        expected = rc.reject_recall_flags(listing=True, limit=3)
        assert expected is not None, "the shared predicate no longer refuses this pair"
        assert expected in proc.stderr, (
            f"the client's refusal is not the shared one.\n"
            f"shared:  {expected}\nclient:  {proc.stderr}"
        )

    def test_an_out_of_range_limit_is_EXPLAINED_not_a_traceback(
        self, live_store, tmp_path: Path
    ):
        """🔴 A DEFECT THIS FEATURE INTRODUCED, CAUGHT BY EXERCISING IT.

        `recall()` validates `limit`/`page` by raising, and `rc.main()` has
        always caught that and answered 2 with the message. Exposing `--limit`
        on the wrapper made the raise reachable from the command line for the
        first time: before the guard, `--limit 0` printed a traceback and
        exited 1. A wrapper that tracebacks where the module it wraps explains
        itself is worse than not offering the flag at all.
        """
        cache = self._synced_cache(live_store, tmp_path)
        proc = run_cairn(
            "recall", "--scope", "widget-cfg", "--limit", "0", "--no-sync",
            url=None, cache=cache,
        )
        assert proc.returncode == 2, f"rc={proc.returncode}: {proc.stderr}"
        assert "Traceback" not in proc.stderr, proc.stderr
        assert "limit must be an int >= 1" in proc.stderr, proc.stderr

    def test_the_featured_pick_RESOLVES_via_the_handoff_instead_of_falling_back(
        self, live_store, tmp_path: Path
    ):
        """🔴 THE WRAPPER READERS ARE TOLD TO RUN WAS WORSE THAN THE RAW MODULE.

        The featured-entry pick is driven by a focus window built from the
        repo's newest handoff doc. This client never built one, so its digest
        could only ever say `most-recent fallback` — and its parenthetical said
        "no handoff doc to read a path window from", which was WRONG about the
        world rather than merely unhelpful: the doc was there, the client never
        looked. Measured on a real store, same repo and same instant, the
        module resolved the doc and the client did not.
        """
        cache = self._synced_cache(live_store, tmp_path)
        # Named for the scope so `scope_for_repo` derives it: passing --scope
        # would SUPPRESS the window, which is the module's own rule, so the
        # fixture has to reach it the way a real caller does.
        repo = tmp_path / "widget-cfg"
        (repo / "claudedocs").mkdir(parents=True)
        (repo / "claudedocs" / "handoff-focus.md").write_text(
            "Work on `apps/thing-beta/config.yaml` and `apps/thing-beta/svc.yaml`.\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "init", "-q", str(repo)], check=True, timeout=60)

        proc = run_cairn(
            "recall", "--repo", str(repo), "--no-sync", url=None, cache=cache
        )
        assert proc.returncode == 0, proc.stderr
        featured = [l for l in proc.stdout.splitlines() if "FEATURED IN FULL" in l]
        assert featured, f"no featured line at all:\n{proc.stdout[:600]}"
        line = featured[0]
        assert "resolved via claudedocs/handoff-focus.md" in line, (
            f"the client did not build a focus window, so the pick fell back:\n{line}"
        )
        assert "most-recent fallback" not in line, line


class TestTheConditionalSync:
    """`sync` offers the validator it stored, and a `304` is a FOURTH thing.

    🔴 THE STATE VOCABULARY IS THE POINT. `scope-empty`, `cached` and
    `store-unreachable, no cache` are kept apart by this client's whole design;
    "reached the pod, nothing changed" is a new situation and it has to read as
    itself rather than borrowing one of theirs. It stays inside the `live` state
    because every consumer of that name is asking "did we reach the store" — the
    DETAIL is where what happened lives, exactly as it already separates
    `--no-sync given` from `SERVED FROM CACHE` inside `cached`.
    """

    SYNC_ETAG = ".sync-etag"
    SYNC_STAMP = ".sync-stamp"

    def test_the_first_sync_stores_a_validator_BESIDE_the_stamp(
        self, live_store, tmp_path: Path
    ):
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        tag = (cache / self.SYNC_ETAG).read_text().strip()
        assert re.fullmatch(r'"sha256:[0-9a-f]{64}"', tag), tag
        # 🔴 AND NOT INSIDE THE STAMP, WHICH IS RENDERED LINE BY LINE AS
        # `  stamp: <line>` IN EVERY REPORT HEADER. That is the whole reason it
        # is its own file: a stamp field would print a 78-character digest on
        # every recall a human or an agent ever reads.
        assert "sha256:" not in (cache / self.SYNC_STAMP).read_text()

    def test_a_second_sync_over_an_UNCHANGED_store_is_NOT_MODIFIED(
        self, live_store, tmp_path: Path
    ):
        cache = tmp_path / "cache"
        first = run_cairn("sync", url=live_store.base, cache=cache)
        assert first.returncode == 0
        assert "fetched from" in first.stdout
        before = (cache / self.SYNC_STAMP).stat().st_mtime_ns

        second = run_cairn("sync", url=live_store.base, cache=cache)
        assert second.returncode == 0, second.stderr
        assert "live — already current at" in second.stdout, second.stdout
        assert "not modified" in second.stdout
        assert "snapshot seeded=" in second.stdout
        # 🔴 IT MUST NOT BORROW ANOTHER STATE'S SENTENCE. `SERVED FROM CACHE`
        # means the pod could NOT be reached; `fetched … just now` means bytes
        # arrived. Neither happened.
        assert "SERVED FROM CACHE" not in second.stdout
        assert "fetched from" not in second.stdout
        assert not second.stdout.startswith(("⚠", "🔴"))
        # Nothing was re-extracted: the cache is the same tree, untouched.
        assert (cache / self.SYNC_STAMP).stat().st_mtime_ns == before

    def test_a_second_sync_after_a_CHANGE_downloads_again(
        self, live_store, source_store: Path, tmp_path: Path
    ):
        """🔴 THE LOAD-BEARING CONTROL, END TO END THROUGH THE REAL CLI: a
        validator that stops matching must produce a real download, which is
        also the only path that exercises `install_snapshot`'s
        retire-and-rename swap over an EXISTING cache.

        The key first proposed for this route — the caller's principal plus the
        authorization epoch — would have failed exactly here: nothing on the
        write path moves the epoch, so this second sync would have been told 304
        and the client would never have seen the new bullet.
        """
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        first_tag = (cache / self.SYNC_ETAG).read_text().strip()

        entry = source_store / "widget-cfg" / "thing-alpha.md"
        entry.write_text(entry.read_text() + "- 2026-01-04: a bullet nobody had.\n")

        second = run_cairn("sync", url=live_store.base, cache=cache)
        assert second.returncode == 0, second.stderr
        assert "fetched from" in second.stdout, second.stdout
        assert "already current" not in second.stdout
        assert (cache / self.SYNC_ETAG).read_text().strip() != first_tag
        assert "a bullet nobody had" in (
            cache / "widget-cfg" / "thing-alpha.md"
        ).read_text()

    def test_a_read_after_a_304_still_renders_the_store(
        self, live_store, tmp_path: Path
    ):
        """🔴 A 304 MUST NOT RENDER AS AN EMPTY OR UNREACHABLE STORE. The cache
        it confirms is the one the reader then reads, so the report is the same
        report — this is the assertion that the confirmation did not quietly
        cost the content.

        ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE, AND THE LABEL IS
        MEASURED: it is GREEN at the pre-change base, where the second recall
        simply fetched again. What it pins is that the new 304 path did not
        change the answer — which is the failure mode a reader would never think
        to look for, because a stale-but-present cache renders perfectly."""
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        first = run_cairn("recall", "--scope", "widget-cfg", url=live_store.base,
                          cache=cache)
        second = run_cairn("recall", "--scope", "widget-cfg", url=live_store.base,
                           cache=cache)
        assert first.returncode == 0 and second.returncode == 0, second.stderr
        assert "thing-alpha" in second.stdout
        assert "scope-empty" not in second.stderr
        assert "store-unreachable" not in second.stderr
        # The BODY is the same report; only the banner's state line differs,
        # because the first recall fetched and the second was told nothing
        # changed.
        assert first.stdout == second.stdout

    def test_fetch_snapshot_FILTERS_the_validator_it_is_HANDED(self, live_store):
        """🔴 THE OUTGOING FILTER, ON ITS OWN, AND IT NEEDED ITS OWN TEST.

        `stored_etag` used to filter too. A mutation sweep measured that copy
        unable to fail — removing it changed nothing observable, because this
        one caught the same value — so it was DELETED rather than left reading
        as coverage it did not provide. This row hands `fetch_snapshot` the bad
        value directly, which is the call the surviving filter exists for:
        `etag` comes from a FILE, which anything can edit.

        Without the filter, `http.client` refuses the request outright
        (`_is_illegal_header_value` matches a newline not followed by
        whitespace) and the `ValueError` escapes every handler here as a
        traceback — an unusable validator taking down a sync that would
        otherwise have worked.
        """
        spec = importlib.util.spec_from_loader(
            "cairn_cli_conditional", loader=None, origin=str(CAIRN_CLI)
        )
        mod = importlib.util.module_from_spec(spec)
        mod.__file__ = str(CAIRN_CLI)
        exec(compile(CAIRN_CLI.read_text(encoding="utf-8"), str(CAIRN_CLI), "exec"),
             mod.__dict__)
        body, headers, not_modified = mod.fetch_snapshot(
            live_store.base, GOOD_TOKEN, scope=None, timeout=5,
            etag='"sha256:aaa"\nX-Injected: yes',
        )
        assert not_modified is False
        assert body, "the archive must still arrive"
        assert headers.get("etag"), "…and the pod must still have offered a validator"

    def test_a_MALFORMED_validator_on_disk_is_not_sent(
        self, live_store, tmp_path: Path
    ):
        """A file this client writes is still a file anything can edit, so the
        stored value is filtered on the way OUT as well as on the way in. A
        value carrying a newline would otherwise split the outgoing header.

        ⚠ GREEN AT THE PRE-CHANGE BASE, WHERE NOTHING READ THE FILE AT ALL — so
        it is not regression coverage either. It is MUTATION-VERIFIED instead:
        dropping `storable_etag` from `fetch_snapshot`'s outgoing path makes it
        fail, which is the claim it is here to make."""
        cache = tmp_path / "cache"
        assert run_cairn("sync", url=live_store.base, cache=cache).returncode == 0
        (cache / self.SYNC_ETAG).write_text('"sha256:aaa"\nX-Injected: yes\n')
        again = run_cairn("sync", url=live_store.base, cache=cache)
        assert again.returncode == 0, again.stderr
        assert "fetched from" in again.stdout, (
            "an unusable validator must mean NO conditional, not a broken one"
        )
