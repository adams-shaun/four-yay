package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Foretell family (CR 702.126): the {2} face-down hand exile special
// action, the later foretell-cost cast from exile, the FlagForetold
// provenance both markers carry, and the Count$Foretold branch head the
// cards' "if this spell was foretold" halves read. Pinned on the three real
// corpus carriers: Haunting Voyage (the branch), Starnheim Unleashed (the
// {X}{X}{W} cost and Count$Foretold.X.1) and Lupine Harbingers (the ETB
// CheckSVar$ gate over the persisted flag).
//
// Two measured boundaries this file documents rather than hides:
//
//   - Haunting Voyage's mid-resolution SP$ ChooseType now poses a real
//     KChoose type ask (task ct1); the drives below answer it (the graveyard
//     stocking is built so "Elf" is a real option). The pre-ct1 behaviour --
//     a deterministic fallback type read off the controller's objects, never
//     asked -- is pinned by the effects-side fallback tests, not here.
//   - handEngine's fixture runs no genesis TurnChange, so a turn-1 note
//     records 0 and the ETB delta is read relative to NotedNumber, never
//     against an absolute "turns since" number.

func optionByMode(t *testing.T, opts []decision.Option, mode string) decision.Option {
	t.Helper()
	for _, o := range opts {
		if o.Mode == mode {
			return o
		}
	}
	t.Fatalf("no option with mode %q in %+v", mode, opts)
	return decision.Option{}
}

func optionByLabel(opts []decision.Option, label string) int {
	for _, o := range opts {
		if o.Label == label {
			return o.Index
		}
	}
	return -1
}

func graveCreature(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	o.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, p, append(e.G.Zone(state.ZGraveyard, p), o.ID))
	return o.ID
}

// foretellIt foretells the hand card (paying {2} from the pool) and returns
// the exiled object.
func foretellIt(t *testing.T, e *Engine, id state.ObjID) *state.Object {
	t.Helper()
	opt := optionByMode(t, e.legalActions(0), "foretell")
	e.beginCast(0, opt)
	o := e.G.Obj(id)
	if o.Zone != state.ZExile || o.CastFlags&state.FlagForetold == 0 || !o.FaceDown || o.ExiledWith != 0 {
		t.Fatalf("foretell action did not exile face down with the flag: zone=%s flags=%#x faceDown=%v exiledWith=%d",
			o.Zone, o.CastFlags, o.FaceDown, o.ExiledWith)
	}
	if e.Pending() != nil {
		t.Fatalf("foretell action left a decision pending: %+v", e.Pending())
	}
	return o
}

// submitOption answers the pending priority decision with the option whose
// Mode/Label matches, the sanctioned path for a cast begun after a drive.
func submitOption(t *testing.T, e *Engine, mode, label string) {
	t.Helper()
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected a fresh priority decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if (mode != "" && o.Mode == mode) || (label != "" && o.Label == label) {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no matching option in %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit %q: %v", label, err)
	}
}

// finishCast resolves a committed cast that asked nothing further. Two
// committed shapes exist: a direct beginCast (no priority re-granted -- the
// object sits on the stack until resolveTop runs) and a cast submitted
// through a priority decision (the engine re-granted priority -- the stack
// drains through the ordinary passes). Both end with the spell resolved and
// nothing pending.
func finishCast(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	switch {
	case d == nil:
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
			t.Fatalf("cast object not on the stack: %+v", o)
		}
		e.resolveTop()
	case d.Kind == decision.KPriority:
		passUntilStackEmpty(t, e, 60)
	default:
		t.Fatalf("unexpected pending decision before resolution: %+v", d)
	}
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		// A trailing priority ask is the ordinary post-resolution state; any
		// other outstanding decision means the resolution wedged.
		t.Fatalf("resolution left a decision pending: %+v", d)
	}
}

// resolveOffStack drives resolveTop once over a committed cast and leaves any
// mid-resolution ask the suspension posed standing (the caller asserts its
// shape and answers it).
func resolveOffStack(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if d := e.Pending(); d != nil {
		t.Fatalf("unexpected pending decision before resolution: %+v", d)
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		t.Fatalf("cast object not on the stack: %+v", o)
	}
	e.resolveTop()
}

func driveToTurn3Main(t *testing.T, e *Engine) {
	t.Helper()
	e.priorityRound()
	driveToStep(t, e, 3, 0, state.StepMain1)
	if e.G.Turn != 3 || e.G.Active != 0 || e.G.Step != state.StepMain1 {
		t.Fatalf("could not reach turn 3 seat 0 main1 (at turn %d seat %d step %s)", e.G.Turn, e.G.Active, e.G.Step)
	}
}

func battlefieldNamed(t *testing.T, e *Engine, name string) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			n++
		}
	}
	return n
}

func TestForetellActionOfferedOnYourTurnAtTwoGeneric(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Haunting Voyage"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	opt := optionByMode(t, e.legalActions(0), "foretell")
	if opt.Obj != id || opt.Label != "Foretell Haunting Voyage" {
		t.Fatalf("foretell option mislabelled or misplaced: %+v", opt)
	}
	// CR 702.126a's timing gate is "during your turn": a seat that is not
	// the active player gets no offer.
	e.G.Active = 1
	if optionByLabel(e.legalActions(0), "Foretell Haunting Voyage") >= 0 {
		t.Error("foretell offered off-turn")
	}
	e.G.Active = 0
	// Unaffordable {2}: no offer.
	e.G.Players[0].Pool[state.MC] = 1
	if optionByLabel(e.legalActions(0), "Foretell Haunting Voyage") >= 0 {
		t.Error("foretell offered with an unpayable {2}")
	}
}

func TestHauntingVoyageForetellUnlocksTheReturnAllHalf(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Haunting Voyage"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	voyage := foretellIt(t, e, id)
	if got := e.G.Players[0].Pool[state.MC]; got != 0 {
		t.Fatalf("foretell action left %d generic in the pool, want 0", got)
	}
	// CR 702.126a: the cast comes only on a LATER turn. Same turn: no offer.
	if optionByLabel(e.legalActions(0), "Cast Haunting Voyage (foretold)") >= 0 {
		t.Error("foretell cast offered on the foretell turn itself")
	}
	// Graveyard stock: three 1/1 Elves and a Zombie -- two owned creature
	// subtypes, so the mid-resolution ChooseType ask (ct1) is real (two
	// options) and the foretold arm's ReturnAll (ChangeZoneAll) is
	// distinguishable from the ordinary arm's ChangeNum$ 2 cap by the
	// zombie's rest state. The ask is answered "Elf".
	var elves [3]state.ObjID
	for i := range elves {
		elves[i] = graveCreature(t, e, 0, "Name:Elf"+string(rune('A'+i))+"\nManaCost:no cost\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	}
	zombie := graveCreature(t, e, 0, "Name:Zombie\nManaCost:no cost\nTypes:Creature Zombie\nPT:1/1\nOracle:x\n")
	// Later turn, funded at the K: line's colon parameter {5}{B}{B}. The pool
	// is funded AFTER the drive: the turn changes it crosses clear pools.
	driveToTurn3Main(t, e)
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MB] = 5, 2
	submitOption(t, e, "foretell_cast", "Cast Haunting Voyage (foretold)")
	// Pass priority once: the spell resolves and suspends on the
	// mid-resolution creature-type ask (ct1); answer it "Elf", the same type
	// the pre-ct1 fallback recorded, so the movement assertions below are
	// unchanged.
	passUntilAsk(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosetype" ||
		len(d.Options) != len(effects.CreatureTypeWordList()) {
		t.Fatalf("expected the mid-resolution creature-type ask, got %+v", d)
	}
	idx := optionByLabel(d.Options, "Elf")
	if idx < 0 {
		t.Fatalf("no Elf option in the type ask: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The reveal-on-cast state: the face-down marker is gone the moment the
	// card moves off exile (CR 702.126c), here onto the stack.
	if voyage.FaceDown {
		t.Error("foretold card still face down after the cast move")
	}
	finishCast(t, e, id)
	for _, eid := range elves {
		if o := e.G.Obj(eid); o.Zone != state.ZBattlefield {
			t.Fatalf("foretold ReturnAll left %s in %s, want battlefield", o.Face().Name, o.Zone)
		}
	}
	if o := e.G.Obj(zombie); o.Zone != state.ZGraveyard {
		t.Fatalf("the unchosen type's card moved to %s", o.Zone)
	}
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		// CastFlags are deliberately 0 here: a stack->graveyard move resets
		// them (the engine's documented leave-the-stack reset), and a resolved
		// sorcery's foretell provenance is dead text.
		t.Fatalf("resolved voyage in %s, want graveyard", o.Zone)
	}
	// Unfunded: no offer (a second copy, still foretold, still in exile).
	e2 := handEngine(t, corpusAlternativeCard(t, "Haunting Voyage"))
	id2 := e2.G.Zone(state.ZHand, 0)[0]
	e2.G.Players[0].Pool[state.MC] = 2
	foretellIt(t, e2, id2)
	driveToTurn3Main(t, e2)
	if optionByLabel(e2.legalActions(0), "Cast Haunting Voyage (foretold)") >= 0 {
		t.Error("foretell cast offered at an unpayable {5}{B}{B}")
	}
}

func TestHauntingVoyageOrdinaryCastReturnsUpToTwo(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Haunting Voyage"))
	id := e.G.Zone(state.ZHand, 0)[0]
	var elves [3]state.ObjID
	for i := range elves {
		elves[i] = graveCreature(t, e, 0, "Name:Elf"+string(rune('A'+i))+"\nManaCost:no cost\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	}
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MB] = 4, 2
	zombie := graveCreature(t, e, 0, "Name:Zombie\nManaCost:no cost\nTypes:Creature Zombie\nPT:1/1\nOracle:x\n")
	castMode(t, e, id, "")
	// The false branch of Count$Foretold.1.0 (the un-foretold ordinary cast)
	// is the ChangeNum$ 2 pick over the three eligible Elves: the ask fires
	// (strictly more eligible than the cap) and the answered pair is exactly
	// what returns. Its first ask is the mid-resolution creature-type ask
	// (ct1); answer it "Elf".
	resolveOffStack(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosetype" ||
		len(d.Options) != len(effects.CreatureTypeWordList()) {
		t.Fatalf("expected the mid-resolution creature-type ask, got %+v", d)
	}
	tidx := optionByLabel(d.Options, "Elf")
	if tidx < 0 {
		t.Fatalf("no Elf option in the type ask: %+v", d.Options)
	}
	submitChoices(t, e, tidx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 {
		t.Fatalf("expected the return-up-to-two pick, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	returned := 0
	for _, eid := range elves {
		if e.G.Obj(eid).Zone == state.ZBattlefield {
			returned++
		}
	}
	if returned != 2 {
		t.Fatalf("ordinary cast returned %d Elves, want exactly the capped 2", returned)
	}
	if o := e.G.Obj(zombie); o.Zone != state.ZGraveyard {
		t.Fatalf("the unchosen type's card moved to %s", o.Zone)
	}
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("resolved voyage in %s, want graveyard", o.Zone)
	}
}

const angelTokenSrc = "Name:Angel Warrior\nManaCost:no cost\nColors:white\n" +
	"Types:Creature Angel Warrior\nPT:4/4\nK:Flying\nK:Vigilance\nOracle:x\n"

func TestStarnheimUnleashedForetoldCreatesXTokens(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Starnheim Unleashed"))
	e.G.Tokens = map[string]*cards.Card{"w_4_4_angel_warrior_flying_vigilance": card(t, angelTokenSrc)}
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	foretellIt(t, e, id)
	driveToTurn3Main(t, e)
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MW] = 3, 1
	submitOption(t, e, "foretell_cast", "Cast Starnheim Unleashed (foretold)")
	// The {X}{X}{W} foretell cost rides the ordinary X machinery: the cast
	// announces its X before payment.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 {
		t.Fatalf("expected the {X}{X}{W} X ask, got %+v", d)
	}
	idx := optionByLabel(d.Options, "X = 1")
	if idx < 0 {
		t.Fatalf("no X = 1 option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	finishCast(t, e, id)
	if got := battlefieldNamed(t, e, "Angel Warrior"); got != 1 {
		t.Fatalf("foretold X=1 cast created %d Angel tokens, want 1", got)
	}
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("resolved Starnheim in %s, want graveyard", o.Zone)
	}
}

func TestStarnheimUnleashedPlainCastCreatesOne(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Starnheim Unleashed"))
	e.G.Tokens = map[string]*cards.Card{"w_4_4_angel_warrior_flying_vigilance": card(t, angelTokenSrc)}
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MW] = 2, 2
	castMode(t, e, id, "")
	// The false branch of Count$Foretold.X.1 is the literal 1: exactly one
	// Angel token, and no X ask was ever posed.
	finishCast(t, e, id)
	if got := battlefieldNamed(t, e, "Angel Warrior"); got != 1 {
		t.Fatalf("plain cast created %d Angel tokens, want the false branch's 1", got)
	}
}

func TestLupineHarbingersWasForetoldEntersWithNotedTurns(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Lupine Harbingers"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	foretellIt(t, e, id)
	// The exile trigger notes the turn count onto the CARD (the
	// events.NotedNumber marker Count$NotedNumber reads). It lands when the
	// queued trigger resolves during the drive below, not at the action
	// itself -- the trigger queue drains with the priority flow.
	driveToTurn3Main(t, e)
	noted := e.G.Obj(id).NotedNumber
	noteSeen := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.NoteNumber && ev.Obj == id {
			noteSeen = true
		}
	}
	if !noteSeen {
		t.Fatal("the exile trigger never noted a turn count onto the card")
	}
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MG] = 4, 2
	submitOption(t, e, "foretell_cast", "Cast Lupine Harbingers (foretold)")
	finishCast(t, e, id)
	lup := e.G.Obj(id)
	if lup.Zone != state.ZBattlefield {
		t.Fatalf("foretold Lupine in %s, want battlefield", lup.Zone)
	}
	// X counters = turns begun since the foretell: the ETB's SVar$X/Minus.Y
	// reads YourTurns now minus the noted value.
	want := e.TurnsTaken(0) - noted
	if got := lup.Counter("P1P1"); got != want {
		t.Fatalf("foretold Lupine entered with %d P1P1, want %d (YourTurns %d - noted %d)",
			got, want, e.TurnsTaken(0), noted)
	}
	// The provenance guard: an UN-foretold cast never carries the flag, so
	// the ETB's CheckSVar$ WasForetold gate keeps the replacement off and no
	// counters are put.
	e2 := handEngine(t, corpusAlternativeCard(t, "Lupine Harbingers"))
	lupID := e2.G.Zone(state.ZHand, 0)[0]
	e2.G.Players[0].Pool[state.MC], e2.G.Players[0].Pool[state.MG] = 3, 1
	castMode(t, e2, lupID, "")
	finishCast(t, e2, lupID)
	lup2 := e2.G.Obj(lupID)
	if lup2.Zone != state.ZBattlefield || lup2.CastFlags&state.FlagForetold != 0 || lup2.Counter("P1P1") != 0 {
		t.Fatalf("un-foretold Lupine: zone=%s flags=%#x counters=%d, want battlefield/unflagged/0",
			lup2.Zone, lup2.CastFlags, lup2.Counter("P1P1"))
	}
}

func TestForetellOrdinaryExiledCardNeverGetsTheCastOffer(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Haunting Voyage"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MB] = 5, 2
	// A different copy exiled by some effect (never foretold): no cast
	// offer, ever -- the flag is the action's own provenance marker.
	other := e.G.AddObject(corpusAlternativeCard(t, "Haunting Voyage"), 0)
	other.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, append(e.G.Zone(state.ZExile, 0), other.ID))
	if optionByLabel(e.legalActions(0), "Cast Haunting Voyage (foretold)") >= 0 {
		t.Error("an ordinarily exiled copy got the foretell-cast offer")
	}
	// A foretold copy gets none on the SAME turn (foretellCastAvailable's
	// TurnChange scan), and the ordinary-cast path never reaches exile.
	foretellIt(t, e, id)
	if optionByLabel(e.legalActions(0), "Cast Haunting Voyage (foretold)") >= 0 {
		t.Error("foretell cast offered on the foretell turn itself")
	}
}
