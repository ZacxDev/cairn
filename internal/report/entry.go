package report

import (
	"os"
	"path/filepath"
	"syscall"

	"github.com/ZacxDev/cairn/internal/store"
)

// RecalledEntry is one entry's surfaced sections, plus what was NOT there.
type RecalledEntry struct {
	Ref      string
	Filename string

	// Sensitivity is the EFFECTIVE marker after the fail-safe fold.
	Sensitivity string

	// DeclaredSensitivity is a `sensitivity:` the file wrote that the schema does not
	// know, which the fold overrode. "" when the marker was absent or was honoured.
	// Printed beside the effective value so an override is never silent.
	DeclaredSensitivity string

	// Sections is the surfaced headings the entry HAS. A heading present with nothing
	// under it is a key with an empty value; an ABSENT heading is no key at all, which
	// is the distinction MissingSections is derived from.
	Sections map[string]string

	// BulletCount is top-level `## Nuance / work-history` bullets. The index line's
	// SIZE SIGNAL: it is what a reader wants in order to decide whether an entry is
	// worth a `--ref`, and it is the unit the store's prune-on-resolve discipline is
	// denominated in. A byte count would have been cheaper and would have measured
	// markdown, not history.
	BulletCount int

	// OpenCount is bullets DECLARING `OPEN:` — unfinished business the writer marked.
	//
	// 🔴 A ZERO HERE IS NOT "NOTHING IS OPEN". The marker is opt-in and every bullet
	// written before it existed carries none, so zero means "nothing was declared". The
	// caveat says so; this comment exists so a future caller cannot quietly promote the
	// field to a completeness claim.
	OpenCount int

	// NearMissCount is bullets that TRIED to write an openness marker and missed the
	// grammar — 🔴 THE POPULATION MOST LIKELY TO HOLD A STALE OPEN ACTION, and until it
	// was counted it was byte-identical to "no marker" on the read surface: the badge
	// simply did not render, and a vanishing badge LOOKS like success.
	NearMissCount int

	// UnverifiableCount is `RESOLVED:` bullets naming no sha — closed, but the closure
	// is unprovable. Advisory, not a defect: closing an action is the point, and a
	// sha-less `RESOLVED` is a real closure that simply cannot be checked.
	UnverifiableCount int

	// MTime is the entry file's mtime. Used ONLY for the listing order and the
	// featured-entry fallback, and deliberately NOT rendered — the text must be
	// identical bytes for an unchanged store, and a printed timestamp would make a diff
	// of two runs show movement that is not there.
	//
	// 🔴 IT IS A `float64` OF SECONDS AND NOT A `time.Time`, BECAUSE THE TIE-BREAK
	// DEPENDS ON THE PRECISION. CPython's `st_mtime` is `sec + 1e-9*nsec` computed as a
	// double, so two files whose nanosecond stamps differ can land on ONE float and fall
	// through to the ref tie-break. Comparing `time.Time` would order them by
	// nanosecond instead and produce a different index order with no error and no
	// missing entry — which reads as a stale cache. See pyMtime.
	MTime float64

	// MissingSections is requested COUNTED headings this entry does not carry —
	// REPORTED, never silent.
	//
	// 🔴 IT REACHES THE INDEX ROW, not only a printed body. On the oracle it was
	// computed and rendered ONLY under an entry the digest printed in full — and the
	// digest prints exactly ONE body out of N, so for every other entry the field
	// existed and was discarded. Measured differential control there (two entries
	// differing in NOTHING but the nuance heading): the renamed one reported `0 nuance`
	// and lost its `🔴 1 OPEN` badge entirely, rendering byte-identical to a well-formed
	// entry with an empty work-history.
	MissingSections []string

	// Tasks are the entry's `tasks:` refs as written (`<system>:<id>`), in file order.
	// Carried from the loader's validated refs rather than re-parsed here: a second
	// parse at the read surface is the duplicated predicate that lets a reader show refs
	// the validator rejected.
	Tasks []string
}

// IsBare is true when neither COUNTED section had any content.
//
// 🔴 DELIBERATELY NOT "no section has content". Sections also carries `## What it is`,
// which the writer's own template pre-fills with a placeholder — so reading every value
// would make a freshly created stub report as filled-in and delete the "exists but has
// not been filled in" notice in exactly the case it was written for.
func (e RecalledEntry) IsBare() bool {
	for _, h := range CountedHeadings {
		if e.Sections[h] != "" {
			return false
		}
	}
	return true
}

// ReadEntry reads ONE entry's surfaced sections. READ-ONLY.
//
// The file is located from the loader's own scope + filename, never from a path
// reconstructed out of the ref — `<slug>.<kind>.md` and `<slug>.md` are different files
// and only the loader knows which one this entry came from.
func ReadEntry(storeRoot string, entry store.Entry) (RecalledEntry, error) {
	path := filepath.Join(storeRoot, entry.Scope, entry.Filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return RecalledEntry{}, store.EntryUnreadable(path, err)
	}
	text := store.DecodeReplace(data)
	sections := store.ExtractSections(text, SurfacedHeadings)
	fm := store.ParseFrontMatter(text)
	// ⚠ THE STAT IS SEPARATE FROM THE READ AND ITS FAILURE IS AN ORDINARY 0.0, which is
	// the oracle's own shape: the read above already succeeded, so a stat that then fails
	// is a store mutating underneath the reader, and the answer that keeps the report
	// coming is a zero mtime — an entry sorted last, never an aborted report.
	mtime := 0.0
	if info, statErr := os.Stat(path); statErr == nil {
		mtime = pyMtime(info)
	}
	rawSensitivity, hasSensitivity := fm.String("sensitivity")

	bullets := store.ParseJournalBullets(sections[store.NuanceHeading])
	// 🔴 ONE PASS, ONE PREDICATE. Every count on the index row is read off
	// OpennessPopulation — the single source of the precedence order — rather than off
	// the three fields separately. A delta audit on the oracle caught two surfaces
	// disagreeing about ONE bullet because each decided membership for itself.
	populations := map[string]int{}
	for _, b := range bullets {
		populations[b.OpennessPopulation()]++
	}

	var missing []string
	for _, h := range CountedHeadings {
		if _, present := sections[h]; !present {
			missing = append(missing, h)
		}
	}
	tasks := make([]string, 0, len(entry.Tasks))
	for _, t := range entry.Tasks {
		tasks = append(tasks, t.String())
	}

	return RecalledEntry{
		Ref:                 entry.Ref(),
		Filename:            entry.Filename,
		Sensitivity:         FoldSensitivity(rawSensitivity, hasSensitivity),
		DeclaredSensitivity: DiscardedSensitivity(rawSensitivity, hasSensitivity),
		Sections:            sections,
		BulletCount:         len(bullets),
		OpenCount:           populations[store.PopulationOpen],
		NearMissCount:       populations[store.PopulationNearMiss],
		UnverifiableCount:   populations[store.PopulationUnverifiable],
		MTime:               mtime,
		MissingSections:     missing,
		Tasks:               tasks,
	}, nil
}

// pyMtime is CPython's `os.stat_result.st_mtime`: `sec + 1e-9*nsec`, evaluated as a
// double in that order.
//
// 🔴 THE EXPRESSION IS THE CONTRACT, NOT THE VALUE. `float64(nsec)/1e9` and
// `1e-9*float64(nsec)` are NOT the same double for every nsec — `1e-9` is not exactly
// representable, so the two differ in the last bit on some inputs — and the index order
// is decided by comparing these numbers. Two entries written inside one second are
// exactly the shape the corpus fixture carries on purpose, so a last-bit difference is
// reachable: it flips a comparison that then falls to the ref tie-break, and the
// resulting order reads as a stale cache rather than as an error.
func pyMtime(info os.FileInfo) float64 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// ⚠ NO `Stat_t` MEANS NO NANOSECONDS TO ADD, not a different formula. Reachable
		// only on a platform this server is not built for; the whole-second value is the
		// honest degradation and it keeps the comparison well-defined.
		return float64(info.ModTime().Unix())
	}
	sec, nsec := st.Mtim.Unix()
	return float64(sec) + 1e-9*float64(nsec)
}
