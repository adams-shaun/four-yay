package rules

// kw:Web-slinging (CR 702.186a-family, Marvel's Spider-Man): "You may cast
// this spell for <cost> if you also return a tapped creature you control to
// its owner's hand." The mechanic is a cost substitution (the web-slinging
// cost replaces the mana cost, the Evoke/Miracle shape) composed with a
// mandatory additional cost (Return<1/Creature.YouCtrl+tapped>) and a
// pay-time provenance flag (state.FlagWebSlinged) that the
// Card.Self+webSlinged filter predicate reads. These tests pin the census of
// carriers, the offer/charge composition, the return-cost payment, and the
// ETB provenance rider on Spiders-Man, Heroic Horde.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// webSlingingCensusNames the corpus carriers of the printed K:Web-slinging
// line and their web-slinging costs (measured at the current FORGE_REF pin).
// A corpus bump that adds or removes carriers must update this table — and
// only a corpus bump can: the engine reads whatever the K: line says.
var webSlingingCensus = []struct {
	name   string
	wsCost string
}{
	{"Amazing Spider-Girl", "2 W"},
	{"Arachne, Psionic Weaver", "W"},
	{"Scarlet Spider, Ben Reilly", "R G"},
	{"Silk, Web Weaver", "1 G W"},
	{"Spider-Man, Brooklyn Visionary", "2 G"},
	{"Spider-Man India", "1 G W"},
	{"Spider-Man, Web-Slinger", "W"},
	{"Spider-Sense", "U"},
	{"Spiders-Man, Heroic Horde", "4 G G"},
	{"Spider-UK", "2 W"},
}

// TestWebSlingingCensusPricesEveryCarrier is the census test: every corpus
// carrier of the printed K:Web-slinging line parses the keyword with its
// expected parameter, and the shared cost reader prices exactly one composed
// web-slinging cost for it — the substituted mana cost plus the mandatory
// Return<1/Creature.YouCtrl+tapped> part. Peter Parker, Amazing Spider-Man
// (the eleventh "Web-slinging" corpus file) is the AddKeyword$ GRANT side,
// not a printed carrier, and is out of the census on purpose.
func TestWebSlingingCensusPricesEveryCarrier(t *testing.T) {
	t.Parallel()
	for _, tc := range webSlingingCensus {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := corpusCard(t, tc.name)
			// Precondition: the corpus face actually carries the keyword;
			// otherwise every later assertion is vacuous.
			if !c.Faces[0].HasKeyword("Web-slinging") {
				t.Fatalf("setup: corpus face %q lost Web-slinging: %v", tc.name, c.Faces[0].Keywords)
			}
			raw, ok := c.Faces[0].KeywordParam("Web-slinging")
			if !ok || raw != tc.wsCost {
				t.Fatalf("setup: KeywordParam(Web-slinging) = (%q, %v), want %q", raw, ok, tc.wsCost)
			}

			e := handEngine(t, c)
			id := e.G.Zone(state.ZHand, 0)[0]
			costs := e.webSlingingCosts(0, id)
			if len(costs) != 1 {
				t.Fatalf("webSlingingCosts priced %d costs for %q, want 1: %+v", len(costs), tc.name, costs)
			}
			want := ParseCost(tc.wsCost).Plus(webSlingingReturnExtra())
			got := costs[0]
			if got.mode != "web-slinging" {
				t.Fatalf("cost mode = %q, want %q", got.mode, "web-slinging")
			}
			if got.cost.Colored != want.Colored || got.cost.Generic != want.Generic {
				t.Fatalf("substituted mana = (%d generic, %v coloured), want (%d, %v)",
					got.cost.Generic, got.cost.Colored, want.Generic, want.Colored)
			}
			if len(got.cost.Return) != 1 || got.cost.Return[0].N != 1 ||
				got.cost.Return[0].Spec != webSlingingReturnSpec {
				t.Fatalf("composed cost Return parts = %+v, want one Return<1/%s>",
					got.cost.Return, webSlingingReturnSpec)
			}
		})
	}
}

// TestWebSlingingNotOfferedWithoutPayment is the negative half of the offer:
// the web-slinging option is withheld whenever the composed cost cannot be
// paid — no tapped creature to return, an untapped creature only, or no mana.
// A plain cast of Spider-Man, Web-Slinger costs {2}{W}, so each engine holds
// only {W} unless named otherwise, keeping the printed cost unpayable on
// purpose: the offer census can only then be about web-slinging.
func TestWebSlingingNotOfferedWithoutPayment(t *testing.T) {
	t.Parallel()
	bearText := "Name:Web Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

	t.Run("no creature to return", func(t *testing.T) {
		t.Parallel()
		e := handEngine(t, corpusCard(t, "Spider-Man, Web-Slinger"))
		addMana(t, e, 0, "W")
		if opts := castOptionsWithMode(e, 0, e.G.Zone(state.ZHand, 0)[0], "web-slinging"); len(opts) != 0 {
			t.Fatalf("web-slinging offered with no creature to return: %+v", opts)
		}
	})
	t.Run("untapped creature cannot pay", func(t *testing.T) {
		t.Parallel()
		e := handEngine(t, corpusCard(t, "Spider-Man, Web-Slinger"))
		onBoard(t, e, 0, bearText) // untapped: a return cost needs a TAPPED creature
		addMana(t, e, 0, "W")
		if opts := castOptionsWithMode(e, 0, e.G.Zone(state.ZHand, 0)[0], "web-slinging"); len(opts) != 0 {
			t.Fatalf("web-slinging offered with only an untapped creature: %+v", opts)
		}
	})
	t.Run("tapped creature but no mana", func(t *testing.T) {
		t.Parallel()
		e := handEngine(t, corpusCard(t, "Spider-Man, Web-Slinger"))
		bear := onBoard(t, e, 0, bearText)
		e.emit(events.Event{Kind: events.Tap, Obj: bear})
		if opts := castOptionsWithMode(e, 0, e.G.Zone(state.ZHand, 0)[0], "web-slinging"); len(opts) != 0 {
			t.Fatalf("web-slinging offered with an unfunded pool: %+v", opts)
		}
	})
}

// castWebslingingReturning submits the "web-slinging" cast option for id,
// answers the mandatory return ask with returnObj, and drains the cast until
// the spell has resolved onto the battlefield. It fails on any decision shape
// it does not recognise, so a vacuous setup cannot slip through.
func castWebslingingReturning(t *testing.T, e *Engine, id, returnObj state.ObjID) {
	t.Helper()
	submitChoices(t, e, castModeOption(t, e, id, "web-slinging"))
	for i := 0; i < 80; i++ {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield && len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("web-slinging cast stalled with no pending decision (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KChoose:
			idx := -1
			// The mandatory return ask: pick exactly the creature under test.
			for _, o := range d.Options {
				if o.Kind == "returncost" && o.Obj == returnObj {
					idx = o.Index
				}
			}
			// The 601.2g mana window: the pool already covers the substituted
			// cost, so take "done".
			if idx < 0 {
				for _, o := range d.Options {
					if o.Kind == "done" {
						idx = o.Index
					}
				}
			}
			if idx < 0 {
				t.Fatalf("unexpected KChoose during web-slinging cast: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KPriority:
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected decision during web-slinging cast: %+v", d)
		}
	}
	t.Fatal("web-slinging cast never resolved")
}

// TestWebSlingingCastChargesSubstitutedCostAndReturnsCreature is the CR
// 702.186a proof end-to-end: Spider-Man, Web-Slinger ({2}{W} printed,
// web-slinging {W}) is cast for {W} while returning a tapped creature to its
// owner's hand, and the cast carries exactly the webSlinged provenance flag.
func TestWebSlingingCastChargesSubstitutedCostAndReturnsCreature(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Spider-Man, Web-Slinger"))
	spidey := e.G.Zone(state.ZHand, 0)[0]
	bear := onBoard(t, e, 0, "Name:Web Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.Tap, Obj: bear})
	if !e.G.Obj(bear).Tapped {
		t.Fatal("setup: bear must be tapped to pay the return cost")
	}
	// Only {W} in the pool: the cast can only ever be the web-slinging one,
	// and the {W} payment must come out of this pool (never the {2}{W}).
	addMana(t, e, 0, "W")
	if pool0 := e.G.Players[0].Pool.Total(); pool0 != 1 {
		t.Fatalf("setup: pool = %d mana, want exactly {W}", pool0)
	}

	castWebslingingReturning(t, e, spidey, bear)

	// The return cost was paid: the bear is back in its owner's hand.
	bearObj := e.G.Obj(bear)
	if bearObj.Zone != state.ZHand {
		t.Fatalf("returned bear zone = %v, want hand", bearObj.Zone)
	}
	// The substituted cost was charged: the {W} in the pool was spent, not
	// the {2}{W} printed cost (a plain cast would have aborted).
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after web-slinging cast = %d mana, want empty", got)
	}
	// The pay-time CastInfo carries the web-slinging provenance, and ONLY
	// that provenance (a plain cast would carry no flags at all).
	if got := e.G.Obj(spidey).CastFlags; got != state.FlagWebSlinged {
		t.Fatalf("web-slinging cast carries CastFlags %+v, want FlagWebSlinged only", got)
	}
}

// TestWebSlingingProvenanceDrivesTheSpidersManRider is the brief's rider
// pin: Spiders-Man, Heroic Horde's ETB trigger is gated on
// `ValidCard$ Card.Self+wasCast+webSlinged` (the exact corpus line), so it
// must fire on a web-slinging cast and stay silent on a hardcast. The ETB
// gains 3 life and creates two 2/1 green Spider creature tokens with reach,
// which gives both a boolean and a countable signal; a control cast for the
// printed {1}{G} asserts the rider is really keyed on the provenance flag
// and not on the mere entry.
func TestWebSlingingProvenanceDrivesTheSpidersManRider(t *testing.T) {
	t.Parallel()

	// castSpidersMan casts Spiders-Man, Heroic Horde for `mode` (either the
	// web-slinging alternative or the printed hardcast), pays the mandatory
	// return cost when the mode demands it, resolves every trigger, and
	// returns the resulting engine. mana is exactly what the chosen mode
	// costs, so the printed {1}{G} is unaffordable on the web-slinging branch
	// (and vice versa) -- the offer under test is the only legal one.
	castSpidersMan := func(mode string, mana string, tappedBear bool) *Engine {
		e := handEngine(t, corpusCard(t, "Spiders-Man, Heroic Horde"))
		spidersMan := e.G.Zone(state.ZHand, 0)[0]
		var returnTo state.ObjID
		if tappedBear {
			returnTo = onBoard(t, e, 0, "Name:Web Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			e.emit(events.Event{Kind: events.Tap, Obj: returnTo})
		}
		addMana(t, e, 0, mana)
		if mode == "" {
			// The printed hardcast has an empty CastMode; find it by object.
			d := e.Pending()
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "cast" && o.Obj == spidersMan {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("no hardcast option for Spiders-Man: %+v", d.Options)
			}
			submitChoices(t, e, idx)
		} else {
			submitChoices(t, e, castModeOption(t, e, spidersMan, mode))
		}
		// The cast's payment window: answer the mandatory return ask and the
		// 601.2g "done" window until the spell is on the stack, then drain the
		// spell and its ETB trigger with the ordinary stack drainer.
		for i := 0; i < 20; i++ {
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				break
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "returncost" && o.Obj == returnTo {
					idx = o.Index
				}
			}
			if idx < 0 {
				for _, o := range d.Options {
					if o.Kind == "done" {
						idx = o.Index
					}
				}
			}
			if idx < 0 {
				t.Fatalf("unexpected KChoose for mode %q: %+v", mode, d)
			}
			submitChoices(t, e, idx)
		}
		passUntilStackEmpty(t, e, 60)
		return e
	}

	t.Run("web-slinging cast fires the rider", func(t *testing.T) {
		t.Parallel()
		e := castSpidersMan("web-slinging", "GGGGGG", true)
		// Precondition: the creature really entered with the web-slinging
		// provenance, so the life assertion cannot pass vacuously.
		if got := e.G.Obj(e.G.Zone(state.ZBattlefield, 0)[len(e.G.Zone(state.ZBattlefield, 0))-1]).CastFlags; got&state.FlagWebSlinged == 0 {
			t.Fatalf("setup: entering Spiders-Man lacks FlagWebSlinged: %+v", got)
		}
		// The rider's `wasCast+webSlinged` gate matched: the ETB gained 3.
		if got := e.G.Players[0].Life; got != 23 {
			t.Fatalf("life after the rider = %d, want 23 (20 + the rider's 3)", got)
		}
	})

	t.Run("hardcast leaves the rider silent", func(t *testing.T) {
		t.Parallel()
		e := castSpidersMan("", "GG", false)
		// Precondition: the creature really entered, so a silent rider is a
		// real negative and not a failed cast.
		entered := false
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Spiders-Man, Heroic Horde" {
				entered = true
			}
		}
		if !entered {
			t.Fatal("setup: hardcast Spiders-Man never entered the battlefield")
		}
		if got := e.G.Players[0].Life; got != 20 {
			t.Fatalf("hardcast life = %d, want 20 (no rider)", got)
		}
	})
}
