package capture

import "github.com/ZacxDev/cairn/internal/client"

// The two seams `repo_route_test.go` reaches the repo arm through. They exist so that file
// compiles UNCHANGED against the code before the arm existed — with a stand-in for this file that
// ignores the repo and the owner rules — and its tests can be watched FAIL there on their own
// assertions rather than on a compile error. Nothing else should call them.

func decide(v []string, repo Repo, routing client.Routing, owners OwnerRoutes) Decision {
	return Decide(v, repo, routing, owners)
}

func setOwnerRoutes(a *Agent, owners OwnerRoutes) { a.OwnerRoutes = owners }
