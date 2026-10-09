package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// collapseRoomLock rewrites gorge's Room lock/unlock answer pair into XMage's
// single boolean ask, the same kind of rewrite xanswersForScenario does for a
// composite election. CR 709.5f/709.5g's Mode$ LockOrUnlock asks the resolving
// player unlock-vs-lock and, when more than one locked door qualifies, which
// door; gorge poses the two as separate choose_n asks (effects/unlockdoor.go
// askUnlockDoorHalf, then the door chooser). XMage's
// LockOrUnlockRoomTargetEffect poses ONE chooseUse boolean whose polarity
// depends on the doors' state; in the both-locked state every scenario here
// reaches (a setup-placed Room has both doors locked) the ask is "the left
// door?" and Left is face 0, so the pair collapses to choice "yes" when gorge
// picked the front face and "no" for the back.
//
// gorge spells every card object by its front face (the driver's
// gorgeSpellingRule), so the door pick's scenario ref IS the front face: a
// picked label equal to the ref's name is face 0. A ref that still spells a
// whole "A // B" name (a Room the scenario named by its whole name, which the
// alias bindings also keep) carries no front-face convention, so its pair is
// left untouched rather than guessed.
//
// Any other observed shape -- the lock half, one candidate, a follow-up that
// is not a two-option card pick -- is left untouched, so nothing outside the
// Mode$ LockOrUnlock both-locked shape changes.
func collapseRoomLock(ds []rules.OracleDecision) {
	for i := range ds {
		d := &ds[i]
		if d.Kind != "choose_n" || d.Resume != "choice" || d.Options != 2 ||
			len(d.Picks) != 1 || d.Picks[0] != "Unlock a door" || pickKind(*d, 0) != "unlock" {
			continue
		}
		for j := i + 1; j < len(ds) && ds[j].Step == d.Step; j++ {
			p := &ds[j]
			if p.Seat != d.Seat || p.Kind != "choose_n" || p.Resume != "choice" ||
				p.Options != 2 || len(p.Picks) != 1 || pickKind(*p, 0) != "card" ||
				len(p.PickRefs) != 1 {
				continue
			}
			ref := p.PickRefs[0]
			if strings.Contains(ref, ":token:") {
				continue
			}
			front := oraclediffRefName(ref)
			if front == "" || strings.Contains(front, " // ") || !strings.HasPrefix(ref, "p") {
				continue
			}
			value := "no"
			if p.Picks[0] == front {
				value = "yes"
			}
			// The unlock ask becomes the single boolean answer; the door ask
			// is absorbed by it and emits nothing (the composite election's
			// skip kind).
			d.Kind = "yesno"
			d.Picks = []string{value}
			d.PickKinds = []string{value}
			p.Kind = "composite_election"
			break
		}
	}
}
