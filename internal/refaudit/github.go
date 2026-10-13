package refaudit

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// EnvGitHubAPI is the REST base the PR check calls; unset is [DefaultGitHubAPI]. `tests/refaudit/e2e.sh`
// points it at a loopback stub.
const EnvGitHubAPI = "CAIRN_AUDIT_GITHUB_API"

// DefaultGitHubAPI is GitHub's public REST API.
const DefaultGitHubAPI = "https://api.github.com"

// GitHubPRs is the real [PRReader]: `GET /repos/<o>/<r>/pulls/<n>`, falling back to `/issues/<n>` on
// a 404 — because a `github:<o>/<r>#N` ref names an issue OR a pull request (`store/refurl.go`), and
// without the fallback every issue ref would read `not-found` (stale, exit 9). ⚠ An issue reuses the PR
// states: open is `open`, closed is `closed-unmerged` — informational either way, never a verdict.
// The token, when set, is sent ONLY to `Base` — never printed, logged or
// sent to the pod.
type GitHubPRs struct {
	Base   string
	Token  string
	Client *http.Client
}

func (g GitHubPRs) get(path string) (int, map[string]any, error) {
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(g.Base, "/")+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("unreadable answer from %s: %w", g.Base, err)
	}
	return resp.StatusCode, doc, nil
}

// PRState implements [PRReader].
func (g GitHubPRs) PRState(owner, repo string, number int) (string, error) {
	base := "/repos/" + owner + "/" + repo
	code, doc, err := g.get(base + "/pulls/" + strconv.Itoa(number))
	if err != nil {
		return "", err
	}
	if code == http.StatusNotFound {
		code, doc, err = g.get(base + "/issues/" + strconv.Itoa(number))
		if err != nil {
			return "", err
		}
		if code == http.StatusNotFound {
			// 🔴 A 404 IS "NOT FOUND" ONLY WHEN THE REPOSITORY ITSELF IS VISIBLE. GitHub answers 404 —
			// not 401/403 — for a PRIVATE repository to a caller without access, so without this every
			// PR ref in a private scope audited with no (or the wrong) token read STALE at exit 9
			// (measured by the S5 audit). An invisible repository is could-not-look.
			repoCode, _, err := g.get(base)
			if err != nil {
				return "", err
			}
			if repoCode != http.StatusOK {
				return "", fmt.Errorf("%s/repos/%s/%s answered HTTP %d: the repository is not visible to this credential", g.Base, owner, repo, repoCode)
			}
			return PRNotFound, nil
		}
	}
	if code != http.StatusOK {
		// 401/403/429 and everything else: rate-limited, no token, or refused — could not look.
		return "", fmt.Errorf("%s answered HTTP %d", g.Base, code)
	}
	if merged, _ := doc["merged_at"].(string); merged != "" {
		return PRMerged, nil
	}
	if state, _ := doc["state"].(string); state == "open" {
		return PROpen, nil
	}
	return PRClosedUnmerged, nil
}
