package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// K:Sneak (CR 702.190) is pinned end to end on a real corpus carrier, Oroku
// Saki, Shredder Rising ("Sneak {1}{B}"). Sneak is a CAST for an alternative
// cost (not an activated ability, the Ninjutsu distinction), offered only
// during the caster's own declare-blockers step, whose mandatory additional
// cost is returning an unblocked attacker you control (rules/sneak.go's
// sneakCosts); the permanent enters tapped and attacking the returned
// creature's defender (CR 702.190b), and the `sneaked` filter predicate reads
// the paid cost (effects/filter.go).

// TestSneakRealCardEntersTappedAndAttacking is the positive pin: the printed
// Oroku Saki is offered the "sneak" cast at the declare-blockers step, its
// Return cost is paid by an unblocked attacker, and it lands on the
// battlefield tapped and attacking the SAME defender the returned creature
// attacked (CR 702.190b).
func TestSneakRealCardEntersTappedAndAttacking(t *testing.T) {
	if !effects.Supported()["kw:Sneak"] {
		t.Fatal("kw:Sneak is not registered; the coverage ratchet would still report it")
	}
	reg := searchTestRegistry(t)
	saki := searchCorpusCard(t, reg, "Oroku Saki, Shredder Rising")
	if d := saki.Link(); len(d) != 0 {
		t.Fatalf("link Oroku Saki, Shredder Rising: %v", d)
	}
	if !saki.Faces[0].HasKeyword("Sneak") {
		t.Fatal("precondition: Oroku Saki does not print Sneak in the corpus")
	}
	e, cfg := ninjutsuDeck(t, 9501, saki)
	sakiID := searchMoveByName(t, e, "Oroku Saki, Shredder Rising", state.ZHand)
	if o := e.G.Obj(sakiID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Oroku Saki not in hand: %+v", o)
	}
	bear := attackWithBear(t, e)

	// Fund exactly the sneak cost {1}{B}; the normal cost {2}{B} needs three
	// mana, so a cast here can only be the sneak alternative.
	fundPool(t, e, "CB")
	opt := castByName(t, e, 0, "Oroku Saki, Shredder Rising")
	if opt == nil || opt.Mode != "sneak" {
		t.Fatalf("sneak cast not offered at the declare-blockers step: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)

	// The Return cost asks which unblocked attacker pays. Precondition: it is
	// the Bear, still a battlefield attacker at answer time.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "returncost" {
		t.Fatalf("sneak did not ask to return an unblocked attacker: %+v", d)
	}
	chosen := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			chosen = o.Index
		}
	}
	if chosen < 0 {
		t.Fatalf("sneak return ask did not offer the unblocked Bear: %+v", d.Options)
	}
	submitChoices(t, e, chosen)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the returned Bear is in %v, want its owner's hand", o)
	}
	o := e.G.Obj(sakiID)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Oroku Saki is in %v, want the battlefield", o)
	}
	if !o.Tapped {
		t.Error("Oroku Saki entered untapped, want tapped (CR 702.190b)")
	}
	if !o.IsAttacking || o.Attacking != 1 {
		t.Errorf("Oroku Saki attacking=%v defender=%d, want attacking seat 1 (CR 702.190b)", o.IsAttacking, o.Attacking)
	}
	if o.CastFlags&state.FlagSneaked == 0 {
		t.Errorf("the entered permanent does not carry FlagSneaked")
	}
	// The `sneaked` filter predicate reads the paid cost off the permanent.
	if !e.matchesSpec("Card.sneaked", sakiID, e.specCtx(0, 0)) {
		t.Error("Card.sneaked does not match the sneak-cast permanent")
	}
	replayCheck(t, e, cfg)
}

// TestSneakNotOfferedOutsideDeclareBlockers pins CR 702.190a's window: at the
// declare-attackers step the sneak cast is withheld, so the offer really is
// confined to the one step.
func TestSneakNotOfferedOutsideDeclareBlockers(t *testing.T) {
	saki := searchCorpusCard(t, searchTestRegistry(t), "Oroku Saki, Shredder Rising")
	e, _ := ninjutsuDeck(t, 9502, saki)
	searchMoveByName(t, e, "Oroku Saki, Shredder Rising", state.ZHand)
	bear := putCreature(t, e, 0, ninjutsuBearSrc)
	e.priorityRound()
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, bear)
	fundPool(t, e, "CB")
	if e.G.Step != state.StepDeclareAttackers {
		t.Fatalf("expected the declare-attackers window, got step %s", e.G.Step)
	}
	if opt := castByName(t, e, 0, "Oroku Saki, Shredder Rising"); opt != nil && opt.Mode == "sneak" {
		t.Fatalf("sneak was offered at the declare-attackers step: %+v", opt)
	}
}

// TestSneakWithholdsWhenTheOnlyAttackerIsBlocked pins the
// attacking+unblocked half of the Return cost spec: a blocked attacker is not
// a legal payment, so the sneak option is withheld entirely (never offered
// then aborted).
func TestSneakWithholdsWhenTheOnlyAttackerIsBlocked(t *testing.T) {
	saki := searchCorpusCard(t, searchTestRegistry(t), "Oroku Saki, Shredder Rising")
	e, cfg := ninjutsuDeckWithBlocker(t, 9503, saki)
	searchMoveByName(t, e, "Oroku Saki, Shredder Rising", state.ZHand)
	bear, wall := attackWithBearBlockedBySeatOne(t, e)
	if o := e.G.Obj(bear); o == nil || len(o.BlockedBy) == 0 || o.BlockedBy[0] != wall {
		t.Fatalf("precondition: Bear should be blocked by the Wall Bear, got %+v", o)
	}
	fundPool(t, e, "CB")
	if opt := castByName(t, e, 0, "Oroku Saki, Shredder Rising"); opt != nil && opt.Mode == "sneak" {
		t.Fatalf("sneak was offered with a BLOCKED attacker; the unblocked cost gate did not bind")
	}
	replayCheck(t, e, cfg)
}

// TestSneakCorpusCensusNamesEveryCarrier is the accountability census for
// kw:Sneak: every corpus card whose face prints a K:Sneak line must resolve
// and must no longer be reported unsupported for `kw:Sneak`. It names the
// count (27 distinct card names at the current FORGE_REF) so a regression that
// silently unregisters the keyword is caught here, and it names Ninja Teen's
// level-3 grant (AddKeyword$ Sneak:3 B + MayPlay$ True | ValidSA$ Spell.Sneak)
// as the mechanism's granted instance. The 26 TMT carriers live in
// setAuditTMTNames; Elektra, Daughter of the Hand prints Sneak but is not in
// that audit's 195-name set (its face also prints another name), hence 27
// corpus-wide against 26 in-set.
func TestSneakCorpusCensusNamesEveryCarrier(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Fatal("precondition: corpus registry missing (.cards not linked?)")
	}
	supported := effects.Supported()
	if !supported["kw:Sneak"] {
		t.Fatal("kw:Sneak is not registered; the coverage census would report every carrier")
	}
	var carriers []string
	for _, c := range reg.Cards {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			if f != nil && f.HasKeyword("Sneak") {
				carriers = append(carriers, f.Name)
				break
			}
		}
	}
	if len(carriers) != 27 {
		t.Fatalf("kw:Sneak carriers = %d, want 27; carriers=%v", len(carriers), carriers)
	}
	for _, name := range carriers {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("census: corpus is missing %q", name)
		}
		for _, m := range reg.Unsupported(c, supported) {
			if m == "kw:Sneak" {
				t.Errorf("%s is still reported unsupported for kw:Sneak", name)
			}
		}
	}
	// Ninja Teen's level 3 is the granted instance of the mechanism.
	ninja, ok := reg.Lookup("Ninja Teen")
	if !ok {
		t.Fatal("census: corpus is missing Ninja Teen")
	}
	grant := ""
	for _, f := range ninja.Faces {
		if f == nil {
			continue
		}
		for _, body := range f.SVars {
			if strings.Contains(body, "AddKeyword$ Sneak:") {
				grant = body
			}
		}
	}
	if grant == "" {
		t.Fatal("Ninja Teen's level-3 AddKeyword$ Sneak grant is gone from the corpus")
	}
}
