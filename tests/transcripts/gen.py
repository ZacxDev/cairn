#!/usr/bin/env python3
"""Generate the SYNTHETIC transcript world the transcript slices are tested against.

    python3 tests/transcripts/gen.py           # (re)write the committed fixture
    python3 tests/transcripts/gen.py --check   # exit 1 if the committed fixture is stale

🔴 EVERY BYTE HERE IS GENERATED, AND NONE OF IT WAS COPIED. The shapes — record types, block
types, field names, value TYPES and the closed-vocabulary enum values — follow the key sets
`claudedocs/plan-cairn-plugins.md` R1-R3 records, which were measured by instruments that
printed STRUCTURE only. Every string VALUE is drawn from the small vocabulary below, a seeded
RNG, or a synthetic name (`alpha-notes`, `host-a`, `clk0000a1`); every timestamp is in the year
2000. No message, prompt, tool output or title from any real session was read to write this.

🔴 AND IT PLANTS NO SECRET. A committed credential-shaped value is itself a `leakscan` finding
(and should be), so the redaction slice plants its secrets at RUN time from a seeded RNG
(`internal/redact`); this world carries only the clean shapes those tests need around them.

The output is ONE JSON file, `internal/transcript/testdata/synthetic_world.json`, so the nix
`onlyGo` filter carries it with one named row. Go tests MATERIALISE the layout from it
(`claude.files` paths are relative to a Claude Code projects root, `ledgers` to a
`read-ledger/` directory). Regenerate and diff — never hand-edit: `--check` is the gate
(`tests/test_transcript_fixtures.py`).
"""
from __future__ import annotations

import argparse
import base64
import json
import random
import struct
import sys
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / "internal" / "transcript" / "testdata" / "synthetic_world.json"
SEED = 20000101

#: The only words content is built from. Deliberately NOT the program's name: F1 (decision 3)
#: fires on any tool input naming it, so a vocabulary word spelling it would make every session
#: look like a cairn reader.
WORDS = (
    "lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt "
    "ut labore et dolore magna aliqua enim minim veniam quis nostrud exercitation ullamco"
).split()

CLAUDE_VERSION = "2.1.289"
OPENCODE_VERSION = "1.18.29"
PROJECT = "-work-alpha"
CWD = "/work/alpha"
BRANCH = "feature/clk0000a1-sample"


class Gen:
    """All randomness and every clock in one place, so the output is a function of SEED."""

    def __init__(self, seed: int) -> None:
        self.rng = random.Random(seed)
        self.tick = 0

    def words(self, n: int) -> str:
        return " ".join(self.rng.choice(WORDS) for _ in range(n))

    def hexs(self, n: int) -> str:
        return "".join(self.rng.choice("0123456789abcdef") for _ in range(n))

    def uuid(self) -> str:
        h = self.hexs(32)
        return f"{h[:8]}-{h[8:12]}-4{h[13:16]}-a{h[17:20]}-{h[20:]}"

    def b62(self, n: int) -> str:
        alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
        return "".join(self.rng.choice(alphabet) for _ in range(n))

    def rand_bytes(self, n: int) -> bytes:
        return bytes(self.rng.randrange(256) for _ in range(n))

    def ts(self) -> str:
        """An ISO timestamp in the year 2000 (leakscan's synthetic year)."""
        self.tick += 1
        minutes, seconds = divmod(self.tick * 7, 60)
        hours, minutes = divmod(minutes, 60)
        return f"2000-01-01T{hours:02d}:{minutes:02d}:{seconds:02d}.{self.tick % 1000:03d}Z"

    def ms(self) -> int:
        """Epoch milliseconds in the year 2000 (946684800000 is 2000-01-01T00:00:00Z)."""
        self.tick += 1
        return 946684800000 + self.tick * 7000

    def tool_id(self) -> str:
        return "toolu_" + self.b62(24)

    def signature(self) -> str:
        """A thinking `signature`: base64 of random bytes — NOT text, NOT a file signature.

        It must survive redaction untouched (decision 6a's reason the base64 rule keys on a
        decoded TEXT match rather than on "decodes to non-text")."""
        return base64.b64encode(self.rand_bytes(240)).decode()


def png(g: Gen, width: int = 2, height: int = 2) -> bytes:
    """A real, minimal PNG: signature, IHDR, IDAT, IEND. Carries NUL bytes by construction."""

    def chunk(kind: bytes, data: bytes) -> bytes:
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

    raw = b"".join(b"\x00" + g.rand_bytes(width * 3) for _ in range(height))
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


def pdf_binary(g: Gen) -> bytes:
    """A PDF whose stream holds bytes that are not UTF-8, so the text rule calls it binary."""
    return b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<< /Length 32 >>\nstream\n" + g.rand_bytes(32) + b"\nendstream\nendobj\n%%EOF\n"


def jpeg(g: Gen) -> bytes:
    return b"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00" + g.rand_bytes(48) + b"\xff\xd9"


def recall_header(scope: str, status: str = "recalled") -> str:
    """The rendered recall header (`internal/report/text.go` `RecallReport.RenderText`)."""
    return (
        f"subsystem-recall: status={status} scope={scope}\n"
        "  store: /srv/store\n"
        "  host: host-a  (the store's own host label)\n"
        "  caveat: synthetic\n"
    )


def search_header(scope: str, query: str) -> str:
    """The rendered search header (`internal/report/searchtext.go`)."""
    return (
        f"subsystem-recall: status=search-hit scope={scope} query='{query}' threshold=0.50 "
        "context=bullet\n  store: /srv/store\n"
    )


def unreadable_header(scope: str) -> str:
    """The third header form (`internal/report/renderer.go` `ExitFor`): it carries NO `scope=`."""
    return (
        f"subsystem-recall: scope-unreadable: all 2 entry files under `{scope}` are MALFORMED — "
        "nothing could be read, so recall was unavailable.\n"
    )


def jsonl(records: list[dict]) -> str:
    return "".join(json.dumps(r, separators=(",", ":"), ensure_ascii=False) + "\n" for r in records)


class ClaudeSession:
    """Builds one Claude Code stream (the main file or one subagent file)."""

    def __init__(self, g: Gen, session: str, agent_id: str | None = None) -> None:
        self.g = g
        self.session = session
        self.agent_id = agent_id
        self.records: list[dict] = []
        self.last_uuid: str | None = None

    def envelope(self, kind: str) -> dict:
        u = self.g.uuid()
        rec = {
            "parentUuid": self.last_uuid,
            "isSidechain": self.agent_id is not None,
            "userType": "external",
            "cwd": CWD,
            "sessionId": self.session,
            "version": CLAUDE_VERSION,
            "gitBranch": BRANCH,
            "entrypoint": "cli",
        }
        if self.agent_id is not None:
            rec["agentId"] = self.agent_id
        rec.update({"type": kind, "uuid": u, "timestamp": self.g.ts()})
        self.last_uuid = u
        return rec

    def add(self, rec: dict) -> dict:
        self.records.append(rec)
        return rec

    # ---- conversation records -------------------------------------------------------------

    def human(self, text: str, as_blocks: bool = False) -> dict:
        rec = self.envelope("user")
        rec["promptId"] = self.g.uuid()
        content = [{"type": "text", "text": text}] if as_blocks else text
        rec["message"] = {"role": "user", "content": content}
        return self.add(rec)

    def meta_user(self, text: str) -> dict:
        rec = self.envelope("user")
        rec["isMeta"] = True
        rec["message"] = {"role": "user", "content": [{"type": "text", "text": text}]}
        return self.add(rec)

    def compact_summary(self, text: str) -> dict:
        rec = self.envelope("user")
        rec["isCompactSummary"] = True
        rec["isVisibleInTranscriptOnly"] = True
        rec["message"] = {"role": "user", "content": text}
        return self.add(rec)

    def assistant(self, blocks: list[dict]) -> dict:
        rec = self.envelope("assistant")
        rec["requestId"] = "req_" + self.g.b62(24)
        rec["message"] = {
            "id": "msg_" + self.g.b62(24),
            "type": "message",
            "role": "assistant",
            "model": "claude-synthetic-1",
            "content": blocks,
            "stop_reason": "tool_use" if any(b["type"] == "tool_use" for b in blocks) else "end_turn",
            "stop_sequence": None,
            "usage": {"input_tokens": self.g.rng.randrange(10, 900), "output_tokens": self.g.rng.randrange(10, 900)},
        }
        return self.add(rec)

    def tool_use(self, name: str, tool_input: dict, think: bool = False, say: bool = False) -> str:
        tid = self.g.tool_id()
        blocks: list[dict] = []
        if think:
            blocks.append({"type": "thinking", "thinking": self.g.words(12), "signature": self.g.signature()})
        if say:
            blocks.append({"type": "text", "text": self.g.words(10)})
        blocks.append({"type": "tool_use", "id": tid, "name": name, "input": tool_input, "caller": {"type": "direct"}})
        self.assistant(blocks)
        return tid

    def tool_result(self, tid: str, content, tool_use_result=None, is_error: bool = False) -> dict:
        rec = self.envelope("user")
        block = {"tool_use_id": tid, "type": "tool_result", "content": content}
        if is_error:
            block["is_error"] = True
        rec["message"] = {"role": "user", "content": [block]}
        if tool_use_result is not None:
            rec["toolUseResult"] = tool_use_result
        rec["sourceToolAssistantUUID"] = self.records[-1]["uuid"] if self.records else None
        return self.add(rec)

    def bash(self, command: str, stdout: str, think: bool = False) -> str:
        tid = self.tool_use("Bash", {"command": command, "description": self.g.words(4)}, think=think)
        self.tool_result(
            tid,
            stdout,
            {"stdout": stdout, "stderr": "", "interrupted": False, "isImage": False, "noOutputExpected": False},
        )
        return tid

    # ---- runtime and bookkeeping records --------------------------------------------------

    def attachment(self, body: dict) -> dict:
        rec = self.envelope("attachment")
        rec["attachment"] = body
        return self.add(rec)

    def system(self, subtype: str, extra: dict) -> dict:
        rec = self.envelope("system")
        rec["subtype"] = subtype
        rec["isMeta"] = False
        rec["level"] = "info"
        rec.update(extra)
        return self.add(rec)

    def bare(self, kind: str, fields: dict) -> dict:
        """A metadata-only record: no envelope, just `type` + `sessionId` + its own keys."""
        rec = {"type": kind}
        rec.update(fields)
        rec["sessionId"] = self.session
        return self.add(rec)


def claude_world(g: Gen) -> tuple[dict, dict, dict]:
    files: dict[str, dict] = {}
    sessions: dict[str, dict] = {}
    ledgers: dict[str, str] = {}

    def put_text(rel: str, text: str) -> None:
        files[rel] = {"utf8": text}

    def put_bytes(rel: str, data: bytes) -> None:
        files[rel] = {"base64": base64.b64encode(data).decode()}

    def ledger(session: str, records: list[tuple[str, str]]) -> None:
        ledgers[session + ".jsonl"] = jsonl(
            [
                {
                    "schema": 1,
                    "session": session,
                    "verb": verb,
                    "instance": "personal",
                    "scope": scope,
                    "client": "cairn-go",
                    "client_version": "synthetic",
                    "at": g.ts(),
                }
                for verb, scope in records
            ]
        )

    # =====================================================================================
    # cc-rich: every record type, block type and blob kind the table declares.
    # =====================================================================================
    sid = g.uuid()
    sessions["cc-rich"] = {"runtime": "claude", "id": sid, "project": PROJECT}
    s = ClaudeSession(g, sid)
    s.bare("permission-mode", {"permissionMode": "default"})
    s.human("please " + g.words(14))
    s.attachment({"type": "hook_success", "hookName": "SessionStart", "hookEvent": "SessionStart",
                  "toolUseID": g.tool_id(), "command": "true", "content": "", "stdout": "", "stderr": "",
                  "exitCode": 0, "durationMs": 4})
    s.attachment({"type": "credential_org", "organizationUuid": g.uuid()})
    s.attachment({"type": "file", "filename": CWD + "/notes.txt", "displayPath": "notes.txt",
                  "content": {"type": "text", "file": {"filePath": CWD + "/notes.txt", "content": g.words(30),
                                                       "numLines": 3, "startLine": 1, "totalLines": 3}}})
    # R7: an explicit `--scope` read, with its rendered header in the result.
    s.bash("cairn recall --scope alpha-notes", recall_header("alpha-notes") + g.words(40) + "\n", think=True)
    # R7: a `cd … && cairn recall` chain (no flag): the header carries the RESOLVED scope.
    s.bash(f"cd {CWD} && cairn recall", recall_header("alpha-notes") + g.words(20) + "\n")
    # R7: a `--repo` read.
    s.bash(f"cairn search --repo {CWD} lorem", search_header("alpha-notes", "lorem") + g.words(20) + "\n")
    # R7: a bare, HEADER-LESS `ls-entries`.
    s.bash("cairn ls-entries", "alpha-notes/lorem-ipsum.md\nalpha-notes/dolor-sit.md\n")
    # An edit: the duplicate field carries the whole original file.
    tid = s.tool_use("Edit", {"file_path": CWD + "/lorem.py", "old_string": g.words(6),
                              "new_string": g.words(6), "replace_all": False}, say=True)
    s.tool_result(tid, "The file has been updated.", {
        "filePath": CWD + "/lorem.py", "oldString": g.words(6), "newString": g.words(6),
        "originalFile": "\n".join(g.words(8) for _ in range(12)) + "\n",
        "structuredPatch": [{"oldStart": 3, "oldLines": 1, "newStart": 3, "newLines": 1,
                             "lines": ["-" + g.words(6), "+" + g.words(6)]}],
        "userModified": False, "replaceAll": False})
    # An inline IMAGE block AND its `toolUseResult.file.base64` duplicate (decision 6a carriers).
    image = base64.b64encode(png(g, 4, 4)).decode()
    tid = s.tool_use("Read", {"file_path": CWD + "/shot.png"})
    s.tool_result(tid, [{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": image}}], {
        "type": "image",
        "file": {"base64": image, "type": "image/png", "originalSize": len(base64.b64decode(image)),
                 "dimensions": {"originalWidth": 4, "originalHeight": 4, "displayWidth": 4, "displayHeight": 4}},
    })
    # A LARGE inline `tool_result` block (bigger than decision 18's 64 KiB display limit).
    big = "\n".join(g.words(12) for _ in range(900)) + "\n"
    s.bash("seq 1 900", big)
    # A tool_result whose content is a LIST of text + tool_reference items, and an error result.
    tid = s.tool_use("ToolSearch", {"query": "select:Read", "max_results": 1})
    s.tool_result(tid, [{"type": "text", "text": g.words(5)}, {"type": "tool_reference", "tool_name": "Read"}])
    tid = s.tool_use("Bash", {"command": "false", "description": g.words(3)})
    s.tool_result(tid, "Exit code 1", {"stdout": "", "stderr": "", "interrupted": False, "isImage": False,
                                       "noOutputExpected": False}, is_error=True)
    # PERSISTED tool results: the inline result is a pointer, the bytes are a FILE (a BLOB).
    blobs = {}
    for kind in ("text", "json", "utf16", "pdf", "jpeg"):
        tid = s.tool_use("Bash", {"command": "cat report-" + kind, "description": g.words(3)})
        name = tid + {"text": ".txt", "json": ".txt", "utf16": ".txt", "pdf": ".pdf", "jpeg": ".jpg"}[kind]
        path = f"{PROJECT}/{sid}/tool-results/{name}"
        blobs[kind] = path
        s.tool_result(tid, f"Output too large. Full output saved to: ~/.claude/projects/{path}", {
            "stdout": "", "stderr": "", "interrupted": False, "isImage": False, "noOutputExpected": False,
            "persistedOutputPath": "~/.claude/projects/" + path, "persistedOutputSize": 1})
    put_text(blobs["text"], "\n".join(g.words(10) for _ in range(40)) + "\n")
    put_text(blobs["json"], json.dumps({"items": [{"id": 12345678901234567890, "name": g.words(2),
                                                   "note": g.words(6) + " é"} for _ in range(4)]},
                                       ensure_ascii=True, indent=2) + "\n")
    put_bytes(blobs["utf16"], ("﻿" + g.words(40) + "\n").encode("utf-16-le"))
    put_bytes(blobs["pdf"], pdf_binary(g))
    put_bytes(blobs["jpeg"], jpeg(g))
    # Two SUBAGENTS: separate files sharing the parent's sessionId (R2), the second spawned BY
    # the first (spawnDepth 2, parentAgentId).
    agent_ids = [g.hexs(16), g.hexs(16)]
    spawn_ids = []
    for i, aid in enumerate(agent_ids):
        tid = s.tool_use("Agent", {"description": g.words(3), "prompt": g.words(20), "subagent_type": "general-purpose"})
        spawn_ids.append(tid)
        s.tool_result(tid, [{"type": "text", "text": g.words(15)}], {
            "status": "completed", "agentId": aid, "prompt": g.words(20), "content": [{"type": "text", "text": g.words(15)}],
            "totalDurationMs": 1200, "totalTokens": 3000, "totalToolUseCount": 2})
    # Compaction: a boundary, then the runtime's own summary as a `user` record (NOT a human turn).
    s.system("compact_boundary", {"content": "Conversation compacted", "logicalParentUuid": s.last_uuid,
                                  "compactMetadata": {"trigger": "auto", "preTokens": 160000, "postTokens": 9000,
                                                      "durationMs": 21000, "cumulativeDroppedTokens": 151000}})
    s.compact_summary("This session is being continued. " + g.words(40))
    s.meta_user("<local-command-stdout>" + g.words(6) + "</local-command-stdout>")
    s.human(g.words(9), as_blocks=True)
    s.assistant([{"type": "text", "text": g.words(30)}])
    s.system("turn_duration", {"durationMs": 64000, "messageCount": 40})
    s.system("away_summary", {"content": g.words(12)})
    s.system("informational", {"content": g.words(6)})
    s.system("local_command", {"content": "<command-name>/lorem</command-name>"})
    s.system("stop_hook_summary", {"hookCount": 1, "hookInfos": [{"command": "true", "durationMs": 3}],
                                   "hookErrors": [], "preventedContinuation": False, "stopReason": "",
                                   "hasOutput": False, "toolUseID": g.tool_id()})
    # Measured as subtypes (R2) but with key sets this plan did not record: envelope + content.
    s.system("agents_killed", {"content": g.words(4)})
    s.system("scheduled_task_fire", {"content": g.words(4)})
    s.bare("queue-operation", {"operation": "enqueue", "timestamp": g.ts(), "content": g.words(8)})
    s.bare("last-prompt", {"lastPrompt": g.words(8), "leafUuid": s.last_uuid})
    s.bare("ai-title", {"aiTitle": g.words(4)})
    s.bare("mode", {"mode": "normal"})
    s.bare("pr-link", {"prNumber": 7, "prUrl": "https://github.com/example-org/alpha-notes/pull/7",
                       "prRepository": "example-org/alpha-notes", "timestamp": g.ts()})
    s.bare("bridge-session", {"bridgeSessionId": g.uuid(), "lastSequenceNum": 41, "noHistoryBackfill": False,
                              "ownerAccountUuid": g.uuid(), "ownerOrganizationUuid": g.uuid()})
    s.bare("artifact-autoreact-ledger", {"v": 1, "accountUuid": g.uuid()})
    s.bare("artifact-comment-monitor", {"v": 1, "artifacts": {}})
    s.bare("atis-latch", {"atis": g.words(1)})
    s.bare("history-suppression", {"cause": g.words(1), "vetoedAgainstAccountUuid": g.uuid(), "ts": g.ts()})
    s.bare("frame-link", {"path": "frames/one", "frameUrl": "https://example.invalid/frames/one", "title": g.words(2),
                          "artifactCount": 1, "timestamp": g.ts()})
    s.bare("agent-name", {"agentName": g.words(1)})
    s.bare("continued-in", {"timestamp": g.ts(), "continuedInSessionId": g.uuid()})
    s.bare("cost-state", {"totalCostUSD": 1.25, "totalAPIDuration": 90000, "totalDuration": 120000,
                          "totalLinesAdded": 12, "totalLinesRemoved": 3, "startTime": 946684800000})
    put_text(f"{PROJECT}/{sid}.jsonl", jsonl(s.records))
    ledger(sid, [("recall", "alpha-notes"), ("recall", "alpha-notes"), ("search", "alpha-notes"),
                 ("ls-entries", "alpha-notes")])
    sessions["cc-rich"]["subagents"] = agent_ids

    for depth, aid in enumerate(agent_ids, start=1):
        sub = ClaudeSession(g, sid, agent_id=aid)
        sub.human(g.words(20))
        sub.attachment({"type": "hook_success", "hookName": "PreToolUse", "hookEvent": "PreToolUse",
                        "toolUseID": g.tool_id(), "command": "true", "content": "", "stdout": "", "stderr": "",
                        "exitCode": 0, "durationMs": 2})
        sub.system("turn_duration", {"durationMs": 9000, "messageCount": 4})
        sub.bash("cairn recall --scope alpha-notes", recall_header("alpha-notes") + g.words(10) + "\n", think=True)
        sub.assistant([{"type": "text", "text": g.words(25)}])
        put_text(f"{PROJECT}/{sid}/subagents/agent-{aid}.jsonl", jsonl(sub.records))
        meta = {"agentType": "general-purpose", "description": g.words(3), "spawnDepth": depth,
                "toolUseId": spawn_ids[depth - 1]}
        if depth == 2:
            meta.update({"parentAgentId": agent_ids[0], "spawnedWithWorktree": True,
                         "worktreeBranch": "worktree-agent-" + aid[:8],
                         "worktreePath": CWD + "/.claude/worktrees/agent-" + aid[:8]})
        put_text(f"{PROJECT}/{sid}/subagents/agent-{aid}.json", json.dumps(meta, indent=2) + "\n")

    # =====================================================================================
    # Small sessions, one R7 read path / fallback each. The expected V for each is pinned as a
    # LITERAL by the tests that read it (never derived from an implementation).
    # =====================================================================================
    def small(name: str, build, ledger_records=None) -> str:
        sid2 = g.uuid()
        sessions[name] = {"runtime": "claude", "id": sid2, "project": PROJECT}
        ss = ClaudeSession(g, sid2)
        ss.human(g.words(10))
        build(ss)
        ss.assistant([{"type": "text", "text": g.words(12)}])
        put_text(f"{PROJECT}/{sid2}.jsonl", jsonl(ss.records))
        if ledger_records is not None:
            ledger(sid2, ledger_records)
        return sid2

    # A recall rendered into a HOOK attachment, with NO command line anywhere → beta-notes.
    small("cc-hook-recall", lambda ss: ss.attachment({
        "type": "hook_additional_context", "hookName": "UserPromptSubmit", "hookEvent": "UserPromptSubmit",
        "toolUseID": g.tool_id(), "content": [recall_header("beta-notes") + g.words(10)]}))
    # A store-wide search rendered `scope=(all scopes)` in a hook attachment → `*`.
    small("cc-hook-allscopes", lambda ss: ss.attachment({
        "type": "hook_additional_context", "hookName": "UserPromptSubmit", "hookEvent": "UserPromptSubmit",
        "toolUseID": g.tool_id(), "content": [search_header("(all scopes)", "lorem") + g.words(10)]}))
    # F1: a program-naming input and an EMPTY ledger → `*`.
    small("cc-f1-empty-ledger", lambda ss: ss.bash("cairn ls-entries", "alpha-notes/lorem-ipsum.md\n"),
          ledger_records=[])
    # F1, case-insensitive.
    small("cc-f1-uppercase", lambda ss: ss.bash("CAIRN recall", "command not found\n"), ledger_records=[])
    # F2: a file-tool read of the client cache root → `*`.
    def f2(ss: ClaudeSession) -> None:
        tid = ss.tool_use("Read", {"file_path": "/home/dev/.cache/subsystem-store/alpha-notes/lorem-ipsum.md"})
        ss.tool_result(tid, g.words(20), {"type": "text", "file": {"filePath": "/home/dev/.cache/subsystem-store/"
                                                                   "alpha-notes/lorem-ipsum.md",
                                                                   "content": g.words(20), "numLines": 1,
                                                                   "startLine": 1, "totalLines": 1}})
    small("cc-f2-cache-read", f2)
    # The `renderer.go` header form that names NO scope → `*`.
    small("cc-unreadable-header", lambda ss: ss.bash("cairn recall --scope alpha-notes",
                                                     unreadable_header("alpha-notes")),
          ledger_records=[("recall", "alpha-notes")])
    # THE routing fixture (clause h): WROTE alpha-notes and READ beta-notes in its FIRST turn.
    def routing(ss: ClaudeSession) -> None:
        ss.bash("cairn append --scope alpha-notes --session x --ref lorem 'ipsum'", "appended\n")
        ss.bash("cairn recall --scope beta-notes", recall_header("beta-notes") + g.words(10) + "\n")
    small("cc-routing-two-instances", routing,
          ledger_records=[("append", "alpha-notes"), ("recall", "beta-notes")])
    # The ledger is the read record: bare calls in a chain, a redirection and a wrapper, and a
    # ledger naming both scopes → both, and no `*`.
    def ledgered(ss: ClaudeSession) -> None:
        ss.bash(f"cd {CWD} && cairn recall > /dev/null 2>&1; echo done", "done\n")
        ss.bash("env FOO=1 cairn recall | head -1", "\n")
    small("cc-ledgered-bare", ledgered, ledger_records=[("recall", "alpha-notes"), ("recall", "beta-notes")])
    # An unrelated session: no program-naming input, no ledger → V empty.
    small("cc-plain", lambda ss: ss.bash("ls -la", "total 0\n"))
    return files, sessions, ledgers


def opencode_world(g: Gen) -> dict:
    """Two recorded `opencode export` documents per session ("before" and "after") and the
    `opencode session list --format json` answer. Between the two exports exactly THREE parts
    change and ONE is added (S2's diff test), and nothing else moves."""

    def oid(prefix: str) -> str:
        return prefix + "_" + g.hexs(12) + g.b62(14)

    root = oid("ses")
    child = oid("ses")
    project = g.hexs(40)
    t0 = g.ms()

    def info(sid: str, parent: str | None) -> dict:
        d = {"id": sid, "slug": "-".join(g.words(2).split()), "projectID": project, "directory": CWD,
             "path": {"root": CWD, "cwd": CWD}, "title": g.words(4), "agent": "build",
             "model": {"id": "synthetic-model-1", "providerID": "synthetic", "variant": "default"},
             "version": OPENCODE_VERSION, "summary": {"additions": 3, "deletions": 1, "files": 1},
             "cost": 0.0, "tokens": {"input": 10, "output": 20, "reasoning": 0, "cache": {"read": 0, "write": 0}},
             "time": {"created": t0, "updated": t0 + 60000},
             "permission": [{"permission": "bash", "pattern": "*", "action": "allow"}]}
        if parent:
            d["parentID"] = parent
        return d

    def user_msg(sid: str, parts: list[dict]) -> dict:
        mid = oid("msg")
        for p in parts:
            p.update({"id": oid("prt"), "sessionID": sid, "messageID": mid})
        return {"info": {"id": mid, "sessionID": sid, "role": "user", "time": {"created": g.ms()}, "agent": "build",
                         "model": {"providerID": "synthetic", "modelID": "synthetic-model-1"}}, "parts": parts}

    def asst_msg(sid: str, parent_id: str, parts: list[dict]) -> dict:
        mid = oid("msg")
        for p in parts:
            p.update({"id": oid("prt"), "sessionID": sid, "messageID": mid})
        created = g.ms()
        return {"info": {"id": mid, "sessionID": sid, "role": "assistant", "parentID": parent_id,
                         "time": {"created": created, "completed": created + 4000}, "modelID": "synthetic-model-1",
                         "providerID": "synthetic", "mode": "build", "agent": "build",
                         "path": {"cwd": CWD, "root": CWD}, "cost": 0.0,
                         "tokens": {"input": 10, "output": 20, "reasoning": 0, "total": 30,
                                    "cache": {"read": 0, "write": 0}}, "finish": "stop"}, "parts": parts}

    def tool(name: str, status: str, tool_input: dict, output: str | None = None, extra: dict | None = None) -> dict:
        start = g.ms()
        state = {"status": status, "input": tool_input, "time": {"start": start}}
        if status in ("completed", "error"):
            state["time"]["end"] = start + 900
        if status == "completed":
            state.update({"output": output or "", "title": g.words(3),
                          "metadata": {"output": output or "", "exit": 0, "description": g.words(3), "truncated": False}})
        if status == "error":
            state["error"] = "Error: " + g.words(4)
        if extra:
            state.update(extra)
        return {"type": "tool", "callID": "call_" + g.b62(24), "tool": name, "state": state}

    def step_start() -> dict:
        return {"type": "step-start", "snapshot": g.hexs(40)}

    def step_finish(tokens_out: int) -> dict:
        return {"type": "step-finish", "reason": "stop", "snapshot": g.hexs(40), "cost": 0.0,
                "tokens": {"input": 10, "output": tokens_out, "reasoning": 0, "total": 10 + tokens_out,
                           "cache": {"read": 0, "write": 0}}}

    img = "data:image/png;base64," + base64.b64encode(png(g, 3, 3)).decode()

    # ---- the ROOT session, "before" -----------------------------------------------------
    u1 = user_msg(root, [{"type": "text", "text": "please " + g.words(10), "time": {"start": g.ms(), "end": g.ms()}},
                         {"type": "file", "mime": "image/png", "filename": "shot.png", "url": img}])
    running = tool("bash", "running", {"command": "seq 1 3", "description": g.words(3)})
    text_part = {"type": "text", "text": g.words(6), "time": {"start": g.ms()}}
    finish = step_finish(20)
    a1_parts = [
        step_start(),
        {"type": "reasoning", "text": g.words(12), "time": {"start": g.ms(), "end": g.ms()}},
        tool("bash", "completed", {"command": "cairn recall --scope alpha-notes", "description": g.words(3)},
             recall_header("alpha-notes") + g.words(15) + "\n"),
        tool("read", "completed", {"filePath": CWD + "/shot.png"}, "Image read successfully",
             extra={"attachments": [{"id": oid("prt"), "sessionID": root, "messageID": "", "type": "file",
                                     "mime": "image/png", "url": img}]}),
        tool("task", "completed", {"description": g.words(3), "prompt": g.words(12), "subagent_type": "general"},
             g.words(10), extra={"metadata": {"sessionId": child, "parentSessionId": root,
                                              "model": {"providerID": "synthetic", "modelID": "synthetic-model-1"},
                                              "truncated": False}}),
        tool("bash", "error", {"command": "false", "description": g.words(3)}),
        running,
        {"type": "patch", "hash": g.hexs(40), "files": [CWD + "/lorem.py"]},
        text_part,
        # Its keys beyond the part envelope were NOT measured (R3 counted ONE such part), so the
        # fixture carries the envelope alone rather than a guess.
        {"type": "compaction"},
        {"type": "text", "text": g.words(5), "synthetic": True, "time": {"start": g.ms(), "end": g.ms()}},
        finish,
    ]
    a1 = asst_msg(root, u1["info"]["id"], a1_parts)
    for att in a1_parts[3]["state"]["attachments"]:
        att["messageID"] = a1["info"]["id"]
    before_root = {"info": info(root, None), "messages": [u1, a1]}

    # ---- the ROOT session, "after": 3 parts CHANGED, 1 part ADDED -------------------------
    after_root = json.loads(json.dumps(before_root))
    parts = after_root["messages"][1]["parts"]
    for p in parts:
        if p["id"] == running["id"]:  # 1: running -> completed
            p["state"].update({"status": "completed", "output": "1\n2\n3\n", "title": g.words(3),
                               "metadata": {"output": "1\n2\n3\n", "exit": 0, "description": g.words(3),
                                            "truncated": False}})
            p["state"]["time"]["end"] = p["state"]["time"]["start"] + 500
        elif p["id"] == text_part["id"]:  # 2: streamed text grows and ends
            p["text"] += " " + g.words(4)
            p["time"]["end"] = g.ms()
        elif p["id"] == finish["id"]:  # 3: the step's token count moves
            p["tokens"]["output"] += 7
            p["tokens"]["total"] += 7
    added = {"type": "text", "text": g.words(7), "time": {"start": g.ms(), "end": g.ms()},
             "id": oid("prt"), "sessionID": root, "messageID": after_root["messages"][1]["info"]["id"]}
    parts.insert(len(parts) - 1, added)  # 4: a new part
    after_root["info"]["time"]["updated"] += 30000

    # ---- the CHILD session (a subagent run as its own session id) ------------------------
    cu = user_msg(child, [{"type": "text", "text": g.words(12), "time": {"start": g.ms(), "end": g.ms()}}])
    ca = asst_msg(child, cu["info"]["id"], [
        step_start(),
        tool("bash", "completed", {"command": "cairn recall --scope alpha-notes", "description": g.words(3)},
             recall_header("alpha-notes") + g.words(8) + "\n"),
        {"type": "text", "text": g.words(9), "time": {"start": g.ms(), "end": g.ms()}},
        step_finish(12),
    ])
    child_doc = {"info": info(child, root), "messages": [cu, ca]}

    listing = [{"id": root, "title": before_root["info"]["title"], "updated": t0 + 60000, "created": t0,
                "projectId": project, "directory": CWD}]
    return {
        "sessions": {"oc-root": {"runtime": "opencode", "id": root}, "oc-child": {"runtime": "opencode", "id": child,
                                                                                    "parent": root}},
        "exports": {"before": {root: before_root, child: child_doc}, "after": {root: after_root, child: child_doc}},
        "session_list": listing,
        "changed_parts": [running["id"], text_part["id"], finish["id"]],
        "added_parts": [added["id"]],
    }


def build() -> dict:
    g = Gen(SEED)
    files, sessions, ledgers = claude_world(g)
    oc = opencode_world(g)
    sessions.update(oc.pop("sessions"))
    return {
        "schema": 1,
        "generator": "tests/transcripts/gen.py",
        "note": "SYNTHETIC. Regenerate with `python3 tests/transcripts/gen.py`; never hand-edit.",
        "sessions": sessions,
        "claude": {"files": files},
        "ledgers": ledgers,
        "opencode": oc,
    }


def render() -> str:
    return json.dumps(build(), indent=1, sort_keys=True, ensure_ascii=True) + "\n"


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--check", action="store_true", help="exit 1 if the committed fixture is stale")
    args = ap.parse_args(argv)
    text = render()
    if args.check:
        if not OUT.exists():
            print(f"STALE: {OUT.relative_to(ROOT)} does not exist; run tests/transcripts/gen.py", file=sys.stderr)
            return 1
        if OUT.read_text(encoding="utf-8") != text:
            print(f"STALE: {OUT.relative_to(ROOT)} differs from a fresh generation; run tests/transcripts/gen.py "
                  "and commit the result (never hand-edit it)", file=sys.stderr)
            return 1
        print(f"fresh: {OUT.relative_to(ROOT)} ({len(text)} bytes)")
        return 0
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(text, encoding="utf-8")
    print(f"wrote {OUT.relative_to(ROOT)} ({len(text)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
