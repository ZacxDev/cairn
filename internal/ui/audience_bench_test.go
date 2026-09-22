package ui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// BenchmarkAudience is what makes `ControlSharing.Audience`'s cost comment a
// RE-DERIVABLE measurement rather than four numbers somebody once saw.
//
// 🔴 IT EXISTS BECAUSE A ROUND WROTE TIMINGS INTO A COMMENT WITH NO HARNESS BEHIND
// THEM. An audit could not check them: `grep -l "func Benchmark"` over every `_test.go`
// returned nothing, so the only honest verdict available was "could not verify". A number
// in prose that nothing can re-derive is the shape this repository files as a defect
// everywhere else; this is the cheap fix rather than the apology.
//
// Run it:
//
//	go test ./internal/ui/ -run '^$' -bench BenchmarkAudience -benchtime 10x
//
// ⚠ ABSOLUTE TIMES ARE HOST- AND LOAD-DEPENDENT AND ARE NOT THE CLAIM. What survives a
// different machine is the RATIO between adjacent sizes, which is what the comment on
// `Audience` reasons from.
func BenchmarkAudience(b *testing.B) {
	for _, size := range []int{10, 100, 300, 600} {
		b.Run(fmt.Sprintf("users=%d_grants=%d", size, size), func(b *testing.B) {
			sharing := benchWorld(b, size)
			scope := control.DerivedID(control.PrefixScope, "bench-scope-0")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := sharing.Audience(scope); err != nil {
					b.Fatalf("Audience: %v", err)
				}
			}
		})
	}
}

// benchWorld builds a world with `size` users and `size` live grants over one project.
//
// ⚠ USERS AND GRANTS MOVE TOGETHER, WHICH IS WHY THE COMMENT ON `Audience` READS ITS
// RATIOS AGAINST `P x G x log G` AND NOT AGAINST ONE PARAMETER. A benchmark that varied
// only one of them would be measuring a different function from the one the page calls.
func benchWorld(b *testing.B, size int) ControlSharing {
	b.Helper()
	at := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	project := control.DerivedID(control.PrefixProject, "bench-project")
	scope := control.DerivedID(control.PrefixScope, "bench-scope-0")

	events := []control.Event{
		{Kind: control.EventProjectCreated, At: at, ProjectID: project, Name: "bench",
			UserID: control.DerivedID(control.PrefixUser, "bench-user-0")},
		{Kind: control.EventScopeCreated, At: at, ScopeID: scope,
			DisplayName: "bench-scope-0", ProjectID: project},
	}
	// The owner has to exist before the project references it.
	events = append([]control.Event{{
		Kind: control.EventUserCreated, At: at,
		UserID:   control.DerivedID(control.PrefixUser, "bench-user-0"),
		Provider: "bench", Subject: "00000000-0000-4000-8000-000000000000",
		Email: "bench-0@notes.example.invalid",
	}}, events...)

	for i := 1; i < size; i++ {
		user := control.DerivedID(control.PrefixUser, fmt.Sprintf("bench-user-%d", i))
		events = append(events, control.Event{
			Kind: control.EventUserCreated, At: at, UserID: user,
			Provider: "bench", Subject: fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
			Email: fmt.Sprintf("bench-%d@notes.example.invalid", i),
		})
	}
	for i := 0; i < size; i++ {
		events = append(events, control.Event{
			Kind: control.EventGranted, At: at,
			GrantID:     control.DerivedID(control.PrefixGrant, fmt.Sprintf("bench-grant-%d", i)),
			SubjectKind: control.KindUser,
			SubjectID:   control.DerivedID(control.PrefixUser, fmt.Sprintf("bench-user-%d", i%size)),
			ObjectKind:  control.ObjectScope, ObjectID: scope,
			Verbs: control.NewVerbSet(control.VerbRead),
		})
	}

	m, err := control.Replay(events)
	if err != nil {
		b.Fatalf("building the bench world: %v", err)
	}
	cache := control.NewCache(benchSource{m}, control.CacheOptions{})
	if err := cache.Refresh(b.Context()); err != nil {
		b.Fatalf("materializing the bench world: %v", err)
	}
	return ControlSharing{Authority: cache, Now: func() time.Time { return at }}
}

type benchSource struct{ m control.Model }

func (s benchSource) Model(context.Context) (control.Model, error) { return s.m, nil }
