package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Fight primitive (CR 701.12) pinned on real corpus carriers and on
// synthetic fixtures for the rider/edge shapes. The corpus carriers are read
// as script text so they seed through newFixtureDeck as real deck cards and
// replayCheck can replay them.

// corpusCardText reads one Forge script out of the gitignored corpus — a
// file-backed twin of msh_commander_trigger_test.go's mshCorpusCardPath.
func corpusCardText(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", ".cards", "cardsfolder", rel))
	if err != nil {
		t.Fatalf("read corpus card %s: %v", rel, err)
	}
	return string(b)
}

// fightDrain passes priority until the stack empties, answering the targeting
// asks Fight's paths pose along the way: the ETB placement ask (KTarget, from
// rules' pushTrigger) and the sub-shaped carve-out ask (KChoose "tgts", from
// effects' chosenTargetsFor). answer indexes the single offered target;
// declineAnswers makes every ask be answered with zero targets.
func fightDrain(t *testing.T, e *Engine, declineAnswers bool) {
	t.Helper()
	for i := 0; i < 30 && len(e.G.Stack) > 0 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KTarget:
			if declineAnswers {
				submitChoices(t, e)
			} else {
				submitChoices(t, e, 0)
			}
		case decision.KChoose:
			if declineAnswers {
				submitChoices(t, e)
			} else {
				submitChoices(t, e, 0)
			}
		case decision.KPriority:
			for _, o := range d.Options {
				if o.Kind == "pass" {
					submitChoices(t, e, o.Index)
					break
				}
			}
		default:
			t.Fatalf("unexpected decision %+v while draining", d)
		}
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack did not drain: depth %d", len(e.G.Stack))
	}
}

// fightDamageEvents returns every Damage event in the log.
func fightDamageEvents(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage {
			out = append(out, ev)
		}
	}
	return out
}

// TestWarbriarBlessingFightAnswered pins the execute-shaped Fight carrier end
// to end on the real corpus card: the ETB trigger's placement ask offers up
// to one target (TargetMin$ 0 | TargetMax$ 1), and the answered fight deals
// BOTH powers simultaneously — the two per-side-sourced Damage events sit
// adjacent in the event stream, inside one damage batch.
func TestWarbriarBlessingFightAnswered(t *testing.T) {
	e, cfg, aura := newFixtureDeck(t, 3, corpusCardText(t, "w/warbriar_blessing.txt"),
		"Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:3/3\nOracle:x\n")
	ox := putCreature(t, e, 0, "Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:3/3\nOracle:x\n")
	bear := putToken(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "CG")

	// Cast the aura on the Ox (Enchant: Creature.YouCtrl — the only legal
	// target), then let it resolve.
	opt := castOptionFor(t, e, aura)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != ox {
		t.Fatalf("aura cast target ask = %+v", d)
	}
	submitChoices(t, e, 0)
	fightDrain(t, e, false)
	if e.G.Obj(aura).AttachedTo != ox {
		t.Fatalf("aura attached to %d, want the Ox (%d)", e.G.Obj(aura).AttachedTo, ox)
	}

	// The fight: the 3-power Ox deals 3 to the 2/2 Bear, the Bear deals 2
	// back, both inside one batch.
	dmg := fightDamageEvents(e)
	if len(dmg) != 2 {
		t.Fatalf("fight damage events = %+v, want exactly 2", dmg)
	}
	if dmg[0].Obj != bear || dmg[0].Amount != 3 {
		t.Fatalf("fighter→victim damage = %+v, want bear taking 3", dmg[0])
	}
	if dmg[1].Obj != ox || dmg[1].Amount != 2 {
		t.Fatalf("victim→fighter damage = %+v, want ox taking 2", dmg[1])
	}
	if dmg[1].Seq-dmg[0].Seq > 2 {
		t.Fatalf("the two fight hits are %d seqs apart — not one simultaneous batch", dmg[1].Seq-dmg[0].Seq)
	}
	if e.G.Obj(ox).Zone != state.ZBattlefield {
		t.Fatalf("Ox (3 toughness) died to 2 damage")
	}
	if e.G.Obj(bear).Zone == state.ZBattlefield {
		t.Fatalf("Bear (2 toughness) survived 3 damage")
	}
	replayCheck(t, e, cfg)
}

// TestWarbriarBlessingFightDeclineDealsNothing pins the optional-target
// decline: answering the placement ask with zero targets (Min 0) deals no
// damage and emits no fight event.
func TestWarbriarBlessingFightDeclineDealsNothing(t *testing.T) {
	e, cfg, aura := newFixtureDeck(t, 4, corpusCardText(t, "w/warbriar_blessing.txt"),
		"Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:3/3\nOracle:x\n")
	ox := putCreature(t, e, 0, "Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:3/3\nOracle:x\n")
	bear := putToken(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "CG")

	opt := castOptionFor(t, e, aura)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != ox {
		t.Fatalf("aura cast target ask = %+v", d)
	}
	submitChoices(t, e, 0)
	before := len(fightDamageEvents(e))
	fightDrain(t, e, true) // decline the ETB placement ask
	if got := len(fightDamageEvents(e)); got != before {
		t.Fatalf("declined fight dealt damage: %d damage events after, want %d", got, before)
	}
	if e.G.Obj(ox).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("declined fight killed a creature")
	}
	replayCheck(t, e, cfg)
}

// TestKraulHarpoonerFightSubPosesItsOwnAsk pins the SUB-shaped Fight carrier
// (Kraul Harpooner: DB$ Pump | SubAbility$ DBFight): the placement walk now
// reaches the depth-2 Fight link, so the fight's target is announced at
// placement (CR 603.3d, KTarget/trig_sub) before anyone gets priority, and
// the answered fight resolves without re-posing the ask mid-resolution.
func TestKraulHarpoonerFightSubPosesItsOwnAsk(t *testing.T) {
	kraulSrc := corpusCardText(t, "k/kraul_harpooner.txt")
	e, cfg, _ := newFixtureDeck(t, 5, kraulSrc)
	dragon := putToken(t, e, 1, "Name:Dragon\nManaCost:2 U\nTypes:Creature Dragon\nK:Flying\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	kraul := putCreature(t, e, 0, kraulSrc)
	e.pending = nil
	e.Advance()

	// The ETB trigger has no ValidTgts$ (TrigPump), but its depth-2 Fight
	// link does: the announced ask surfaces at placement as KTarget/trig_sub.
	for i := 0; i < 30 && len(e.G.Stack) > 0 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		switch {
		case d.Kind == decision.KTarget && len(d.Options) == 1 && d.Options[0].Obj == dragon:
			// The Fight link's announcement: Min 0 / Max 1 over the flying
			// creature, posed before priority rather than at resolution.
			if d.Min != 0 || d.Max != 1 {
				t.Fatalf("fight sub ask bounds = %d..%d, want 0..1", d.Min, d.Max)
			}
			if d.ResumeKind != "trig_sub" {
				t.Fatalf("fight sub ask ResumeKind = %q, want trig_sub", d.ResumeKind)
			}
			submitChoices(t, e, 0)
		case d.Kind == decision.KPriority:
			for _, o := range d.Options {
				if o.Kind == "pass" {
					submitChoices(t, e, o.Index)
					break
				}
			}
		default:
			t.Fatalf("unexpected decision %+v while draining", d)
		}
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack did not drain: depth %d", len(e.G.Stack))
	}
	// The answered fight: Kraul Harpooner (3 power, +0 from an empty
	// graveyard) deals 3 to the 1/1 Dragon; the Dragon deals 1 back.
	dmg := fightDamageEvents(e)
	if len(dmg) != 2 {
		t.Fatalf("fight damage events = %+v, want exactly 2", dmg)
	}
	if dmg[0].Obj != dragon || dmg[0].Amount != 3 {
		t.Fatalf("fighter→victim damage = %+v, want dragon taking 3", dmg[0])
	}
	if dmg[1].Obj != kraul || dmg[1].Amount != 1 {
		t.Fatalf("victim→fighter damage = %+v, want kraul taking 1", dmg[1])
	}
	if e.G.Obj(dragon).Zone == state.ZBattlefield {
		t.Fatal("Dragon (1 toughness) survived 3 damage")
	}
	if e.G.Obj(kraul).Zone != state.ZBattlefield {
		t.Fatal("Kraul Harpooner (2 toughness) died to 1 damage")
	}
	replayCheck(t, e, cfg)
}

// TestFightLifelinkPaysTheFighter pins the per-side rider: the FIGHTING
// creature is each hit's damage source, not the resolving spell — a
// lifelinked fighter's controller gains its power in life from ITS hit,
// where the resolving source is the aura that asked for the fight.
func TestFightLifelinkPaysTheFighter(t *testing.T) {
	const auraSrc = "Name:Lifeblessing\nManaCost:G\nTypes:Enchantment Aura\nK:Enchant:Creature.YouCtrl\n" +
		"S:Mode$ Continuous | Affected$ Creature.EnchantedBy | AddKeyword$ Lifelink | Description$ x\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigFight | TriggerDescription$ x\n" +
		"SVar:TrigFight:DB$ Fight | Defined$ Enchanted | ValidTgts$ Creature.YouDontCtrl | TargetMin$ 0 | TargetMax$ 1\n"
	e, cfg, aura := newFixtureDeck(t, 7, auraSrc,
		"Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:2/3\nOracle:x\n")
	ox := putCreature(t, e, 0, "Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:2/3\nOracle:x\n")
	bear := putToken(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/3\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "G")
	opt := castOptionFor(t, e, aura)
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget || d.Options[0].Obj != ox {
		t.Fatalf("aura cast target ask = %+v", d)
	}
	submitChoices(t, e, 0)
	lifeBefore := e.G.Players[0].Life
	fightDrain(t, e, false)
	if !e.HasKeyword(ox, "Lifelink") {
		t.Fatal("the static's Lifelink is not derived on the enchanted Ox")
	}
	dmg := fightDamageEvents(e)
	if len(dmg) != 2 || dmg[0].Obj != bear || dmg[0].Amount != 2 || dmg[1].Obj != ox || dmg[1].Amount != 2 {
		t.Fatalf("fight damage = %+v, want bear 2 / ox 2", dmg)
	}
	if got := e.G.Players[0].Life; got != lifeBefore+2 {
		t.Fatalf("seat 0 life = %d, want %d (the fighter's own power gained from ITS hit)", got, lifeBefore+2)
	}
	if got := e.G.Players[1].Life; got != lifeBefore {
		t.Fatalf("seat 1 life moved: %d", got)
	}
	if e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(bear).Counter("Deathtouched") != 0 {
		t.Fatal("the Bear should have taken 2 plain damage and survived")
	}
	replayCheck(t, e, cfg)
}

// TestFightDeathtouchNamesTheFighter pins the rider's source attribution the
// other way: the fighter's hit carries the FIGHTER's deathtouch, not the
// resolving aura's (the aura has none) — one point of fighter damage
// destroys the 3-toughness victim through CR 704.5g's deathtouch SBA, with
// the Deathtouched witness counter emitted on the hit before the SBA
// destroys it.
func TestFightDeathtouchNamesTheFighter(t *testing.T) {
	const auraSrc = "Name:Deathblessing\nManaCost:G\nTypes:Enchantment Aura\nK:Enchant:Creature.YouCtrl\n" +
		"S:Mode$ Continuous | Affected$ Creature.EnchantedBy | AddKeyword$ Deathtouch | Description$ x\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigFight | TriggerDescription$ x\n" +
		"SVar:TrigFight:DB$ Fight | Defined$ Enchanted | ValidTgts$ Creature.YouDontCtrl | TargetMin$ 0 | TargetMax$ 1\n"
	e, cfg, aura := newFixtureDeck(t, 8, auraSrc,
		"Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:1/3\nOracle:x\n")
	ox := putCreature(t, e, 0, "Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:1/3\nOracle:x\n")
	bear := putToken(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/3\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "G")
	opt := castOptionFor(t, e, aura)
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget || d.Options[0].Obj != ox {
		t.Fatalf("aura cast target ask = %+v", d)
	}
	submitChoices(t, e, 0)
	fightDrain(t, e, false)
	dmg := fightDamageEvents(e)
	if len(dmg) != 2 || dmg[0].Obj != bear || dmg[0].Amount != 1 || dmg[1].Obj != ox || dmg[1].Amount != 2 {
		t.Fatalf("fight damage = %+v, want bear 1 / ox 2", dmg)
	}
	// The hit carried the FIGHTER's deathtouch: one point of fighter damage
	// destroyed the 3-toughness Bear through CR 704.5g, and the Deathtouched
	// witness counter rode the hit before the SBA destroyed it.
	witnessed := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == bear && ev.Counter == "Deathtouched" && ev.Amount == 1 {
			witnessed = true
		}
	}
	if !witnessed {
		t.Fatal("the fighter's hit carried no Deathtouched witness counter")
	}
	if e.G.Obj(bear).Zone == state.ZBattlefield {
		t.Fatal("Bear survived one point of fighter deathtouch damage")
	}
	if e.G.Obj(ox).Zone != state.ZBattlefield {
		t.Fatal("Ox (3 toughness) died to 2 plain damage")
	}
	replayCheck(t, e, cfg)
}

// TestFightZeroPowerAndOffBattlefieldNoWedge pins the as-much-as-possible
// edges at the SA level: a zero-power fighter deals its (zero) damage like
// effDealDamage's n<=0 shape, and a fighter or opponent that has already left
// the battlefield is skipped — both sides no-op cleanly, no wedge, no panic.
func TestFightZeroPowerAndOffBattlefieldNoWedge(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, "Name:Vanisher\nManaCost:U\nTypes:Creature\nPT:0/1\nOracle:x\n"))
	ox := onBoard(t, e, 0, "Name:Ox\nManaCost:G\nTypes:Creature Ox\nPT:0/1\nOracle:x\n")
	bear := onBoard(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	sa := &cards.SA{Kind: "DB", API: "Fight", Params: map[string]string{
		"Defined": "Self", "ValidTgts": "Creature",
		"TargetMin": "0", "TargetMax": "1", "TgtPrompt": "x"}}

	// Zero power is a legal deal: the 0-power fighter's hit emits Amount 0,
	// and the Bear's 2-power hit lands.
	before := len(fightDamageEvents(e))
	effects.Resolve(e, &effects.Ctx{Source: ox, Controller: 0,
		Targets: []state.Target{{Obj: bear}}, TargetsOffered: true}, sa)
	dmg := fightDamageEvents(e)[before:]
	if len(dmg) != 2 || dmg[0].Obj != bear || dmg[0].Amount != 0 || dmg[1].Obj != ox || dmg[1].Amount != 2 {
		t.Fatalf("zero-power fight = %+v, want bear taking 0 and ox taking 2", dmg)
	}

	// An opponent no longer on the battlefield is skipped: no damage at all.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	before = len(fightDamageEvents(e))
	effects.Resolve(e, &effects.Ctx{Source: ox, Controller: 0,
		Targets: []state.Target{{Obj: bear}}, TargetsOffered: true}, sa)
	if got := fightDamageEvents(e); len(got) != before {
		t.Fatalf("off-battlefield fight emitted %+v, want nothing", got[before:])
	}

	// A fighter off the battlefield (a graveyard fixture as Source) is
	// skipped too, and the empty-opponent decline shape is a silent no-op.
	dead := onBoardCard(t, e, 1, card(t, "Name:Ghost\nTypes:Creature\nPT:2/2\nOracle:x\n"))
	e.emit(events.Event{Kind: events.MoveZone, Obj: dead, From: state.ZBattlefield, To: state.ZGraveyard})
	before = len(fightDamageEvents(e))
	effects.Resolve(e, &effects.Ctx{Source: dead, Controller: 1,
		Targets: []state.Target{{Obj: ox}}, TargetsOffered: true}, sa)
	effects.Resolve(e, &effects.Ctx{Source: ox, Controller: 0,
		Targets: nil, TargetsOffered: true}, sa)
	if got := fightDamageEvents(e); len(got) != before {
		t.Fatalf("dead-fighter / empty-decline fight emitted %+v, want nothing", got[before:])
	}
}
