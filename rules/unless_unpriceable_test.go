package rules

import (
	"sort"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// walkAllSAs visits every SA reachable from a face: its printed abilities,
// the effect of every trigger, the With of every replacement, and the body
// of every SVar (resolved on demand). This is the same traversal the Audit *cost
// walker in counter_unlesspay_test.go and the Discard walker use, so the
// compiled corpus is swept the way a card is actually compiled -- an
// UnlessCost$ that only reaches an effect through a SubAbility$ or an SVar
// body is still found.
func walkAllSAs(face *cards.Face, visit func(*cards.SA)) {
	var walk func(sa *cards.SA)
	walk = func(sa *cards.SA) {
		for ; sa != nil; sa = sa.Sub {
			visit(sa)
		}
	}
	for _, a := range face.Abilities {
		walk(a)
	}
	for _, t := range face.Triggers {
		walk(t.Effect)
	}
	for _, r := range face.Repls {
		walk(r.With)
	}
	for name := range face.SVars {
		walk(cards.ResolveSVar(face.SVars, name))
	}
}

// unpriceableCounterCards returns the sorted set of card names whose
// compiled corpus carries a Counter SA with a non-empty UnlessCost$ that
// ParseCost cannot price -- the population I-5 protects. The walker dedups
// per (face name, SA line) so a signature reached twice (printed and through
// an SVar) counts once.
func unpriceableCounterCards(reg *cards.Registry) []string {
	set := map[string]struct{}{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			walkAllSAs(f, func(sa *cards.SA) {
				if sa.API != "Counter" {
					return
				}
				uc := sa.Params["UnlessCost"]
				if uc == "" {
					return
				}
				if !ParseCost(uc).Priceable() {
					set[f.Name] = struct{}{}
				}
			})
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TestUnlessCostUnpriceablePopulation pins the corpus population of Counter
// SAs whose UnlessCost$ ParseCost cannot price. It is the executable version
// of the I-5 scope, and it reads the RAW parameter: every one of them must
// satisfy the same !Priceable() predicate (so no SVar-sourced X, cast-time X
// or Sac component escapes as a special case at the strict-parser level), and
// the set itself is a golden -- a corpus or grammar change that adds or
// removes an unpriceable unless-cost here is a real scope change that must be
// understood, not silently absorbed. NOTE: the resolved-cost fold
// (effects.UnlessCostResolved) is a separate layer ABOVE this parser, so a
// card here (Mausoleum Wanderer, Power Sink, Condescend, ...) may still reach
// the unless-pay ask with a concrete generic amount even though its raw
// UnlessCost$ stays in this population. Measured on the compiled
// .cards/ir.gob.gz corpus at FORGE_REF: 29 distinct cards, of which 21 carry
// UnlessCost$ X (the I-5 population the issue names), 3 a Sac<...> part
// (Blood Funnel, Brain Gorgers, Mana Vortex), 3 a Discard<...> part
// (Perplex, Phantasmagorian, Reality Smasher), 1 an ExileFromGrave part
// (Grip of Amnesia -- its UnlessCost$ ExileFromGrave<1/All> flattened to one
// generic mana before the ExileFrom* cost grammar existed, so one floating
// mana "paid" it; now it is a real Exile part, Priceable is false, and the
// unless-pay ask hard-declines, the conservative correct direction), and 1 a
// DamageYou<4> part (Molten Influence -- the head was ParseCost-unmodelled
// one-generic until the cost-token family work; the Sacrifice arm is the
// distinct DamageYou payment path and still offers it).
// Raw .cards/cardsfolder lines with UnlessCost$ X number the same 21.
func TestUnlessCostUnpriceablePopulation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := unpriceableCounterCards(reg)
	var want = []string{
		"Blood Funnel", "Brain Gorgers", "Broken Ambitions", "Cephalid Shrine",
		"Clash of Wills", "Condescend", "Dispelling Exhale", "In the Eye of Chaos",
		"Invoke Prejudice", "Lilting Refrain", "Logic Knot", "Mana Vortex",
		"Martyr of Frost", "Mausoleum Wanderer", "Mindswipe", "Molten Influence", "Overrule",
		"Perplex", "Phantasmagorian", "Power Sink", "Reality Smasher", "Rethink", "Spectral Denial", "Spell Rupture",
		"Swallowed by Leviathan", "Syncopate", "Thassa's Rebuff", "We Say Thee Nay!",
		// Grip of Amnesia: UnlessCost$ ExileFromGrave<1/All> -- a real Exile
		// cost part since the ExileFrom* cost grammar (altcosts), never a
		// flat generic {1}.
		"Grip of Amnesia",
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("unpriceable-unless-cost card population = %d (%v), want %d (%v)",
			len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("population mismatch at %d: got %q want %q\nfull got: %v\nfull want: %v",
				i, got[i], want[i], got, want)
		}
	}
	// The two repo-deck cases the issue names must be in the population.
	for _, must := range []string{"Mausoleum Wanderer", "Power Sink", "Condescend"} {
		if !containsString(got, must) {
			t.Fatalf("%s is not in the unpriceable population %v", must, got)
		}
	}
}

func containsString(a []string, s string) bool {
	i := sort.SearchStrings(a, s)
	return i < len(a) && a[i] == s
}

// xCounterFixture is counterFixture for a counterspell with an {X} in its own
// cost: after choosing the cast, it answers the cast-time KChoose with
// xValue, then targets the creature spell. Everything after that (resolution,
// the unless_pay ask) is the caller's.
func xCounterFixture(t *testing.T, reg *cards.Registry, counter, creature string, xValue string) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := handEngine(t, mustCorpusCard(t, reg, counter), mustCorpusCard(t, reg, creature))
	ids := handIDsByFace(e)
	casterID, creatureID := ids[counter], ids[creature]
	if casterID == 0 || creatureID == 0 {
		t.Fatalf("hand missing %q or %q", counter, creature)
	}
	e.G.Players[0].Pool[state.MU] = 8
	e.G.Players[0].Pool[state.MG] = 8
	e.G.Players[0].Pool[state.MR] = 8
	e.askPriority(0)

	submitChoices(t, e, passToCast(t, e, creatureID))
	submitChoices(t, e, passToCast(t, e, casterID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected a cast-time X choose for %s, got %+v", counter, d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && strconv.Itoa(int(o.Amount)) == xValue {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no X=%s option in %+v", xValue, d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision after casting %s, got %+v", counter, d)
	}
	spellIdx := -1
	for _, o := range d.Options {
		if o.Obj == creatureID {
			spellIdx = o.Index
		}
	}
	if spellIdx < 0 {
		t.Fatalf("creature spell not offered as a participation target: %+v", d.Options)
	}
	submitChoices(t, e, spellIdx)
	return e, casterID, creatureID
}

// TestPowerSinkCastTimeXUnlessPayCannotSucceedFromEmptyPool pins the
// empty-pool half: Power Sink's {X} is a real cast-time choice
// (Count$xPaid), and an EMPTY pool cannot cover whatever the unless-pay arm
// prices. The resolved-cost fold (effects.UnlessCostResolved) now folds that
// Count$xPaid into a concrete "{2}" for X=2, so the ask label shows the
// amount and the charge agrees; with nothing in the pool the payment still
// fails, so Power Sink counters the targeted spell rather than resolving
// inertly. It is the case the old reading of I-5 got wrong.
// Under the unless-pay mana window (cli-20260922T150843Z-daf1bd3e) the offer
// gate also proves the pay branch REACHABLE before it is offered: this payer
// has an empty pool and no window-eligible source, so the ask is decline-only
// and the assertion below pins that shape.
func TestPowerSinkCastTimeXUnlessPayCannotSucceedFromEmptyPool(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, creatureID := xCounterFixture(t, reg, "Power Sink", "Grizzly Bears", "2")

	// Empty the pool so nothing can cover the unless-pay cost.
	e.G.Players[0].Pool = state.Mana{}

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Power Sink")
	}
	// The strict-unpriceable {X} must be a decline-only ask. This assertion
	// fails if the offer gate admits the impossible Pay option, before the
	// resolution fallback can hide that error by declining it later.
	if len(pay.Options) != 1 || pay.Options[0].Kind != "mode" || pay.Options[0].Label != "Don't pay" {
		t.Fatalf("unpriceable {X} exposed a Pay option: %+v", pay.Options)
	}
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(creatureID).Zone; z != state.ZGraveyard {
		t.Fatalf("Power Sink should counter the spell on an unpriceable {X}: zone = %s, want Graveyard", z)
	}
}

// counterSA returns the first Counter SA on a card's first face (or nil).
func counterSA(t *testing.T, c *cards.Card) *cards.SA {
	t.Helper()
	for _, f := range c.Faces {
		for _, ab := range f.Abilities {
			if ab.API == "Counter" {
				return ab
			}
		}
	}
	t.Fatalf("%s has no printed Counter ability", c.Faces[0].Name)
	return nil
}

// inDeck reports whether name appears in the resolved repo deck.
func inDeck(deck []*cards.Card, name string) bool {
	for _, c := range deck {
		if c.Faces[0].Name == name {
			return true
		}
	}
	return false
}

// TestMausoleumWandererUnlessCostX pins the real corpus Counter ability and
// verifies its X unless cost resolves from the captured sacrificed-card LKI.
// The card ships in the mono-blue-tempo and uw-tempo replay decks.
func TestMausoleumWandererUnlessCostX(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	wanderer := mustCorpusCard(t, reg, "Mausoleum Wanderer")
	sa := counterSA(t, wanderer)
	uc := sa.Params["UnlessCost"]
	if uc != "X" {
		t.Fatalf("Mausoleum Wanderer unless cost = %q, want X", uc)
	}
	if ParseCost(uc).Priceable() {
		t.Fatalf("raw Mausoleum Wanderer UnlessCost %q unexpectedly priceable", uc)
	}
	if len(wanderer.Faces) == 0 || wanderer.Faces[0].SVars["X"] != "Sacrificed$CardPower" {
		t.Fatalf("Mausoleum Wanderer X SVar is not Sacrificed$CardPower: %+v", wanderer.Faces[0].SVars)
	}
	e := handEngine(t, wanderer)
	resolved := effects.UnlessCostResolved(e, &effects.Ctx{SVars: wanderer.Faces[0].SVars,
		Sacrificed: []state.SacrificedInfo{{Power: 3}}}, counterSA(t, wanderer))
	if resolved != "{3}" {
		t.Fatalf("Mausoleum Wanderer resolved unless cost = %q, want {3}", resolved)
	}
	if _, ok := ParseUnlessCost(resolved); !ok {
		t.Fatalf("resolved unless cost %q is not payable", resolved)
	}
	for _, deck := range []string{"mono-blue-tempo", "uw-tempo"} {
		if !inDeck(testutil.RepoDeck(t, reg, deck), "Mausoleum Wanderer") {
			t.Fatalf("Mausoleum Wanderer missing from repo deck %s", deck)
		}
	}
}

// TestParseCostPriceable is the unit-level predicate test behind I-5: it pins
// exactly which parsed shapes the payment API can price, so the "unpriceable
// cost" property travels through Cost.Priceable and not through a
// `if cost == "X"` special case.
func TestParseCostPriceable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cost string
		want bool
	}{
		{"", true},                 // free
		{"no cost", true},          // free
		{"1", true},                // one generic
		{"2 U", true},              // generic + colour
		{"R R", true},              // two red
		{"X", false},               // the I-5 shape: unpriceable
		{"X 2", false},             // X alongside a real generic amount is still unpriceable
		{"Y", true},                // Y collapses to {1} -- a substitution, but priceable
		{"Z", true},                // Z likewise, priceable
		{"Sac<1/Creature>", false}, // non-mana part Pay cannot charge
		{"Sac<1/Land>", false},
		{"Discard<1/Card>", false},
		{"PayLife<5>", true}, // fixed life cost; payMana charges the payer's life
		{"T", false},         // Tap is a non-mana part Pay cannot charge
	}
	for _, tc := range cases {
		if got := ParseCost(tc.cost).Priceable(); got != tc.want {
			t.Errorf("Priceable(%q) = %v, want %v", tc.cost, got, tc.want)
		}
	}
}

// strictUnpriceableCards returns the sorted set of card names whose compiled
// corpus carries ANY SA with a non-empty UnlessCost$ that ParseUnlessCost —
// the strict parser rules' unless-pay arm actually charges with — cannot
// price. Where the ParseCost golden above records what the lenient parser
// silently substitutes, this one records what the strict gate declines.
// Chargeable mid-resolution: mana symbols, fixed PayLife<N>, PayEnergy<N>/<X>
// energy parts (the announced X bound at the pay sites), Return<N/Spec>
// choice parts, the LifeTotalHalfUp token, a fixed Mill<N> (Deep Spawn; CR
// 701.13a, payable at any library size) and the Sac/Discard/SubCounter/
// Draw/Reveal components (Sac/Discard/Reveal through the payer-choice
// continuation). Everything else is a hard decline (a decline-only ask is
// still posed and recorded), except the Sacrifice arm's DamageYou<N> payment.
// Measured on the compiled .cards/ir.gob.gz corpus at FORGE_REF: 146 distinct
// cards. API Ward is excluded: the ward keyword expansion stamps the raw ward
// cost onto a DB$ Ward line, and ward costs are priced by rules' ward payment
// handler (beginWardPayment's mana/alt-cost/CollectEvidence/Blight/Waterbend
// arms), never by the shared strict gate — counting them here would label a
// population the unless-pay arm never sees. NOTE the same layering the
// ParseCost golden documents: this reads the RAW parameter, so a card here
// whose SVar/DefinedCost/announced-X body RESOLVES at the fold above the
// parser (Rune Snag's Z, Disruption Aura's DefinedCost_Self, Essence
// Vortex's PayLife<X>, Behemoth of Vault 0's PayEnergy<X>...) can still
// reach the unless-pay ask with a payable amount; the token is unpriceable
// only for a resolution that cannot bind it, which is exactly the fail-closed
// direction.
func strictUnpriceableCards(reg *cards.Registry) []string {
	set := map[string]struct{}{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			walkAllSAs(f, func(sa *cards.SA) {
				if sa.API == "Ward" {
					return
				}
				uc := sa.Params["UnlessCost"]
				if uc == "" {
					return
				}
				if _, ok := ParseUnlessCost(uc); !ok {
					set[f.Name] = struct{}{}
				}
			})
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TestUnlessCostStrictParsePopulation pins the corpus population whose
// UnlessCost$ the strict unless-pay parser declines. It is the executable
// boundary of the payment grammar this task built: mana symbols, fixed
// PayLife<N>, and Sac/Discard/SubCounter/Draw/Reveal/Return/Exile components
// are chargeable mid-resolution (Sac/Discard/Reveal/Return/Exile through the
// payer-choice continuation); everything else is a hard decline (a
// decline-only ask is still posed and recorded), except the Sacrifice arm's
// DamageYou<N> payment.
// A corpus or grammar change that
// adds or removes a name here is a real scope change that must be
// understood, not silently absorbed.
// Task exil1 removed the 15 ExileFromGrave/AnyGrave carriers (Egon, God of
// Death through Web of Inertia): the Exile head is now a priceable
// choice-bearing component, so those cards left this list. Dragon's Approach
// (ExileFromStack<1/Card.Self>) and The War Games (Exile<1/Creature.!token>)
// stay: their spellings are outside the exileCost grammar.
func TestUnlessCostStrictParsePopulation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := strictUnpriceableCards(reg)
	want := []string{"A-Karn, Living Legacy", "Aether Spike", "Alliance of Arms",
		"Anurid Scavenger", "Archfiend of Spite", "Arcum's Whistle", "Armor Wars", "Barbarian Bully",
		"Blazing Salvo", "Book Burning", "Breaking Point", "Brine Seer",
		"Broken Ambitions", "Browbeat", "Cephalid Shrine",
		"Champions of Minas Tirith", "Charismatic Conqueror", "Cheering Crowd",
		"Chisei, Heart of Oceans", "Circular Logic", "Clash of Wills",
		"Collective Voyage", "Combustion Man", "Command Bridge", "Concerted Defense", "Condescend",
		"Countervailing Winds", "Court of Ambition", "Craig Boone, Novac Guard", "Cyclone",
		"Dazzling Denial", "Dispelling Exhale", "Disruption Aura", "Draco",
		"Dragon's Approach", "Dwarven Driller", "Dwarven Scorcher",
		"Elven Passage", "Energy Vortex", "Errant Minion", "Esper Sentinel", "Essence Leak",
		"Essence Vortex", "Evasive Action", "Excise", "Extravagant Spirit",
		"Feather, Radiant Arbiter", "Fettergeist", "Flash",
		"Gurzigost", "Gutsplitter Gang", "Hungry Hungry Heifer", "Ice Cave",
		"In the Eye of Chaos", "Invoke Prejudice", "Ixidor's Will",
		"Karn, Living Legacy", "Killing Wave", "Koskun Falls", "Lava Blister",
		"Liege of the Hollows", "Lilting Refrain", "Lofty Denial", "Logic Knot",
		"Longhorn Firebeast", "Mana-Charged Dragon", "Martyr of Frost", "Mausoleum Wanderer",
		"Megatherium", "Memory Vampire", "Minds Aglow", "Mindswipe", "Molten Influence", "Musician",
		"Oppressive Will", "Overencumbered", "Override", "Overrule", "Pendrell Flux",
		"Phantasmal Sphere", "Pia's Revolution", "Plague of Vermin", "Plunge into Darkness",
		"Power Leak", "Power Sink", "Primordial Ooze", "Protect the Negotiators",
		"Protection Racket", "Public Thoroughfare", "Rakshasa's Disdain", "Rampaging Aetherhood",
		"Rent Is Due", "Repulsive Mutation", "Reservoir Kraken", "Rethink", "Risk Factor",
		"Rites of Refusal", "Rogue Skycaptain", "Rose Room Treasurer", "Rune Snag",
		"Saheeli, Filigree Master", "Sanctuary Wall", "Scent of Brine", "Shared Trauma",
		"Skullscorch", "Soul Strings", "Soul Tithe", "Spectral Denial", "Spell Rupture",
		"Spell Stutter", "Spell Syphon", "Swallowed by Leviathan", "Syncopate", "Tainted Specter",
		"Tariff", "Thassa's Intervention", "Thassa's Rebuff", "The War Games", "Thelon's Chant",
		"Tibalt, Wicked Tormentor", "Tourach's Chant", "Transmute Artifact",
		"Vexing Devil",
		"Volatile Stormdrake", "Wand of Ith", "Waterbending Lesson", "We Say Thee Nay!",
		"Well of Lost Dreams", "Worms of the Earth", "Wrath of the Skies"}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("strict-unpriceable card population = %d, want %d\ngot:  %v\nwant: %v",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("population mismatch at %d: got %q want %q", i, got[i], want[i])
		}
	}
}
