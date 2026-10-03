package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TargetUniqueRequested reports whether an SA carries Forge's
// `TargetUnique$ True` -- the "every target this resolution chooses must be
// different" constraint. It is the one classifier the mid-resolution pre-ask
// (poseTargetsAsk) and the placement asks (rules' askTarget / cast.go's
// targetAsk) share, so an SA can never be unique-constrained at one site and
// not the other.
//
// The corpus reads the parameter two ways, both served by one exclusion rule:
//
//   - On a root/SubAbility pair (Biomantic Mastery's "another target player",
//     Cybernetica Datasmith's, Venom Blast's / Betrayal at the Vault's "other
//     target creature"), the sub's targets must not repeat the parent's --
//     Ctx.Targets is the set to exclude.
//   - On a standalone multi-target SA (Visions of Duplicity's "two target
//     creatures"), the picks must be pairwise distinct. That half is already
//     structural: one option per candidate plus Decision.Validate's
//     duplicate-choice rejection means the same target cannot be picked
//     twice, so no extra grouping is owed.
//
// The charm family's cross-mode "each mode must target a different player"
// (Shadrix Silverquill, the duo cycle, Balor) is a third instance of the same
// rule, but it is served by its own combined ask (rules/stack.go's
// askCrossModeCharmTargets and effects' charmCrossModeRun), whose per-mode
// re-entry sets Ctx.OfferedSA so this pre-ask is skipped -- the two cannot
// double-enforce.
func TargetUniqueRequested(sa *cards.SA) bool {
	return TargetsOf(sa).Has(TgtUnique)
}

// TargetsAlreadyChosen is the exclusion set a TargetUnique$ ask must not
// re-offer: the resolution's placement/announcement targets (Ctx.Targets) plus
// the targets earlier TargetUnique$ asks in the same chain chose
// (Ctx.TargetsUnique). It returns nil for a context with neither, so a call
// site can pass it unconditionally without allocating on the common path.
func TargetsAlreadyChosen(c *Ctx) []state.Target {
	if c == nil || (len(c.Targets) == 0 && len(c.TargetsUnique) == 0) {
		return nil
	}
	out := make([]state.Target, 0, len(c.Targets)+len(c.TargetsUnique))
	out = append(out, c.Targets...)
	out = append(out, c.TargetsUnique...)
	return out
}

// TargetUniqueFilter drops from candidates every target already chosen in the
// current resolution chain when the SA carries TargetUnique$ True. It is the
// single home of the exclusion so every mid-resolution ask site gets the same
// answer; the placement asks instead rely on Decision.Validate's
// duplicate-choice rejection (see TargetUniqueRequested). The returned slice
// is a fresh allocation (never an in-place filter of candidates) because
// callers hold candidates from Host.LegalTargets and read it again for the
// no-host stand-in's slice bound.
func TargetUniqueFilter(sa *cards.SA, candidates []state.Target, chosen []state.Target) []state.Target {
	if !TargetUniqueRequested(sa) || len(chosen) == 0 || len(candidates) == 0 {
		return candidates
	}
	out := make([]state.Target, 0, len(candidates))
	for _, cand := range candidates {
		dup := false
		for _, have := range chosen {
			if sameTarget(cand, have) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, cand)
		}
	}
	return out
}

// sameTarget reports whether two target designations name the same player or
// the same object. A player and an object are never the same target even at
// the same numeric id (state.Target.IsPlayer discriminates them), which is the
// same distinction Decision.Option's Kind carries on the wire.
func sameTarget(a, b state.Target) bool {
	if a.IsPlayer != b.IsPlayer {
		return false
	}
	if a.IsPlayer {
		return a.Player == b.Player
	}
	return a.Obj == b.Obj
}
