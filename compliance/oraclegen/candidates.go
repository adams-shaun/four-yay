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
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// Slot is one target filter plus whether the cast may omit it (TargetMin$ 0).
type Slot struct {
	Filter   string
	Optional bool
	// Mirror places this slot's candidate on the other seat: the second
	// pick of a TargetsWithDifferentControllers$ slot (Run Away Together)
	// needs a different controller than the first.
	Mirror bool
}

// requiredSlotCount is how many distinct targets one ability demands: its
// TargetMin$ when that is a literal above one, or the paid X when it names
// a Count$xPaid SVar (the generator casts X at xValue). Any other dynamic
// minimum (Count$Kicked, Count$Teamwork) is not priceable here and keeps one
// slot. The repeated slots get distinct objects from fixtures' same-card
// check, so N slots are N distinct targets.
func requiredSlotCount(f *cards.Face, params map[string]string) int {
	raw := strings.TrimSpace(params["TargetMin"])
	if n, err := strconv.Atoi(raw); err == nil {
		if n > 1 {
			return n
		}
		return 1
	}
	if raw == "X" {
		// templates.xAnswers casts a TargetMin$ X spell with X = 1, so it
		// demands one target; more slots would only add decoys.
		return 1
	}
	if body, ok := f.SVars[raw]; ok && strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
		return xValue
	}
	return 1
}

// paramTrue reports whether a boolean ability param is set.
func paramTrue(params map[string]string, key string) bool {
	return strings.EqualFold(strings.TrimSpace(params[key]), "True")
}

// repeatedSlots expands one ability's slot to the number of targets it
// demands (requiredSlotCount, or the combat expansion when combat is set),
// marking the mirror picks of a different-controllers slot.
func repeatedSlots(f *cards.Face, params map[string]string, v string, combat bool) []Slot {
	count := requiredSlotCount(f, params)
	if combat {
		if role, _ := filterCombat(params["ValidTgts"]); role != roleNone {
			count = targetSlotCount(params)
		}
	}
	diff := paramTrue(params, "TargetsWithDifferentControllers")
	out := make([]Slot, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, Slot{Filter: v, Optional: optionalTarget(params), Mirror: diff && i%2 == 1})
	}
	return out
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
		// A combat-state filter is expanded to its target maximum, any
		// other filter only to its required minimum: an "up to N" slot keeps
		// one fixture slot, so its setup gains no decoys the cast never names.
		out = append(out, repeatedSlots(f, params, v, true)...)
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
			out = append(out, repeatedSlots(f, params, v, false)...)
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
	isCharm := false
	for _, ability := range f.Abilities {
		if ability.Kind == "SP" && ability.API == "Charm" {
			isCharm = true
			break
		}
	}
	if isCharm {
		combos := CharmCombinations(f)
		if len(combos) == 0 {
			return false, "charm combination"
		}
		plans = nil
		for _, combo := range combos {
			plans = append(plans, combo.Slots)
		}
	}
	firstReason := ""
	for _, slots := range plans {
		possible := true
		for _, s := range slots {
			if SlotIsStack(s.Filter) || s.Optional {
				continue
			}
			if len(candidatesFor(reg, s.Filter)) == 0 {
				if firstReason == "" {
					firstReason = s.Filter
				}
				possible = false
				break
			}
		}
		if possible {
			// Stack targets are supplied by castWith's precast step, not by
			// ordinary board candidates. Keep them in the feasibility scan
			// above (where they are deliberately skipped), but do not ask the
			// fixture cross-product to materialize them.
			fixtureSlots := make([]Slot, 0, len(slots))
			for _, s := range slots {
				if !SlotIsStack(s.Filter) {
					fixtureSlots = append(fixtureSlots, s)
				}
			}
			if len(fixtures(reg, fixtureSlots)) > 0 {
				return true, ""
			}
		}
	}
	if firstReason == "" {
		firstReason = "charm combination"
	}
	return false, firstReason
}

// targetSlotCount expands a repeated target filter to the configured target
// maximum. The oracle generator fixes X at xValue, matching its cast payment.
func targetSlotCount(params map[string]string) int {
	for _, key := range []string{"TargetMax", "TargetMin"} {
		if n, err := strconv.Atoi(strings.TrimSpace(params[key])); err == nil && n > 1 {
			return n
		}
		if strings.EqualFold(strings.TrimSpace(params[key]), "X") {
			return xValue
		}
	}
	return 1
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
	// combat is the creature p0 must attack with (and the one p1 must
	// block with) so a target filter naming an attacking or blocking
	// creature has a legal target. Both empty means no combat is needed.
	combat combatPlan
	// pre are steps that must run before the card's cast (create a token,
	// attach an Aura, stamp a this-turn zone change).
	pre []Step
}

// combatPlan is the attack/block preamble a fixture needs: attacker is the
// p0 creature to declare attacking; blocker is the p1 creature that blocks
// it (empty = p1 declares no blocks).
type combatPlan struct {
	attackers  []string
	blocks     [][2]string // blocker, attacker
	attackSeat int
	defender   string
}

// combatRole is the combat state a target filter demands of its candidate.
// A filter with BOTH "attacking" and "blocking" is served by the attacker
// branch: an attacking creature is legal for it.
type combatRole int

const (
	roleNone combatRole = iota
	roleAttacker
	roleBlocker
)

// filterPredicate reports whether a target filter names word as a predicate
// (a '.'/'+'-separated component after the base type), case-insensitively.
// A negated component ("!attacking") is a restriction the OTHER way and is
// never a demand for the state, so it is skipped.
func filterPredicate(filter, word string) bool {
	for _, part := range strings.FieldsFunc(filter, func(r rune) bool { return r == '.' || r == '+' || r == ',' }) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "!") {
			continue
		}
		if strings.EqualFold(part, word) {
			return true
		}
	}
	return false
}

// filterCombat classifies a target filter: an attacking demand, else a
// blocking demand, and whether it demands the candidate be tapped. A filter
// that names attacking or blocking already has a combat answer, so a
// redundant tapped branch in the same OR list (Sonar Strike's
// "Creature.attacking,Creature.blocking,Creature.tapped") is not a tap
// demand: the attacker satisfies the whole list.
func filterCombat(filter string) (combatRole, bool) {
	attacking := filterPredicate(filter, "attacking")
	blocking := filterPredicate(filter, "blocking")
	switch {
	case attacking && blocking && strings.Contains(filter, "OppCtrl"):
		// The attacking alternative cannot be an opponent-controlled
		// attacker on p0's turn, but an opponent-controlled blocker can.
		return roleBlocker, false
	case attacking:
		return roleAttacker, false
	case blocking:
		return roleBlocker, false
	default:
		return roleNone, filterPredicate(filter, "tapped")
	}
}

// candidates for one target filter, most generic first. Each puts the
// target on the board and names it. role/tapped record the combat or tap
// state the filter demands so the fixture builder can arrange it.
type cand struct {
	seat, zone, card string // seat "p0"/"p1", zone, card; card "" = the player
	role             combatRole
	tapped           bool
	// ref overrides the target reference (a token has no card name in a
	// zone: it is "pN:token:<subtype>").
	ref string
	// pre are steps inserted before the card's cast.
	pre []Step
}

// It reads the registry for subtype and qualifier filters the fixed type list
// cannot see (Elf/Goblin/... .YouCtrl, Villain/Hero in a graveyard, an
// enchanted creature, a token), and marks a board-history filter
// (ThisTurnEntered) with a move prelude.
func candidatesFor(reg *cards.Registry, filter string) []cand {
	zone := ""
	if i := strings.LastIndexByte(filter, '@'); i >= 0 {
		filter, zone = filter[:i], strings.ToLower(filter[i+1:])
	}
	if zone != "" && zone != "battlefield" {
		if strings.Contains(zone, "battlefield") {
			// A mixed zone (Stack,Battlefield, or Origin$ Battlefield,Stack)
			// is served on the battlefield.
			zone = ""
		} else {
			return zoneCandidates(reg, filter, zone)
		}
	}
	role, tapped := filterCombat(filter)
	alt := strings.Split(filter, ",")
	base := strings.ToLower(strings.SplitN(strings.TrimSpace(alt[0]), ".", 2)[0])
	mine := strings.Contains(filter, "YouCtrl") || strings.Contains(filter, "YouOwn")
	if role == roleNone {
		if strings.Contains(filter, "+token") || strings.Contains(filter, "token+") || base == "token" {
			return tokenCandidates(mine)
		}
		if strings.Contains(filter, "+enchanted") || strings.Contains(filter, "enchanted+") {
			return enchantedCandidates(mine)
		}
		if strings.Contains(filter, "AttachedTo") {
			// An attachment tied to a parent target needs an attach op the
			// runner does not pose; the mandatory case is a real gap, the
			// optional case is dropped by fixtures.
			return nil
		}
		if !isCardTypeBase(base) {
			return subtypeBattlefield(reg, base, mine)
		}
	}
	opp := "p1"
	if mine {
		opp = "p0"
	}
	// A combat-role candidate must be on the attacking player's seat (p0,
	// the caster, whose turn the scenario plays) so it can be declared
	// attacking; a blocking-role candidate is the defending player's (p1)
	// creature that blocks p0's attacker. A role the filter restricts to
	// its own controller (Creature.blocking+YouCtrl) cannot be arranged on
	// p0's own turn and stays unpinned below.
	seat := opp
	if role == roleAttacker {
		// On this scenario's turn only p0 can attack. Do not place an
		// OppCtrl attacker under p0's control to fake the requested filter.
		if strings.Contains(filter, "OppCtrl") {
			return nil
		}
		seat = "p0"
	} else if role == roleBlocker {
		// A YouCtrl blocker cannot block on p0's own turn. Keep that
		// controller-constrained shape unsupported rather than inventing
		// an illegal combat fixture.
		if strings.Contains(filter, "YouCtrl") {
			return nil
		}
		seat = "p1"
	}
	withRole := func(cs []cand) []cand {
		out := make([]cand, 0, len(cs))
		for _, c := range cs {
			c.role, c.tapped = role, tapped
			if c.card != "" && c.zone == "battlefield" {
				c.seat = seat
			}
			out = append(out, c)
		}
		return out
	}
	creatures := []cand{{seat: opp, zone: "battlefield", card: "Grizzly Bears"}, {seat: opp, zone: "battlefield", card: "Serra Angel"}, {seat: opp, zone: "battlefield", card: "Ornithopter"}, {seat: opp, zone: "battlefield", card: "Llanowar Elves"}, {seat: opp, zone: "battlefield", card: "Hill Giant"}}
	switch base {
	case "any":
		if role != roleNone {
			return withRole(creatures)
		}
		return append(creatures, cand{seat: "p1"})
	case "creature":
		return withRole(creatures)
	case "player", "opponent":
		if strings.Contains(filter, "You") && !strings.Contains(filter, "Opp") {
			return []cand{{seat: "p0"}}
		}
		return []cand{{seat: "p1"}, {seat: "p0"}}
	case "permanent", "card":
		if strings.Contains(filter, "Graveyard") {
			break
		}
		return withRole(append(creatures, cand{seat: opp, zone: "battlefield", card: "Glorious Anthem"}, cand{seat: opp, zone: "battlefield", card: "Forest"}))
	case "artifact":
		return withRole([]cand{{seat: opp, zone: "battlefield", card: "Ornithopter"}, {seat: opp, zone: "battlefield", card: "Sol Ring"}})
	case "enchantment":
		return withRole([]cand{{seat: opp, zone: "battlefield", card: "Glorious Anthem"}})
	case "land":
		return withRole([]cand{{seat: opp, zone: "battlefield", card: "Forest"}})
	case "planeswalker":
		return withRole([]cand{{seat: opp, zone: "battlefield", card: "Jace Beleren"}})
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
func enchantedCandidates(mine bool) []cand {
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

// registryCardType returns the first card (in corpus order) whose front face
// carries a card type not served by the legacy zone fixture list.
func registryCardType(reg *cards.Registry, cardType string) (string, bool) {
	if reg == nil {
		return "", false
	}
	for i := range reg.Cards {
		c := reg.Cards[i]
		for fi := range c.Faces {
			for _, typ := range c.Faces[fi].Types {
				if strings.EqualFold(strings.TrimSpace(typ), cardType) {
					return c.Faces[fi].Name, true
				}
			}
		}
	}
	return "", false
}

// registrySubtype returns the first card (in corpus order) whose front face
// carries the subtype.
func registrySubtype(reg *cards.Registry, subtype string) (string, bool) {
	if reg == nil {
		return "", false
	}
	if strings.EqualFold(subtype, "mount") {
		// XMage's driver cannot target any Mount card (Alacrian Jaguar,
		// Bounding Felidar, Trained Arynx, Gila Courser all fail with "Can't
		// find ability to activate command" where Grizzly Bears works), so a
		// Mount fixture only turns One Last Job into a harness row. Leave the
		// slot unserved until the driver question is answered.
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
	alts := strings.Split(filter, ",")
	// A comma alone also separates ordinary type alternatives (e.g.
	// Creature,Planeswalker in one graveyard). Only an explicit per-alt
	// zone marker calls for mixed-zone candidate selection.
	if len(alts) > 1 && (strings.Contains(strings.ToLower(filter), "inzonegraveyard") || strings.Contains(strings.ToLower(filter), "inzoneexile")) {
		var out []cand
		seats := []string{"p0", "p1"}
		if strings.Contains(filter, "YouOwn") || strings.Contains(filter, "YouCtrl") {
			seats = []string{"p0"}
		} else if strings.Contains(filter, "OppOwn") || strings.Contains(filter, "OppCtrl") {
			seats = []string{"p1"}
		}
		for _, alt := range alts {
			altZone := zone
			for _, candidateZone := range []string{"graveyard", "exile", "hand"} {
				if strings.Contains(strings.ToLower(alt), "inzone"+candidateZone) {
					altZone = candidateZone
					break
				}
			}
			base := firstFilterBase(alt)
			if isCardTypeBase(base) && base != "card" && base != "permanent" && base != "any" {
				// Prefer the cards already in the legacy zone fixture list;
				// registry order must not change recorded scenarios for them.
				preferred := map[string]string{
					"creature": "Grizzly Bears", "artifact": "Ornithopter",
					"enchantment": "Glorious Anthem", "land": "Forest",
					"instant": "Shock", "sorcery": "Duress",
				}[base]
				if preferred == "" {
					preferred, _ = registryCardType(reg, base)
				}
				if preferred != "" {
					for _, seat := range seats {
						out = append(out, cand{seat: seat, zone: altZone, card: preferred})
					}
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
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
	// A subtype qualifier (Villain, Hero, ...): pick a real card. A subtype
	// the registry does not serve (Mount, below) keeps the legacy list.
	if base := firstFilterBase(filter); base != "" && !isCardTypeBase(base) {
		if name, ok := registrySubtype(reg, base); ok {
			var out []cand
			for _, st := range seats {
				out = append(out, cand{seat: st, zone: zone, card: name})
			}
			return out
		}
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
				if s.Mirror && c.card != "" && !controllerQualified(s.Filter) {
					c.seat = otherSeat(c.seat)
				}
				if c.card != "" && fixtureAlreadyTargetsCard(fx.targets, c.seat, c.card) {
					continue
				}
				n := fixture{p0: clone(fx.p0), p1: clone(fx.p1), targets: append([]string(nil), fx.targets...), combat: cloneCombat(fx.combat), pre: append([]Step(nil), fx.pre...)}
				if c.card == "" {
					n.targets = append(n.targets, c.seat)
				} else {
					s := &n.p1
					if c.seat == "p0" {
						s = &n.p0
					}
					ref := c.ref
					if ref == "" {
						ref = zoneRef(&n, c)
					}
					place(&n, c)
					n.targets = append(n.targets, ref)
					switch c.role {
					case roleAttacker:
						n.combat.attackers = append(n.combat.attackers, ref)
						n.combat.attackSeat, n.combat.defender = 0, "p1"
					case roleBlocker:
						attackSeat := 0
						if c.seat == "p0" {
							attackSeat = 1
						}
						if len(n.combat.attackers) == 0 {
							n.combat.attackSeat = attackSeat
							if attackSeat == 0 {
								n.combat.defender = "p1"
							} else {
								n.combat.defender = "p0"
							}
						}
						attacker := addAuxAttacker(seatFor(&n.p0, &n.p1, attackSeat), attackSeat)
						n.combat.attackers = appendUnique(n.combat.attackers, attacker)
						n.combat.blocks = append(n.combat.blocks, [2]string{ref, attacker})
					}
					if c.tapped && c.zone == "battlefield" {
						s.Tapped = append(s.Tapped, c.card)
					}
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

// addAuxAttacker puts a spare creature on the battlefield for a
// blocking-role fixture to attack with, and returns its ref.
func addAuxAttacker(p0 *Seat, seat int) string {
	const name = "Grizzly Bears"
	count := 0
	for _, x := range p0.Battlefield {
		if x == name {
			count++
		}
	}
	p0.Battlefield = append(p0.Battlefield, name)
	prefix := fmt.Sprintf("p%d:", seat)
	if count > 0 {
		return fmt.Sprintf("%s%s#%d", prefix, name, count+1)
	}
	return prefix + name
}

func seatFor(p0, p1 *Seat, seat int) *Seat {
	if seat == 1 {
		return p1
	}
	return p0
}

func appendUnique(refs []string, ref string) []string {
	for _, existing := range refs {
		if existing == ref {
			return refs
		}
	}
	return append(refs, ref)
}

func cloneCombat(c combatPlan) combatPlan {
	return combatPlan{attackers: append([]string(nil), c.attackers...), blocks: append([][2]string(nil), c.blocks...), attackSeat: c.attackSeat, defender: c.defender}
}

// fixtureAlreadyTargetsCard prevents two target slots from naming the same
// card object/name. XMage's reference normalizer strips the runner's #N
// duplicate suffix, so repeated same-name slots collapse to one object there.
func fixtureAlreadyTargetsCard(targets []string, seat, card string) bool {
	want := seat + ":" + card
	for _, ref := range targets {
		base := strings.SplitN(ref, "#", 2)[0]
		if base == want {
			return true
		}
	}
	return false
}

func clone(s Seat) Seat {
	return Seat{
		Battlefield: append([]string(nil), s.Battlefield...), Tapped: append([]string(nil), s.Tapped...), Hand: append([]string(nil), s.Hand...),
		Graveyard: append([]string(nil), s.Graveyard...), Exile: append([]string(nil), s.Exile...),
		Library: append([]string(nil), s.Library...), LibraryTop: append([]string(nil), s.LibraryTop...),
	}
}

// controllerQualified reports whether a filter pins its target's controller
// or owner, so a candidate cannot be mirrored onto the other seat.
func controllerQualified(filter string) bool {
	for _, q := range []string{"YouCtrl", "OppCtrl", "YouOwn", "OppOwn", "YouDontCtrl"} {
		if strings.Contains(filter, q) {
			return true
		}
	}
	return false
}

func otherSeat(seat string) string {
	if seat == "p1" {
		return "p0"
	}
	return "p1"
}
