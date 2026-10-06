package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Room trigger causes (CR 709.5). A Room's door unlocks only when it enters
// by being cast, or when its locked door is paid for on the battlefield: a
// Room placed by setup has no unlocked door in either engine, so these causes
// cast the door from hand rather than place it.

// roomUnlockProbe is the Room a FullyUnlock cause casts and then finishes
// unlocking. Both of its doors are targetless statics, so its entering and
// unlocking cannot put a target ask or a second Room trigger in the way.
const roomUnlockProbe = "Dazzling Theater // Prop Room"

// roomDoorCast is the cast step for door face of Room card c. Face 0 is cast
// under the catalogue name (name), the way setup deals it; the other door is
// named by its own face name, which the runner binds to room_alt.
func roomDoorCast(c *cards.Card, name string, face int) (oraclegen.Step, bool) {
	pool, why := oraclegen.PoolFor(c.Faces[face].ManaCost)
	if why != "" {
		return oraclegen.Step{}, false
	}
	ref := name
	if face > 0 {
		ref = c.Faces[face].Name
	}
	return oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + ref, Mana: pool}, true
}

// roomRuleCost is a Forge mana cost ("2 W") in XMage's rule-text braces.
func roomRuleCost(cost string) string {
	var b strings.Builder
	for _, tok := range strings.Fields(cost) {
		b.WriteString("{" + tok + "}")
	}
	return b.String()
}

// roomTriggerCauses returns the causes of the two Room-specific trigger
// sub-families; ok is false for any other sub-family.
func roomTriggerCauses(reg *cards.Registry, name string, req levelb.Requirement) (causes []triggerCause, why string, ok bool) {
	switch req.Sub {
	case levelb.UnlockDoorSub:
		c, found := reg.Lookup(name)
		if !found || !levelb.IsRoomCard(c) {
			return nil, "unlock-door source is not a Room", true
		}
		cast, castable := roomDoorCast(c, name, req.Face)
		if !castable {
			return nil, "unlock-door door has no castable cost", true
		}
		return []triggerCause{{selfInHand: true, hand: []string{name}, steps: []oraclegen.Step{cast}}}, "", true
	case levelb.FullyUnlockSub:
		probe, found := reg.Lookup(roomUnlockProbe)
		if !found || !levelb.IsRoomCard(probe) {
			return nil, "fully-unlock probe not in corpus", true
		}
		cast, castable := roomDoorCast(probe, roomUnlockProbe, 0)
		if !castable {
			return nil, "fully-unlock probe has no castable cost", true
		}
		unlockPool, why := oraclegen.PoolFor(probe.Faces[1].ManaCost)
		if why != "" {
			return nil, "fully-unlock probe door has no castable cost", true
		}
		unlock := oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + roomUnlockProbe,
			Mana: unlockPool, Ability: "Unlock " + probe.Faces[1].Name}
		rule := roomRuleCost(probe.Faces[1].ManaCost) + ": Unlock the right half."
		// resolve clears the card's own enchantment-enters Eerie trigger along
		// with the spell, leaving the stack empty for the unlock (a special
		// action that needs one).
		steps := []oraclegen.Step{cast, {Op: "resolve"}, unlock}
		xab := make([]string, len(steps))
		xab[len(steps)-1] = rule
		return []triggerCause{{hand: []string{roomUnlockProbe}, steps: steps, xability: xab}}, "", true
	}
	return nil, "", false
}

// roomSecondDoorCauses turns the face-0 causes of a trigger on a Room's second
// door into causes that first cast that door: the Room starts in hand, the
// prelude casts the door and resolves it, and the cause's own steps then run
// with the second door unlocked on the battlefield. A cause that already casts
// the card itself is dropped, since the prelude is that cast.
func roomSecondDoorCauses(reg *cards.Registry, name string, req levelb.Requirement, causes []triggerCause) []triggerCause {
	c, found := reg.Lookup(name)
	if !found || !levelb.IsRoomCard(c) || req.Face != 1 {
		return causes
	}
	cast, castable := roomDoorCast(c, name, req.Face)
	if !castable {
		return nil
	}
	var out []triggerCause
	for _, cause := range causes {
		if cause.castSelfX || cause.selfInHand {
			continue
		}
		cause.selfInHand = true
		cause.hand = append([]string{name}, cause.hand...)
		cause.prelude = append([]oraclegen.Step{cast, {Op: "resolve"}}, cause.prelude...)
		if len(cause.preludeXAbility) > 0 {
			cause.preludeXAbility = append([]string{"", ""}, cause.preludeXAbility...)
		}
		out = append(out, cause)
	}
	return out
}
