package rules

// The Affinity task: K:Affinity:<spec> expands (cards/keywords.go) into a
// self-scoped ReduceCost cost static priced by a Count$Valid <spec>+YouCtrl
// SVar, and rides the EXISTING cost-modifier machinery unchanged
// (collectCostStatics / costStaticApplies / modAmountX). Every fixture
// below embeds the real corpus script (never a committed .txt).

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const frogmiteSrc = "Name:Frogmite\nManaCost:4\nTypes:Artifact Creature Frog\nPT:2/2\n" +
	"K:Affinity:Artifact\nOracle:Affinity for artifacts (This spell costs {1} less to cast for each artifact you control.)\n"

const artCritterSrc = "Name:Trinket\nManaCost:1\nTypes:Artifact Creature Golem\nPT:1/1\nOracle:x\n"

const banquetGuestsSrc = "Name:Banquet Guests\nManaCost:X G W\nTypes:Creature Halfling Citizen\nPT:0/0\n" +
	"K:Affinity:Food\nK:Trample\nK:etbCounter:P1P1:Y:no Condition:CARDNAME enters with twice X +1/+1 counters on it.\n" +
	"SVar:X:Count$xPaid\nSVar:Y:SVar$X/Twice\n" +
	"A:AB$ Pump | Cost$ 2 Sac<1/Food> | Defined$ Self | KW$ Indestructible | SpellDescription$ CARDNAME gains indestructible until end of turn.\n" +
	"Oracle:Affinity for Foods (This spell costs {1} less to cast for each Food you control.)\\nTrample\\nBanquet Guests enters with twice X +1/+1 counters on it.\\n{2}, Sacrifice a Food: Banquet Guests gains indestructible until end of turn.\n"

const foodSrc = "Name:Yummy\nManaCost:0\nTypes:Artifact Food\nOracle:x\n"

// TestFrogmiteAffinityReducesByArtifactsControlled pins the reduction
// amount against the battlefield count: two artifacts out -> {4} prices {2};
// a third makes the spell castable from an EMPTY pool and the pool ends 0.
func TestFrogmiteAffinityReducesByArtifactsControlled(t *testing.T) {
	t.Parallel()
	e, cfg, frogmite := newFixtureDeck(t, 701, frogmiteSrc, artCritterSrc, artCritterSrc, artCritterSrc)
	putCreature(t, e, 0, artCritterSrc)
	putCreature(t, e, 0, artCritterSrc)
	if got := reduceOf(t, e, 0, frogmite); got != 2 {
		t.Fatalf("Frogmite reduction with 2 artifacts out = %d, want 2", got)
	}

	// The third artifact takes the price to {0}: castable from an empty
	// pool, and the pool ends 0.
	putCreature(t, e, 0, artCritterSrc)
	addMana(t, e, 0, "C")
	opt := castByName(t, e, 0, "Frogmite")
	if opt == nil {
		t.Fatal("Frogmite must be castable from hand with 3 artifacts out")
	}
	submitChoices(t, e, opt.Index)
	if e.G.Obj(frogmite).Zone != state.ZStack {
		t.Fatalf("Frogmite on %s, want stack", e.G.Obj(frogmite).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after the cast = %d, want 0", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestBanquetGuestsFoodAffinityCoversGeneric pins the end-to-end card: with
// X announced 2 and 3 Foods out, the affinity reduction (3) covers the
// whole generic {2} — never the coloured pips — so {X}{G}{W} is paid as
// {G}{W} exactly. With no Foods the reduction is 0 and the generic is real.
func TestBanquetGuestsFoodAffinityCoversGeneric(t *testing.T) {
	t.Parallel()
	e, cfg, guests := newFixtureDeck(t, 702, banquetGuestsSrc, foodSrc, foodSrc, foodSrc)
	putCreature(t, e, 0, foodSrc)
	putCreature(t, e, 0, foodSrc)
	putCreature(t, e, 0, foodSrc)
	if got := reduceOf(t, e, 0, guests); got != 3 {
		t.Fatalf("Banquet Guests reduction with 3 Foods out = %d, want 3", got)
	}
	addMana(t, e, 0, "GW")
	opt := castByName(t, e, 0, "Banquet Guests")
	if opt == nil {
		t.Fatal("Banquet Guests must be castable with 3 Foods and a pool of exactly {G}{W}")
	}
	submitChoices(t, e, opt.Index)
	announceXCast(t, e, guests, 2)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the cast = %d, want 0 ({G}{W} paid, generic {2} fully reduced)", got)
	}
	replayCheck(t, e, cfg)
}

// TestBanquetGuestsNoFoodsPricesFullGeneric pins reduction 0: no Foods out
// means no discount and the {2} generic is real money.
func TestBanquetGuestsNoFoodsPricesFullGeneric(t *testing.T) {
	t.Parallel()
	e, cfg, guests := newFixtureDeck(t, 703, banquetGuestsSrc)
	if got := reduceOf(t, e, 0, guests); got != 0 {
		t.Fatalf("Banquet Guests reduction with no Foods = %d, want 0", got)
	}
	addMana(t, e, 0, "CCGW")
	opt := castByName(t, e, 0, "Banquet Guests")
	if opt == nil {
		t.Fatal("Banquet Guests must be castable with a pool of exactly {2}{G}{W}")
	}
	submitChoices(t, e, opt.Index)
	announceXCast(t, e, guests, 2)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the cast = %d, want 0 (full generic {2} paid)", got)
	}
	replayCheck(t, e, cfg)
}

// announceXCast submits the chosen X announce (label "X = <n>") for the
// X-cost spell sitting at the cast choice, then asserts it lands on the
// stack with that binding.
func announceXCast(t *testing.T, e *Engine, id state.ObjID, x int32) {
	t.Helper()
	d := e.Pending()
	if d == nil || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("want the X announce first, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Label == "X = "+strconv.Itoa(int(x)) {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no X = %d option: %+v", x, d)
	}
	submitChoices(t, e, idx)
	o := e.G.Obj(id)
	if o.Zone != state.ZStack || o.X != x {
		t.Fatalf("after X=%d: zone %s X %d, want stack/%d", x, o.Zone, o.X, x)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// junkWinderSrc is the real Junk Winder script (Junk Winder is one of the 77
// Affinity cards; its spec "Permanent.token" is the one that carries a dot
// before its display field, so the SVar must join with '+'):
// Count$Valid Permanent.token+YouCtrl.
const junkWinderSrc = "Name:Junk Winder\nManaCost:5 U U\nTypes:Creature Serpent\nPT:5/6\n" +
	"K:Affinity:Permanent.token:token\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Permanent.token+YouCtrl | TriggerZones$ Battlefield | Execute$ TrigTap | TriggerDescription$ Whenever a token you control enters, tap target nonland permanent an opponent controls. It doesn't untap during its controller's next untap step.\n" +
	"SVar:TrigTap:DB$ Tap | ValidTgts$ Permanent.nonLand+OppCtrl | TgtPrompt$ Choose target nonland permanent an opponent controls\n" +
	"Oracle:Affinity for tokens (This spell costs {1} less to cast for each token you control.)\\nWhenever a token you control enters, tap target nonland permanent an opponent controls.\n"

const probeTokenSrc = "Name:Winder Token\nManaCost:0\nTypes:Artifact Creature Golem\nPT:1/1\nOracle:x\n"

// TestJunkWinderAffinityCountsTokensOut pins the one dotted-spec exotic the
// brief named as unmeasured: Count$Valid Permanent.token+YouCtrl counts
// real tokens, so a token you control reduces the Winder by 1 and its cost
// drops to {4}{U}{U}.
func TestJunkWinderAffinityCountsTokensOut(t *testing.T) {
	e, cfg, winder := newFixtureDeck(t, 704, junkWinderSrc, probeTokenSrc)
	if got := reduceOf(t, e, 0, winder); got != 0 {
		t.Fatalf("Junk Winder reduction with no tokens out = %d, want 0", got)
	}
	putToken(t, e, 0, probeTokenSrc, state.ZBattlefield)
	if got := reduceOf(t, e, 0, winder); got != 1 {
		t.Fatalf("Junk Winder reduction with 1 token out = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}
