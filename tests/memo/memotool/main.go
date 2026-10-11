// Command memotool is the scope-mail end-to-end check's HELPER (`tests/memo/e2e.sh`,
// `tests/memo/live-probe.sh`; S3 of `claudedocs/plan-cairn-scope-mail.md`). It is a TEST FIXTURE,
// never shipped: nothing in `flake.nix` builds it and no binary imports it.
//
//	memotool env-reads                    every environment name the client reads, aliases included
//	memotool sql exec  <stmt> [args…]     run one statement against $MEMOTOOL_DSN; prints rows affected
//	                                      (an arg `@<path>` is that file's bytes)
//	memotool sql query <stmt> [args…]     one row per line, columns tab-separated, NULL as \N
//	memotool block-check <preview|full> <count>   read stdout of memo-check/memo-read on stdin and
//	                                      verify the FENCE: exactly one block, both markers at column 0
//	                                      with equal nonces, every line between them prefixed `| `,
//	                                      `count=<count>`, and NO code point `memo.Unsafe` is true for
//	                                      (decoded as runes). preview: min(count,5) previews and an
//	                                      `and <count-5> more` line; full: <count> bodies or tombstones.
//	memotool raw-stub <addr-file> <rows.json> <token>   serve the client listener's two READ routes
//	                                      over rows given UNRENDERED (decision 19, Guard D), until killed
//	memotool journal-append <journal> <event.json>   append ONE control-journal event through
//	                                      `control.FileStore.Append`, which validates it against the
//	                                      replayed model (a grant, or a grant's revocation — the two
//	                                      records no `cairn-server` command writes)
//
// 🔴 WHY GO AND NOT `psql` OR PYTHON. The `pgtest` CI job pins its server and its Go toolchain and
// nothing else; a `psql` from the runner image would be an inherited client of an unpinned major.
// The block checker must use `memo.Unsafe` ITSELF — a second spelling of the predicate in a script
// would be a copy that drifts. And the env-reads listing must come from `client.EnvReadsWithAliases`,
// never a typed list (the plan names that mutant).
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/memowire"
	"github.com/ZacxDev/cairn/internal/pgstore"
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: memotool env-reads | sql exec|query <stmt> [args…] | block-check <preview|full> <count> | raw-stub <addr-file> <rows.json> <token>")
	}
	switch os.Args[1] {
	case "env-reads":
		for _, name := range client.EnvReadsWithAliases() {
			fmt.Println(name)
		}
	case "sql":
		runSQL(os.Args[2:])
	case "block-check":
		if len(os.Args) != 4 {
			fail("usage: memotool block-check <preview|full> <count>")
		}
		n, err := strconv.Atoi(os.Args[3])
		if err != nil {
			fail("count: " + err.Error())
		}
		data, _ := io.ReadAll(os.Stdin)
		if err := blockCheck(string(data), os.Args[2], n); err != nil {
			fmt.Println("BLOCK-CHECK FAIL: " + err.Error())
			os.Exit(1)
		}
		fmt.Println("BLOCK-CHECK OK")
	case "journal-append":
		if len(os.Args) != 4 {
			fail("usage: memotool journal-append <journal> <event.json>")
		}
		var ev control.Event
		if err := json.Unmarshal([]byte(os.Args[3]), &ev); err != nil {
			fail("event: " + err.Error())
		}
		fs, err := control.OpenFileStore(os.Args[2])
		if err != nil {
			fail(err.Error())
		}
		if _, err := fs.Append(context.Background(), ev); err != nil {
			fail("append: " + err.Error())
		}
		fmt.Println("appended", ev.Kind)
	case "raw-stub":
		if len(os.Args) != 5 {
			fail("usage: memotool raw-stub <addr-file> <rows.json> <token>")
		}
		rawStub(os.Args[2], os.Args[3], os.Args[4])
	default:
		fail("unknown subcommand " + os.Args[1])
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "memotool: "+msg)
	os.Exit(2)
}

func runSQL(args []string) {
	if len(args) < 2 || (args[0] != "exec" && args[0] != "query") {
		fail("usage: memotool sql exec|query <stmt> [args…]")
	}
	dsn := os.Getenv("MEMOTOOL_DSN")
	if dsn == "" {
		fail("MEMOTOOL_DSN is not set")
	}
	db, err := sql.Open(pgstore.DriverName, dsn)
	if err != nil {
		fail(err.Error())
	}
	defer db.Close()
	// An argument `@<path>` is that FILE's bytes: argv cannot carry every code point a hostile
	// memo holds (a NUL, measured), and a file can.
	params := make([]any, 0, len(args)-2)
	for _, a := range args[2:] {
		if path, ok := strings.CutPrefix(a, "@"); ok {
			data, err := os.ReadFile(path)
			if err != nil {
				fail(err.Error())
			}
			a = string(data)
		}
		params = append(params, a)
	}
	if args[0] == "exec" {
		res, err := db.Exec(args[1], params...)
		if err != nil {
			fail("exec: " + err.Error())
		}
		n, _ := res.RowsAffected()
		fmt.Println(n)
		return
	}
	rows, err := db.Query(args[1], params...)
	if err != nil {
		fail("query: " + err.Error())
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			fail("scan: " + err.Error())
		}
		out := make([]string, len(vals))
		for i, v := range vals {
			out[i] = `\N`
			if v.Valid {
				out[i] = v.String
			}
		}
		fmt.Println(strings.Join(out, "\t"))
	}
	if err := rows.Err(); err != nil {
		fail("rows: " + err.Error())
	}
}

// blockCheck is the fence's STRUCTURE, parsed (S0's assertion, over a built client's stdout).
func blockCheck(out, mode string, count int) error {
	if out == "" {
		return fmt.Errorf("stdout is empty")
	}
	for i, r := range out {
		if r != '\n' && memo.Unsafe(r) {
			return fmt.Errorf("an unsafe code point U+%04X at byte %d", r, i)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		return fmt.Errorf("the block does not end in a newline")
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	open, close := lines[0], lines[len(lines)-1]
	const openPrefix, closePrefix = "<<<cairn-memo untrusted nonce=", "<<<end cairn-memo nonce="
	if !strings.HasPrefix(open, openPrefix) || !strings.HasPrefix(close, closePrefix) || !strings.HasSuffix(close, ">>>") {
		return fmt.Errorf("the first and last lines are not the markers: %q … %q", open, close)
	}
	nonce, rest, ok := strings.Cut(strings.TrimPrefix(open, openPrefix), " count=")
	if !ok || strings.TrimSuffix(strings.TrimPrefix(close, closePrefix), ">>>") != nonce || len(nonce) < 8 {
		return fmt.Errorf("the markers' nonces disagree or are malformed: %q … %q", open, close)
	}
	if rest != strconv.Itoa(count)+">>>" {
		return fmt.Errorf("count is %q, want %d", strings.TrimSuffix(rest, ">>>"), count)
	}
	inner := lines[1 : len(lines)-1]
	headers, previews, bodies, tombstones := 0, 0, 0, 0
	var more []string
	for _, l := range inner {
		if !strings.HasPrefix(l, "| ") && l != "|" {
			return fmt.Errorf("a line between the markers lacks the content prefix: %q", l)
		}
		if strings.Contains(l, "<<<") && strings.Contains(l, nonce) {
			return fmt.Errorf("the current nonce appears inside content: %q", l)
		}
		switch {
		case strings.HasPrefix(l, "| [m-"):
			headers++
		case strings.HasPrefix(l, "|   preview: "):
			previews++
		case strings.HasPrefix(l, "|   body: "):
			bodies++
		case strings.HasPrefix(l, "|   retracted by "):
			tombstones++
		case strings.HasPrefix(l, "| and "):
			more = append(more, l)
		}
	}
	switch mode {
	case "preview":
		want := count
		if want > 5 {
			want = 5
		}
		if headers != want || previews+tombstones != want {
			return fmt.Errorf("%d headers and %d previews, want %d of each", headers, previews, want)
		}
		if count > 5 {
			if len(more) != 1 || !strings.HasPrefix(more[0], fmt.Sprintf("| and %d more on ", count-5)) {
				return fmt.Errorf("the K-more line is %q, want one `and %d more on …`", more, count-5)
			}
		} else if len(more) != 0 {
			return fmt.Errorf("an unexpected K-more line %q", more)
		}
	case "full":
		if headers != count || bodies+tombstones != count || len(more) != 0 {
			return fmt.Errorf("%d headers, %d bodies, %d tombstones, %d more-lines; want %d memos in full",
				headers, bodies, tombstones, len(more), count)
		}
	default:
		return fmt.Errorf("mode %q is not preview or full", mode)
	}
	return nil
}

// rawStub serves `GET /client/v1/memos` and `GET /client/v1/memo` over `rows`, a JSON list of
// `memo.Rendered`-shaped objects whose text fields are RAW — exactly what a listener that skipped
// the render-side replacement would send. It authenticates the one token it is given (a 401 for any
// other) and ignores `after`/`limit`, answering every row of the scope.
func rawStub(addrFile, rowsFile, token string) {
	data, err := os.ReadFile(rowsFile)
	if err != nil {
		fail(err.Error())
	}
	var rows []memo.Rendered
	if err := json.Unmarshal(data, &rows); err != nil {
		fail("rows: " + err.Error())
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(addrFile+".tmp", []byte(ln.Addr().String()), 0o600); err != nil {
		fail(err.Error())
	}
	if err := os.Rename(addrFile+".tmp", addrFile); err != nil {
		fail(err.Error())
	}
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	notFound := memowire.ErrorBody{Error: memowire.ErrorNotFound}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			write(w, http.StatusUnauthorized, memowire.ErrorBody{Error: "unauthorized"})
			return
		}
		scope := r.URL.Query().Get("scope")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == memowire.MemosPath:
			out := memowire.ListBody{Memos: []memo.Rendered{}}
			for _, m := range rows {
				if m.Scope == scope {
					out.Memos = append(out.Memos, m)
				}
			}
			if len(out.Memos) == 0 {
				write(w, http.StatusNotFound, notFound)
				return
			}
			write(w, http.StatusOK, out)
		case r.Method == http.MethodGet && r.URL.Path == memowire.MemoPath:
			id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
			for _, m := range rows {
				if m.Scope == scope && m.ID == id {
					write(w, http.StatusOK, memowire.MemoBody{Memo: m})
					return
				}
			}
			write(w, http.StatusNotFound, notFound)
		default:
			http.NotFound(w, r)
		}
	})
	fmt.Fprintln(os.Stderr, "raw stub on", ln.Addr())
	if err := http.Serve(ln, handler); err != nil {
		fail(err.Error())
	}
}
