// Candidate selection and fixture assembly for the level-A generator.
//
// A cast's target slots become a cross product of candidate setups
// (fixtures): each candidate names a card the setup places in a zone, and
// may carry prelude steps that must run before the card's cast (create a
// token, attach an Aura, move a card into a graveyard so it is stamped as
// having entered this turn). The selection reads the compiled registry, so a
// subtype or a board-history qualifier is served by a real card rather than
// a fixed hand-written list.
package oraclegen

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// Slot is one target filter plus whether the cast may omit it (TargetMin$ 0).
type Slot struct {
	Filter   string
	Optional bool
}

// SlotSpecs lists the ValidTgts$ filters along the card's spell-ability chain
// (permanent spells have none), in the order the cast asks for them. A charm
// contributes only its first mode's chain, matching the scenario builder.
func SlotSpecs(f *cards.Face) []Slot {
	var out []Slot
	add := func(params map[string]string) {
		v := params["ValidTgts"]
		if v == "" {
			return
		}
		if z := targetZone(params, false); z != "" && !playerTargetHead(v) {
			v += "@" + z
		}
		out = append(out, Slot{Filter: v, Optional: optionalTarget(params)})
	}
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		if sa.API == "Charm" {
			// The runner and XMage both take the first mode; its chain
			// carries the targets.
			if first := strings.TrimSpace(strings.Split(sa.Params["Choices"], ",")[0]); first != "" {
				for name := first; name != ""; {
					params := svarParams(f.SVars[name])
					add(params)
					name = params["SubAbility"]
				}
			}
			break
		}
		for s := sa; s != nil; s = s.Sub {
			add(s.Params)
		}
		break
	}
	return out
}

// ChainSlotSpecs lists the target slots along one SVar ability chain.
func ChainSlotSpecs(f *cards.Face, svar string) []Slot {
	var out []Slot
	for name := svar; name != ""; {
		params := svarParams(f.SVars[name])
		if v := params["ValidTgts"]; v != "" {
			if z := targetZone(params, true); z != "" && !playerTargetHead(v) {
				v += "@" + z
			}
			out = append(out, Slot{Filter: v, Optional: optionalTarget(params)})
		}
		name = params["SubAbility"]
	}
	return out
}

// optionalTarget reports whether an ability's target is optional: the Forge
// TargetMin$ 0 spelling (an absent param means the default, at least one).
func optionalTarget(params map[string]string) bool {
	return strings.TrimSpace(params["TargetMin"]) == "0"
}

// targetSlots is the filter-only projection of SlotSpecs, kept for callers
// that only need the strings.
func targetSlots(f *cards.Face) []string { return filters(SlotSpecs(f)) }

// chainSlots is the filter-only projection of ChainSlotSpecs.
func chainSlots(f *cards.Face, svar string) []string { return filters(ChainSlotSpecs(f, svar)) }

func filters(slots []Slot) []string {
	out := make([]string, 0, len(slots))
	for _, s := range slots {
		out = append(out, s.Filter)
	}
	return out
}

// FaceHasFixture reports whether the static fixture builder can satisfy every
// mandatory target the card's cast demands: each mandatory slot has at least
// one candidate (a stack-only slot is coverable by a precast spell, and an
// optional slot with no candidate is simply omitted). The second return names
// the first unsatisfiable mandatory slot, for the census. This is a static
// scan -- it runs no game -- so the census ratchet can scan the whole corpus.
func FaceHasFixture(reg *cards.Registry, f *cards.Face) (bool, string) {
	plans := [][]Slot{SlotSpecs(f)}
	if modes := charmModes(f); len(modes) > 0 {
		plans = nil
		for _, m := range modes {
			plans = append(plans, ChainSlotSpecs(f, m.svar))
		}
	}
	for _, slots := range plans {
		for _, s := range slots {
			if SlotIsStack(s.Filter) || s.Optional {
				continue
			}
			if len(candidatesFor(reg, s.Filter)) == 0 {
				return false, s.Filter
			}
		}
	}
	return true, ""
}

// svarParams splits an SVar ability body ("DB$ Pump | ValidTgts$ ...")
// into its params.
func svarParams(body string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(body, "|") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "$")
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

type fixture struct {
	p0, p1  Seat
	targets []string
	// pre are steps that must run before the card's cast (create a token,
	// attach an Aura, stamp a this-turn zone change).
	pre []Step
}

// candidates for one target filter, most generic first. Each puts the
// target on the board and names it.
type cand struct {
	seat, zone, card string // seat "p0"/"p1", zone, card; card "" = the player
	// ref overrides the target reference (a token has no card name in a
	// zone: it is "pN:token:<subtype>").
	ref string
	// pre are steps inserted before the card's cast.
	pre []Step
}

// candidatesFor resolves one filter's candidate setups. It reads the registry
// for subtype and qualifier filters the fixed type list cannot see
// (Elf/Goblin/... .YouCtrl, Villain/Hero in a graveyard, an enchanted
// creature, a token), and marks a board-history filter (ThisTurnEntered) with
// a move prelude.
func candidatesFor(reg *cards.Registry, filter string) []cand {
	zone := ""
	if i := strings.LastIndexByte(filter, '@'); i >= 0 {
		filter, zone = filter[:i], strings.ToLower(filter[i+1:])
	}
	if zone != "" && zone != "battlefield" && !strings.Contains(zone, "battlefield") {
		return zoneCandidates(reg, filter, zone)
	}
	alt := strings.Split(filter, ",")
	base := strings.ToLower(strings.SplitN(strings.TrimSpace(alt[0]), ".", 2)[0])
	mine := strings.Contains(filter, "YouCtrl") || strings.Contains(filter, "YouOwn")
	if strings.Contains(filter, "+token") || strings.Contains(filter, "token+") || base == "token" {
		return tokenCandidates(mine)
	}
	if strings.Contains(filter, "+enchanted") || strings.Contains(filter, "enchanted+") {
		return enchantedCandidates(reg, mine)
	}
	if strings.Contains(filter, "AttachedTo") {
		// An attachment tied to a parent target needs an attach op the runner
		// does not pose; the mandatory case is a real gap, the optional case
		// is dropped by fixtures.
		return nil
	}
	if !isCardTypeBase(base) {
		return subtypeBattlefield(reg, base, mine)
	}
	opp := "p1"
	if mine {
		opp = "p0"
	}
	creatures := []cand{{seat: opp, zone: "battlefield", card: "Grizzly Bears"}, {seat: opp, zone: "battlefield", card: "Serra Angel"}, {seat: opp, zone: "battlefield", card: "Ornithopter"}, {seat: opp, zone: "battlefield", card: "Llanowar Elves"}, {seat: opp, zone: "battlefield", card: "Hill Giant"}}
	switch base {
	case "any":
		return append(creatures, cand{seat: "p1", zone: "", card: ""})
	case "creature":
		return creatures
	case "player", "opponent":
		if mine && !strings.Contains(filter, "Opp") {
			return []cand{{seat: "p0", zone: "", card: ""}}
		}
		return []cand{{seat: "p1", zone: "", card: ""}, {seat: "p0", zone: "", card: ""}}
	case "permanent", "card":
		if strings.Contains(filter, "Graveyard") {
			break
		}
		return append(creatures, cand{seat: opp, zone: "battlefield", card: "Glorious Anthem"}, cand{seat: opp, zone: "battlefield", card: "Forest"})
	case "artifact":
		return []cand{{seat: opp, zone: "battlefield", card: "Ornithopter"}, {seat: opp, zone: "battlefield", card: "Sol Ring"}}
	case "enchantment":
		return []cand{{seat: opp, zone: "battlefield", card: "Glorious Anthem"}}
	case "land":
		return []cand{{seat: opp, zone: "battlefield", card: "Forest"}}
	case "planeswalker":
		return []cand{{seat: opp, zone: "battlefield", card: "Jace Beleren"}}
	case "instant", "sorcery":
		return []cand{{seat: opp, zone: "graveyard", card: "Shock"}, {seat: "p0", zone: "graveyard", card: "Shock"}}
	}
	return nil
}

// isCardTypeBase reports whether a filter's first alternative names a card
// type (or the generic grouping words) rather than a subtype.
func isCardTypeBase(base string) bool {
	switch base {
	case "any", "creature", "player", "opponent", "permanent", "card",
		"artifact", "enchantment", "land", "planeswalker", "instant",
		"sorcery", "spell", "battle":
		return true
	}
	return false
}

// subtypeBattlefield finds a permanent of a subtype filter on the
// battlefield. A .YouCtrl / .OppCtrl qualifier chooses the seat; base types
// (Equipment, Saga) and creatures are both served by the registry, so a
// Kindred command's "target Elf you control" gets a real Elf.
func subtypeBattlefield(reg *cards.Registry, subtype string, mine bool) []cand {
	name, ok := registrySubtype(reg, subtype)
	if !ok {
		return nil
	}
	seat := "p1"
	if mine {
		seat = "p0"
	}
	return []cand{{seat: seat, zone: "battlefield", card: name}}
}

// tokenCandidates makes a token you control: a prelude casts a token-maker,
// and the target reference names the created token by subtype.
func tokenCandidates(mine bool) []cand {
	if !mine {
		// An opponent's token is not needed by any current filter; fail
		// closed rather than point at a token that was never made.
		return nil
	}
	return []cand{{
		seat: "p0", zone: "battlefield", card: "Grizzly Bears",
		ref: "p0:token:Goblin",
		pre: []Step{
			{Op: "cast", Seat: 0, Card: "p0:Dragon Fodder", Mana: "CR", Targets: nil},
			{Op: "resolve"},
		},
	}}
}

// enchantedCandidates puts a creature you control on the battlefield and
// casts an Aura on it in a prelude, so "enchanted" is true at the cast.
func enchantedCandidates(reg *cards.Registry, mine bool) []cand {
	seat := "p1"
	if mine {
		seat = "p0"
	}
	return []cand{{
		seat: seat, zone: "battlefield", card: "Grizzly Bears",
		pre: []Step{
			{Op: "cast", Seat: seatIndex(seat), Card: seat + ":Unholy Strength", Mana: "B", Targets: []string{seat + ":Grizzly Bears"}},
			{Op: "resolve"},
		},
	}}
}

func seatIndex(seat string) int {
	if seat == "p0" {
		return 0
	}
	return 1
}

// registrySubtype returns the first card (in corpus order) whose front face
// carries the subtype.
func registrySubtype(reg *cards.Registry, subtype string) (string, bool) {
	if reg == nil {
		return "", false
	}
	for i := range reg.Cards {
		c := reg.Cards[i]
		for fi := range c.Faces {
			if faceHasSubtype(c.Faces[fi], subtype) {
				return c.Faces[fi].Name, true
			}
		}
	}
	return "", false
}

// faceHasSubtype reports whether the face's printed types include the
// subtype (a Kindred Elf's "Elf", a Cat Knight Villain's "Villain").
func faceHasSubtype(f *cards.Face, subtype string) bool {
	for _, t := range f.Types {
		if strings.EqualFold(strings.TrimSpace(t), subtype) {
			return true
		}
	}
	return false
}

// zoneCandidates offers cards in a non-battlefield zone (TgtZone$): the
// owner from YouOwn/OppOwn/YouCtrl/OppCtrl, else both seats. A subtype
// qualifier (Villain, Hero) and a this-turn entry stamp are resolved from the
// registry rather than the fixed type list.
func zoneCandidates(reg *cards.Registry, filter, zone string) []cand {
	if strings.Contains(zone, ",") {
		zone = strings.Split(zone, ",")[0]
	}
	switch zone {
	case "graveyard", "exile", "hand":
	default:
		return nil
	}
	seats := []string{"p0", "p1"}
	switch {
	case strings.Contains(filter, "YouOwn") || strings.Contains(filter, "YouCtrl"):
		seats = []string{"p0"}
	case strings.Contains(filter, "OppOwn") || strings.Contains(filter, "OppCtrl"):
		seats = []string{"p1"}
	}
	// A board-history qualifier: the card must have entered this turn, which
	// only a mid-game move produces. Start it in hand and move it in.
	if strings.Contains(filter, "ThisTurnEntered") {
		var out []cand
		for _, st := range seats {
			for _, c := range []string{"Shock", "Grizzly Bears", "Forest"} {
				out = append(out, cand{seat: st, zone: "hand", card: c,
					pre: []Step{{Op: "move", Seat: seatIndex(st), Card: st + ":" + c, To: zone}}})
			}
		}
		return out
	}
	// A subtype qualifier (Villain, Hero, ...): pick a real card.
	if base := firstFilterBase(filter); base != "" && !isCardTypeBase(base) {
		var out []cand
		for _, st := range seats {
			if name, ok := registrySubtype(reg, base); ok {
				out = append(out, cand{seat: st, zone: zone, card: name})
			}
		}
		return out
	}
	var out []cand
	for _, c := range []string{"Grizzly Bears", "Serra Angel", "Shock", "Llanowar Elves", "Glorious Anthem", "Ornithopter", "Forest", "Duress"} {
		for _, st := range seats {
			out = append(out, cand{seat: st, zone: zone, card: c})
		}
	}
	return out
}

// firstFilterBase is the first comma-separated alternative's head word.
func firstFilterBase(filter string) string {
	first := strings.SplitN(filter, ",", 2)[0]
	return strings.ToLower(strings.SplitN(strings.TrimSpace(first), ".", 2)[0])
}

// fixtures is the cross product of every slot's candidates, capped. An
// optional slot with no candidate is omitted (and emits no target), so it
// cannot sink the whole card; a mandatory slot with no candidate returns nil.
func fixtures(reg *cards.Registry, slots []Slot) []fixture {
	out := []fixture{{}}
	for _, s := range slots {
		cs := candidatesFor(reg, s.Filter)
		if len(cs) == 0 {
			if s.Optional {
				continue
			}
			return nil
		}
		var next []fixture
		for _, fx := range out {
			for _, c := range cs {
				n := fixture{p0: clone(fx.p0), p1: clone(fx.p1), targets: append([]string(nil), fx.targets...), pre: append([]Step(nil), fx.pre...)}
				if c.card == "" {
					n.targets = append(n.targets, c.seat)
				} else {
					ref := c.ref
					if ref == "" {
						ref = zoneRef(&n, c)
					}
					place(&n, c)
					n.targets = append(n.targets, ref)
				}
				n.pre = append(n.pre, c.pre...)
				next = append(next, n)
				if len(next) >= 24 {
					break
				}
			}
		}
		out = next
	}
	return out
}

// zoneRef names the candidate's target: the seat and card, with a #n suffix
// when the same name already sits in one of the seat's visible zones (so the
// target is unambiguous). It is computed before place adds the card.
func zoneRef(fx *fixture, c cand) string {
	s := &fx.p1
	if c.seat == "p0" {
		s = &fx.p0
	}
	count := 0
	for _, x := range append(append(append(append([]string(nil), s.Battlefield...), s.Graveyard...), s.Exile...), s.Hand...) {
		if x == c.card {
			count++
		}
	}
	ref := c.seat + ":" + c.card
	if count > 0 {
		ref = fmt.Sprintf("%s#%d", ref, count+1)
	}
	return ref
}

// place adds the candidate's card to the seat's zone, and puts any Aura or
// token-maker a prelude casts into the hand. A token ref has no card in the
// zone (the prelude creates it), so only its maker is staged.
func place(fx *fixture, c cand) {
	if c.ref == "" {
		s := &fx.p1
		if c.seat == "p0" {
			s = &fx.p0
		}
		switch c.zone {
		case "battlefield":
			s.Battlefield = append(s.Battlefield, c.card)
		case "graveyard":
			s.Graveyard = append(s.Graveyard, c.card)
		case "exile":
			s.Exile = append(s.Exile, c.card)
		case "hand":
			s.Hand = append(s.Hand, c.card)
		}
	}
	for _, st := range c.pre {
		if st.Op == "cast" {
			// The prelude cast belongs in the hand of the seat that casts it.
			hand := &fx.p0.Hand
			if st.Seat == 1 {
				hand = &fx.p1.Hand
			}
			*hand = append(*hand, strings.TrimPrefix(st.Card, fmt.Sprintf("p%d:", st.Seat)))
		}
	}
}

// clone copies a Seat deeply enough that a fixture's edits cannot alias
// another's slices.
func clone(s Seat) Seat {
	return Seat{
		Battlefield: append([]string(nil), s.Battlefield...), Tapped: append([]string(nil), s.Tapped...), Hand: append([]string(nil), s.Hand...),
		Graveyard: append([]string(nil), s.Graveyard...), Exile: append([]string(nil), s.Exile...),
		Library: append([]string(nil), s.Library...), LibraryTop: append([]string(nil), s.LibraryTop...),
	}
}
