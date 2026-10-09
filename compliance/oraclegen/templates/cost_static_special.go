package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// This file is the cost-static probes for the special actions a ValidSpell$
// Static.* reducer prices, and for the ChosenType ValidCard rewrite. The
// runner drives the Room unlock through the activate op's option label and
// poses an as-enters type choice through a queued setup answer; the XMage
// driver wires only the unlock (trigger_recipes_room.go's FullyUnlock recipe
// replays there), so a plotting row stays a named skip until the driver
// learns cast_mode plot.

// staticAbilityCostProbes routes a ValidSpell$ Static.* reducer to the probe
// its special action has, or names the gap.
func staticAbilityCostProbes(reg *cards.Registry, reduction int, base costProbe, kind string) ([]costProbe, string) {
	switch strings.ToLower(kind) {
	case "unlock":
		return unlockCostProbes(reg, reduction, base)
	case "plotting":
		return nil, "plotting probe unavailable (cast_mode plot is not XMage-replayable)"
	default:
		return nil, "static-ability cost probe unsupported (" + kind + ")"
	}
}

// unlockCostProbes is the Room unlock probe: the established room unlock
// probe (roomUnlockProbe) is cast and resolved, then its locked right half is
// unlocked at the door's printed cost minus the reduction. The step shape and
// the XMage rule text are the FullyUnlock recipe's (trigger_recipes_room.go),
// which the XMage replay already agrees on.
func unlockCostProbes(reg *cards.Registry, reduction int, base costProbe) ([]costProbe, string) {
	probe, found := reg.Lookup(roomUnlockProbe)
	if !found || !levelb.IsRoomCard(probe) {
		return nil, "room unlock probe absent"
	}
	door := probe.Faces[1]
	pool, why := oraclegen.PoolFor(door.ManaCost)
	if why != "" {
		return nil, "room unlock probe door has no castable cost"
	}
	mana, ok := removeGenericMana(pool, reduction)
	if !ok || mana == "" {
		return nil, "reduction above the unlock cost"
	}
	cast, castable := roomDoorCast(probe, roomUnlockProbe, 0)
	if !castable {
		return nil, "room unlock probe has no castable cost"
	}
	base.hand = appendUnique(base.hand, roomUnlockProbe)
	base.pre = append(base.pre, cast, oraclegen.Step{Op: "resolve"})
	p := base
	p.spell, p.mana = roomUnlockProbe, mana
	p.activate = &costActivation{label: "Unlock " + door.Name, prefix: roomRuleCost(door.ManaCost) + ": Unlock the right half."}
	p.mustReplay = true
	return []costProbe{p}, ""
}

// chosenTypeFixture reads a face's as-enters ChooseType replacement and names
// the type the probe fixes: the entry's options are every creature type, so
// the probe only needs one a simple spell carries. ok is false for a face
// with no creature-type ChooseType entry (the filter stays a named gap).
func chosenTypeFixture(f *cards.Face) (string, bool) {
	for _, kw := range f.Keywords {
		head, tail, ok := strings.Cut(kw, ":")
		if !ok || head != "ETBReplacement" {
			continue
		}
		name := tail[strings.LastIndex(tail, ":")+1:]
		sv := f.SVars[name]
		if !strings.Contains(sv, "DB$ ChooseType") || !strings.Contains(strings.ToLower(sv), "type$ creature") {
			return "", false
		}
		return "Bear", true
	}
	return "", false
}
