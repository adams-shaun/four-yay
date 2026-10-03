package rules

// This file pins DB$ CopyPermanent's ability/trigger/attachment/choice riders
// on real corpus cards (task copyp1's second follow-up): AddTriggers$ +
// AddSVars$, AddAbilities$, AttachedTo$ and the sole supported Choices$/
// Chooser$ shape. Every test asserts the rider's end-to-end effect on the
// MINTED COPY (never on the source), that the implemented families are absent
// from any CopyPermanent skip Note, and that the game replays byte-identically
// from its log.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// resolveSourceFaceSA returns the named SVar body off obj's current face.
func resolveSourceFaceSA(t *testing.T, e *Engine, id state.ObjID, name string) *cards.SA {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("object %d has no face", id)
	}
	sa := cards.ResolveSVar(o.Face().SVars, name)
	if sa == nil {
		t.Fatalf("object %d face %q does not define SVar %q", id, o.Face().Name, name)
	}
	return sa
}

// TestAggressiveBiomancy pins AddTriggers$ + AddSVars$
// on the real corpus sorcery: the copy of a creature you control carries the
// granted "When this creature enters, it fights up to one target creature you
// don't control" trigger, its Execute$ body resolves from the SOURCE
// (Biomancy) SVar table, the trigger fires exactly once, and the copied
// original never gains the trigger.
func TestAggressiveBiomancy(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Aggressive Biomancy"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{lookup(t, reg, "Wall of Stone")})
	moveByName(t, e, 0, "Aggressive Biomancy", state.ZHand)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	// The fight needs a creature the caster does not control.
	giant := moveByName(t, e, 1, "Wall of Stone", state.ZBattlefield)
	if e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(giant).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: bear zone=%s giant zone=%s, want both battlefield",
			e.G.Obj(bear).Zone, e.G.Obj(giant).Zone)
	}

	addMana(t, e, 0, "GGUUUU") // {X}{X}{G}{U} for X=1
	opt := castByName(t, e, 0, "Aggressive Biomancy")
	if opt == nil {
		t.Fatalf("Aggressive Biomancy not castable: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	// The X ask (NumCopies$ X over SVar:X:Count$xPaid) precedes the target
	// ask; answer X=1, then the target with the bear.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the X choose decision, got %+v", d)
	}
	xIdx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 1 {
			xIdx = o.Index
		}
	}
	if xIdx < 0 {
		t.Fatalf("no X=1 option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{xIdx}}); err != nil {
		t.Fatalf("submit X: %v", err)
	}
	answerKTarget(t, e, bear)

	// Resolve the sorcery. Its granted ETB must pose the Fight target ask;
	// choose the Wall explicitly rather than letting a generic drain accept an
	// arbitrary option, so the resulting two-way damage proves the rider's
	// Execute$ body reached end-to-end resolution.
	sawFightTarget := false
	for i := 0; i < 40 && !e.G.Over; i++ {
		if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
			break
		}
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision has no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KTriggerOrder:
			answerTriggerOrders(t, e)
		case decision.KTarget:
			if sawFightTarget {
				t.Fatalf("the copied creature's ETB posed a second target ask: %+v", d)
			}
			if d.Min != 0 || d.Max != 1 {
				t.Fatalf("Fight target bounds = %d..%d, want 0..1", d.Min, d.Max)
			}
			idx := -1
			for _, o := range d.Options {
				if o.Obj == giant {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("Fight target options = %+v, want Wall of Stone %d", d.Options, giant)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit Fight target: %v", err)
			}
			sawFightTarget = true
		default:
			t.Fatalf("unexpected decision while resolving Aggressive Biomancy: %+v", d)
		}
	}
	if !sawFightTarget {
		t.Fatal("the copied creature's granted Fight trigger never posed its target decision")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("Aggressive Biomancy did not finish resolving (stack depth %d)", len(e.G.Stack))
	}

	copyID := findTokenCopyOf(t, e, e.G.Obj(bear).Card, bear)
	if e.G.Obj(copyID).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: the copy is not on the battlefield (zone %s)", e.G.Obj(copyID).Zone)
	}
	if cp, gp := e.Power(copyID), e.Power(giant); cp != 2 || gp != 0 {
		t.Fatalf("fight power precondition copy/Wall = %d/%d, want 2/0", cp, gp)
	}
	damage := fightDamageEvents(e)
	if len(damage) != 2 {
		t.Fatalf("granted Fight damage events = %+v, want exactly the two fight hits", damage)
	}
	if damage[0].Obj != giant || damage[0].Amount != 2 {
		t.Fatalf("copy→Wall fight hit = %+v, want Wall %d taking 2", damage[0], giant)
	}
	if damage[1].Obj != copyID || damage[1].Amount != 0 {
		t.Fatalf("Wall→copy fight hit = %+v, want copy %d taking 0", damage[1], copyID)
	}
	if e.G.Obj(giant).Damage != 2 || e.G.Obj(copyID).Damage != 0 {
		t.Fatalf("fight damage state Wall/copy = %d/%d, want 2/0", e.G.Obj(giant).Damage, e.G.Obj(copyID).Damage)
	}
	// The granted trigger must be on the COPY only, with its Execute body
	// resolved from the Biomancy table (a Fight whose target filter is the
	// source SVar's, never the copy's empty table).
	grant := grantedTriggerCE(t, e, copyID, "Aggressive Biomancy")
	if grant == nil {
		t.Fatalf("the copy has no granted ETB trigger sourced to Biomancy")
	}
	grantor := e.G.Obj(grant.TriggerGrantor)
	if grantor == nil || grantor.Face() == nil {
		t.Fatalf("grantor %d has no face", grant.TriggerGrantor)
	}
	body := cards.ResolveSVar(grantor.Face().SVars, grant.AddTrigger.Params["Execute"])
	if body == nil || body.API != "Fight" {
		t.Fatalf("the granted trigger's Execute$ does not resolve to a Fight body on the source table: %+v", grant.AddTrigger.Params)
	}
	if got := body.Params["ValidTgts"]; got != "Creature.YouDontCtrl" {
		t.Fatalf("the granted Fight's ValidTgts = %q, want the source SVar's Creature.YouDontCtrl", got)
	}
	if grantedTriggerCE(t, e, bear, "Aggressive Biomancy") != nil {
		t.Fatal("the copied original gained the granted ETB trigger -- the grant leaked")
	}
	if n := countGrantTriggerPush(e); n != 1 {
		t.Fatalf("the granted trigger pushed %d time(s), want 1 (notes: %v)", n, copyNotes(e))
	}
	noCopyPermanentModNote(t, e, "AddTriggers", "AddSVars")
	replayCheck(t, e, cfg)
}

// grantedTriggerCE returns the AddTrigger continuous effect on id whose body
// resolves from the named grantor face, or nil.
func grantedTriggerCE(t *testing.T, e *Engine, id state.ObjID, grantorName string) *state.ContinuousEffect {
	t.Helper()
	for _, ce := range e.active() {
		if ce.Source != id || ce.AddTrigger == nil {
			continue
		}
		if ce.AddTrigger.Mode != "ChangesZone" {
			continue
		}
		g := e.G.Obj(ce.TriggerGrantor)
		if g != nil && g.Face() != nil && g.Face().Name == grantorName {
			cp := ce
			return &cp
		}
	}
	return nil
}

func countGrantTriggerPush(e *Engine) int {
	return countEvents(e, func(ev events.Event) bool { return ev.Kind == events.GrantTriggerPush })
}

// copyNotes returns every Note text in the log, for a diagnostic failure.
func copyNotes(e *Engine) []string {
	var out []string
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note {
			out = append(out, ev.Text)
		}
	}
	return out
}

// TestShelobFoodCopy uses Brenard, Ginger Sculptor --
// a real carrier of AddAbilities$ FoodSac, alongside Shelob, Child of
// Ungoliant -- because Shelob's own death trigger is gated on the unmodelled
// DamagedBySpider predicate. The copy of the dead creature is a Food with
// "{2}, {T}, Sacrifice this artifact: You gain 3 life", the ability is
// granted on the COPY, activating it pays the {2} and sacrifices the copy to
// gain 3 life, and it is never offered on the original effect source.
func TestShelobFoodCopy(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Brenard, Ginger Sculptor"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{})
	brenard := moveByName(t, e, 0, "Brenard, Ginger Sculptor", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(brenard).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: brenard=%s bear=%s, want both battlefield",
			e.G.Obj(brenard).Zone, e.G.Obj(bear).Zone)
	}
	// "Whenever another nontoken creature you control dies, you may exile it.
	// If you do, create a token that's a copy..."
	e.emit(events.Sacrifice(bear))
	drainOptionalYes(t, e, 40)

	copyID := findTokenCopyOf(t, e, e.G.Obj(bear).Card, bear)
	if e.G.Obj(copyID).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: the Food copy is not on the battlefield (zone %s)", e.G.Obj(copyID).Zone)
	}
	// The copy's granted ability list must name the source table's FoodSac.
	if !hasGrantedAbility(t, e, copyID, "FoodSac") {
		t.Fatalf("the copy has no granted FoodSac ability")
	}
	if hasGrantedAbility(t, e, brenard, "FoodSac") {
		t.Fatal("FoodSac is offered on the effect source -- the grant leaked")
	}
	// Activate the granted ability: pay {2}, tap and sacrifice the copy. The
	// copy is a creature (Brenard adds Creature), so it must have been
	// controlled since the turn began; advance to the next turn first.
	driveToTurn(t, e, 2, 0)
	addMana(t, e, 0, "CC")
	e.priorityRound()
	gopt := grantedAbilityOption(t, e, copyID, "FoodSac")
	if gopt.Obj != copyID {
		t.Fatalf("granted FoodSac option Obj = %d, want the copy %d", gopt.Obj, copyID)
	}
	lifeBefore := e.G.Players[0].Life
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: e.Pending().Player, Choices: []int{gopt.Index}}); err != nil {
		t.Fatalf("submit FoodSac activation: %v", err)
	}
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Players[0].Life; got != lifeBefore+3 {
		t.Fatalf("life = %d, want %d (+3 from the Food sacrifice)", got, lifeBefore+3)
	}
	if z := e.G.Obj(copyID).Zone; z == state.ZBattlefield {
		t.Fatalf("the Food copy survived its own sacrifice (zone %s)", z)
	}
	noCopyPermanentModNote(t, e, "AddAbilities")
	replayCheck(t, e, cfg)
}

func hasGrantedAbility(t *testing.T, e *Engine, id state.ObjID, name string) bool {
	t.Helper()
	for _, ce := range e.active() {
		if ce.Source != id {
			continue
		}
		for _, nm := range ce.AddAbilities {
			if nm == name {
				return true
			}
		}
	}
	return false
}

// drainOptionalYes answers the "you may" election (option 0 = yes) and drains
// the stack so the copy mints.
func drainOptionalYes(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		if len(e.pendingTriggers) == 0 && len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		if d.Kind == decision.KPriority {
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				return
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		if d.Kind == decision.KTriggerOrder {
			answerTriggerOrders(t, e)
			continue
		}
		if len(d.Options) == 0 {
			return
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("submit %s: %v", d.Kind, err)
		}
	}
}

// TestArnaCopy pins the AttachedTo$ rider:
// a copy of a nontoken permanent attached to the attacker enters attached to
// that same attacker. The carrier is Arna, Skycaptain's real DBCopyPermanents
// body, but its own Defined$ filter is the real source filter
// (`Permanent.!token+AttachedTo TriggeredAttackerLKICopy`); the bare `Attached`
// and the `AttachedTo <ref>` predicates are now implemented (task attached1)
// and the real source filter is driven end to end by
// TestArnaRealSourceFilterReachesCopyRider in arna_source_filter_test.go. This
// test keeps its historical `Defined$ Valid Permanent` substitution so the
// rider's own behaviour stays pinned independently of the filter, and to keep
// the endpoint assertions below deterministic. (Arna's own Mode$ Attacks
// trigger is still unreachable for a separate reason: ValidCard$
// Creature.modified+YouCtrl needs the unimplemented `modified` CardProperty.)
// The endpoint is Arna's real `AttachedTo$ TriggeredAttackerLKICopy`.
func TestArnaCopy(t *testing.T) {
	reg := searchTestRegistry(t)
	arnaCard := lookup(t, reg, "Arna Kennerüd, Skycaptain")
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{arnaCard, lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter")},
		[]*cards.Card{})
	arna := moveByName(t, e, 0, "Arna Kennerüd, Skycaptain", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	equip := moveByName(t, e, 0, "Bonesplitter", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: equip, IDs: []state.ObjID{bear}})
	if e.G.Obj(equip).AttachedTo != bear {
		t.Fatalf("precondition failed: equipment %d is not attached to bear %d (AttachedTo=%d)",
			equip, bear, e.G.Obj(equip).AttachedTo)
	}
	equipCard := e.G.Obj(equip).Card
	base := resolveSourceFaceSA(t, e, arna, "DBCopyPermanents")

	// Build the reachable SA: Arna's real AttachedTo$ value, a resolvable
	// source filter (see the doc comment).
	mk := func() *cards.SA {
		sa := *base
		p := make(map[string]string, len(base.Params))
		for k, v := range base.Params {
			p[k] = v
		}
		p["Defined"] = "Valid Permanent"
		sa.Params = p
		return &sa
	}
	ctx := &effects.Ctx{Source: arna, Controller: 0, Remembered: []state.Target{{Obj: bear}}}
	effects.Resolve(e, ctx, mk())
	// Belt: nothing suspended (AttachedTo is a destination, not an ask).
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("AttachedTo$ posed an unexpected ask: %+v", d)
	}

	copyID := findTokenCopyOf(t, e, equipCard, equip)
	if e.G.Obj(copyID).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: the equipment copy is not on the battlefield (zone %s)", e.G.Obj(copyID).Zone)
	}
	if e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: the bearer left the battlefield (zone %s)", e.G.Obj(bear).Zone)
	}
	if got := e.G.Obj(copyID).AttachedTo; got != bear {
		t.Fatalf("the equipment copy AttachedTo = %d, want the attacking bearer %d", got, bear)
	}
	noCopyPermanentModNote(t, e, "AttachedTo")
	replayCheck(t, e, cfg)

	// A resolved-but-non-battlefield endpoint must NOT receive the copy: the
	// legality gate requires the endpoint on the battlefield.
	reg2 := searchTestRegistry(t)
	e2, cfg2 := corpusEngineCfg(t, reg2,
		[]*cards.Card{arnaCard, lookup(t, reg2, "Grizzly Bears"), lookup(t, reg2, "Bonesplitter")},
		[]*cards.Card{})
	arna2 := moveByName(t, e2, 0, "Arna Kennerüd, Skycaptain", state.ZBattlefield)
	bear2 := moveByName(t, e2, 0, "Grizzly Bears", state.ZBattlefield)
	dead := moveByName(t, e2, 0, "Bonesplitter", state.ZGraveyard)
	if e2.G.Obj(dead).Zone != state.ZGraveyard || e2.G.Obj(bear2).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: dead=%s bear=%s", e2.G.Obj(dead).Zone, e2.G.Obj(bear2).Zone)
	}
	sa2 := *resolveSourceFaceSA(t, e2, arna2, "DBCopyPermanents")
	p2 := make(map[string]string, len(sa2.Params))
	for k, v := range sa2.Params {
		p2[k] = v
	}
	p2["Defined"] = "Valid Creature"
	sa2.Params = p2
	ctx2 := &effects.Ctx{Source: arna2, Controller: 0, Remembered: []state.Target{{Obj: dead}}}
	effects.Resolve(e2, ctx2, &sa2)
	copy2 := findTokenCopyOf(t, e2, e2.G.Obj(bear2).Card, bear2)
	if e2.G.Obj(copy2).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: the creature copy is not on the battlefield (zone %s)", e2.G.Obj(copy2).Zone)
	}
	if got := e2.G.Obj(copy2).AttachedTo; got != 0 {
		t.Fatalf("the copy attached to a non-battlefield endpoint (AttachedTo=%d)", got)
	}
	replayCheck(t, e2, cfg2)
}

// noAskHost embeds the engine but refuses every mid-resolution ask, forcing
// the R-9 no-host stand-in while every state change still goes through the
// engine's real event path.
type noAskHost struct{ *Engine }

func (noAskHost) Ask(*decision.Decision) bool { return false }

// TestZndrspltNoHostCopy pins the R-9 no-host
// degradation for the Choices$ rider: a host that cannot ask copies the first
// eligible friend creature deterministically and records the no-host Note,
// never the resolving spell.
func TestZndrspltNoHostCopy(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Zndrsplt's Judgment")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Hill Giant")})
	z := moveByName(t, e, 0, "Zndrsplt's Judgment", state.ZHand)
	b1 := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	b2 := moveByName(t, e, 1, "Hill Giant", state.ZBattlefield)
	if e.G.Obj(b1).Zone != state.ZBattlefield || e.G.Obj(b2).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: the friend's creatures are not on the battlefield")
	}
	// The first eligible creature in the friend's deterministic zone order.
	want := e.G.Zone(state.ZBattlefield, 1)[0]
	if o := e.G.Obj(want); o == nil || o.Card == nil {
		t.Fatalf("precondition failed: no first battlefield permanent for the friend")
	}

	sa := resolveSourceFaceSA(t, e, z, "DBClone")
	ctx := &effects.Ctx{Source: z, Controller: 0,
		Remembered: []state.Target{{Player: 1, IsPlayer: true}}}
	effects.Resolve(noAskHost{e}, ctx, sa)

	// The no-host path never poses a decision; it copies the first eligible
	// friend creature under the friend.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("no-host run posed a KChoose: %+v", d)
	}
	copyID := findTokenCopyOf(t, e, e.G.Obj(want).Card, want)
	if e.G.Obj(copyID).Zone != state.ZBattlefield {
		t.Fatalf("the no-host fallback minted no copy (zone %s)", e.G.Obj(copyID).Zone)
	}
	if e.G.Obj(copyID).Controller != 1 {
		t.Fatalf("the no-host copy controller = %d, want the friend 1", e.G.Obj(copyID).Controller)
	}
	sawNote := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "has no engine host") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("the no-host fallback recorded no R-9 Note (notes: %v)", copyNotes(e))
	}
	replayCheck(t, e, cfg)
}

// TestHofriCopyPermanentGrants pins the real Hofri CopyPermanent mint: its
// AddSVars$/AddTriggers$ riders are attached to the Spirit copy, rather than
// being dropped as inert notes, AND the granted trigger works end to end --
// the Spirit's real departure (killed through the SBA path, not a raw
// MoveZone emit) fires the granted TrigLeavesBattlefield trigger and the
// exiled bearer returns to the graveyard.
func TestHofriCopyPermanentGrants(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := tokenRememberedBoard(t, reg, "Hofri Ghostforge", "Vampire Nighthawk")
	hofri, bearer := ids["Hofri Ghostforge"], ids["Vampire Nighthawk"]
	if e.G.Obj(hofri).Zone != state.ZBattlefield || e.G.Obj(bearer).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Hofri and bearer must be on battlefield")
	}

	// Hofri's dies trigger first exiles the creature, then its sub-ability
	// CopyPermanent uses that exiled card's LKI.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearer, From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	e.resolveTop()
	passUntilStackEmpty(t, e, 40)

	var spirit state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && id != bearer && spirit == 0 {
			spirit = id
		}
	}
	if spirit == 0 {
		t.Fatalf("precondition: Hofri did not create a Spirit copy; log %+v", e.L.Events)
	}
	if grants := triggerGrantsOn(e, spirit); len(grants) != 1 {
		t.Fatalf("Spirit carries %d granted triggers, want 1", len(grants))
	}
	if got, ok := e.GrantedSVar(spirit, "HofriTrigReturn"); !ok || got == "" {
		t.Fatalf("Spirit lacks granted HofriTrigReturn SVar: %q, %v", got, ok)
	}

	// The departure is the pin's other half. Kill the Spirit through the REAL
	// SBA path -- damage + checkStateBased's destroyLethalDamage death batch,
	// never a raw MoveZone emit, whose live pass cannot match a granted
	// trigger on an already-departed source (the look-back pass needs the
	// death batch's snapshot). The granted trigger queues, resolves, and
	// HofriTrigReturn returns the exiled bearer to its owner's graveyard.
	if z := e.G.Obj(bearer).Zone; z != state.ZExile {
		t.Fatalf("precondition: the bearer must sit in exile before the Spirit's departure (zone %v)", z)
	}
	e.emit(events.Event{Kind: events.Damage, Obj: spirit, Amount: 99})
	e.checkStateBased()
	if z := e.G.Obj(spirit).Zone; z == state.ZBattlefield {
		t.Fatalf("precondition: the SBA pass did not kill the damaged Spirit (zone %v)", z)
	}
	e.putTriggersOnStack()
	// The departure may queue several granted triggers for the copy (a
	// Hofri dies trigger can mint more than one Spirit copy), so order them
	// the way the ordinary drain does before resolving.
	answerTriggerOrders(t, e)
	e.resolveTop()
	passUntilStackEmpty(t, e, 40)
	if back := e.G.Obj(bearer); back == nil || back.Zone != state.ZGraveyard {
		t.Fatalf("the exiled bearer did not return to the graveyard when the Spirit left: zone %v, log %+v",
			e.G.Obj(bearer).Zone, e.L.Events)
	}

	// The whole round-trip replays byte-identically.
	replayCheck(t, e, cfg)
}
