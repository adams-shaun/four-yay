package rules

import "testing"

// The rules test binary runs every memoized Derived hit in verify mode: each
// hit is recomputed from scratch and a difference panics, so the whole suite
// (repo-deck games, replay goldens, the acceptance ratchet) doubles as the
// empirical check of derivedmemo.go's invalidation argument.
//
// The same binary runs the trigger walk's zone skip in verify mode
// (trigger_zoneskip.go): every skipped object is visited anyway and a visit
// that queues anything panics.
//
// The walk-scoped caches (walkcache.go) run in verify mode too: every hit is
// recomputed and a difference panics.
//
// So do the mana walk's bare-{T} payability fast path (manaCostPayable) and
// the per-build active() digest (active_summary.go) and the configured
// mana-ability gate facts (mana_safacts.go), and the per-face text-scan memo
// (face_scan_memo.go).
func init() {
	derivedMemoVerify = true
	sacrificeCardnameVerify = true
	pricedCandidatesVerify = true
	castsOnlyWalkVerify = true
	potentialMembersVerify = true
	walkCacheVerify = true
	trigZoneSkipVerify = true
	manaPayFastVerify = true
	activeSummaryVerify = true
	manaSAFactsVerify = true
	faceScanVerify = true
}

// allocsWithoutWalkCacheVerify runs testing.AllocsPerRun with the walk
// caches' verify mode off: a verify hit recomputes (and so allocates) the
// cached list, which an allocation budget would otherwise charge to the
// production path it is checking. Only for sequential (non-Parallel) tests,
// which never overlap a Parallel one.
func allocsWithoutWalkCacheVerify(runs int, f func()) float64 {
	prev := walkCacheVerify
	walkCacheVerify = false
	defer func() { walkCacheVerify = prev }()
	return testing.AllocsPerRun(runs, f)
}
