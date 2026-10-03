package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The chosen-copy CreateToken replacement (DB$ ReplaceToken |
// TokenScript$ Chosen | ValidChoices$ <spec>, the Esix / Moonlit Meditation /
// Mirrormind Crown class, task esix-chosen-token-copy): the controller's
// election is a real mid-replacement KChoose parked on the replacement
// pipeline; on accept every gated mint becomes a copy of the chosen creature
// (the DB$ CopyPermanent mint shape), on decline the event stands verbatim.

// drainToChooseOrEmpty passes priority until either a KChoose is pending
// (the parked election) or the stack is empty with an ordinary priority
// decision outstanding; it fails on any other decision kind.
func drainToChooseOrEmpty(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KChoose {
			return d
		}
		if d.Kind == decision.KReplacement {
			// A CR 616.1 order ask parked on the queue: option 0 is the
			// deterministic scan order the pre-choice engine composed in.
			submitChoices(t, e, 0)
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %s decision while draining: %+v", d.Kind, d)
		}
		if len(e.G.Stack) == 0 {
			return d
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		submitChoices(t, e, idx)
	}
	t.Fatal("drain did not settle within 40 passes")
	return nil
}

// countCreatureTokenCopiesNamed counts seat p's battlefield TOKEN copies of
// the named card (IsToken && IsCopy), never the original permanent itself.
func countCreatureTokenCopiesNamed(t *testing.T, e *Engine, p state.PlayerID, name string) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.IsCopy && o.Face() != nil && o.Face().Name == name {
			n++
		}
	}
	return n
}

// TestEsixChosenTokenCopyElectionPins is the end-to-end pin on the real
// corpus card: Esix's first token creation of the turn parks on the
// Optional election -- one decline option first (botpolicy's KChoose default
// arm takes option 0, so a bot never replaces), then every creature other
// than Esix itself (the maker artifact is not a creature and must not be
// offered). The decline answer stands the original token verbatim.
func TestEsixChosenTokenCopyElectionPins(t *testing.T) {
	t.Parallel()
	esix := tokenReplCorpusCard(t, "Esix, Fractal Bloom")
	bears := tokenReplCorpusCard(t, "Grizzly Bears")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 61, esix, bears, maker)
	moveSeededCard(t, e, 0, esix, state.ZBattlefield)
	bearsID := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	if o := e.G.Obj(bearsID); o == nil || o.Zone != state.ZBattlefield || !o.Face().IsCreature() {
		t.Fatalf("precondition: Grizzly Bears not a battlefield creature: %+v", o)
	}
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	d := drainToChooseOrEmpty(t, e)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no parked election: the ReplaceToken chosen arm did not ask (%+v)", d)
	}
	if d.Player != 0 || d.Source != esixID(t, e) {
		t.Fatalf("election player/source = %d/%d, want 0/%d", d.Player, d.Source, esixID(t, e))
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "decline" ||
		d.Options[1].Kind != "creature" || d.Options[1].Obj != bearsID {
		t.Fatalf("election options = %+v, want [decline, creature %d]", d.Options, bearsID)
	}
	// The decline: the original squirrel stands, nothing was copied.
	submitChoices(t, e, d.Options[0].Index)
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 1 {
		t.Fatalf("declined election made %d Squirrel Tokens, want 1", got)
	}
	if got := countCreatureTokenCopiesNamed(t, e, 0, "Grizzly Bears"); got != 0 {
		t.Fatalf("declined election still copied the bear: %d copies", got)
	}
	replayCheck(t, e, cfg)
}

// esixID looks the battlefield Esix up (the election's Source).
func esixID(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && !o.IsToken && o.Face() != nil &&
			o.Face().Name == "Esix, Fractal Bloom" {
			return id
		}
	}
	t.Fatal("Esix not on seat 0's battlefield")
	return 0
}

// TestEsixChosenTokenCopyAccept pins the accept arm: the answered election
// mints one token copy of the chosen creature (the CopyPermanent mint shape,
// IsToken+IsCopy with the copied card's printed characteristics) instead of
// the scripted token, and Esix's own first-time gate (the corpus SVar counts
// the tokens that entered the battlefield this turn) then holds the SECOND
// creation of the same turn verbatim -- no second election, a plain squirrel.
func TestEsixChosenTokenCopyAccept(t *testing.T) {
	t.Parallel()
	esix := tokenReplCorpusCard(t, "Esix, Fractal Bloom")
	bears := tokenReplCorpusCard(t, "Grizzly Bears")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 67, esix, bears, maker)
	moveSeededCard(t, e, 0, esix, state.ZBattlefield)
	moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	d := drainToChooseOrEmpty(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("no parked election with two options: %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index) // accept: copy the bear
	copies := countCreatureTokenCopiesNamed(t, e, 0, "Grizzly Bears")
	if copies != 1 {
		t.Fatalf("accepted election minted %d bear copies, want 1", copies)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 0 {
		t.Fatalf("accepted election still made %d Squirrel Tokens", got)
	}
	var copy *state.Object
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.IsCopy &&
			o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			copy = o
		}
	}
	if copy == nil {
		t.Fatal("no bear-copy object on the battlefield")
	}
	f := copy.Face()
	if f.Power() != 2 || f.Toughness() != 2 {
		t.Fatalf("bear copy P/T = %d/%d, want 2/2", f.Power(), f.Toughness())
	}
	if !slices.Contains(f.Types, "Creature") || !slices.Contains(f.Types, "Bear") {
		t.Fatalf("bear copy types = %v, want Creature Bear", f.Types)
	}
	if copy.Zone != state.ZBattlefield || copy.IsToken != true {
		t.Fatalf("bear copy zone/token = %s/%v", copy.Zone, copy.IsToken)
	}

	// Esix's first-time gate: the copy is a token that entered this turn, so
	// the corpus SVar (Count$ThisTurnEntered_..._Card.tokenCreated+YouOwn,
	// SVarCompare$ EQ0) no longer reads zero and the second creation of the
	// turn resolves verbatim -- no second election, a plain squirrel. The
	// maker untaps through one real logged Untap so the same turn can pay
	// for the activation again.
	e.emit(events.Event{Kind: events.Untap, Obj: m})
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	d2 := drainToChooseOrEmpty(t, e)
	if d2 == nil || d2.Kind != decision.KPriority || len(e.G.Stack) != 0 {
		t.Fatalf("second creation posed %+v (stack %d); the first-time gate must hold it verbatim", d2, len(e.G.Stack))
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 1 {
		t.Fatalf("second creation made %d Squirrel Tokens, want 1", got)
	}
	if got := countCreatureTokenCopiesNamed(t, e, 0, "Grizzly Bears"); got != 1 {
		t.Fatalf("second creation copied again: %d bear copies, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestMoonlitChosenTokenCopy pins the non-Creature.Other spec shape:
// Moonlit Meditation's ValidChoices$ Card.EnchantedBy offers exactly the
// enchanted permanent (the attachedBy predicate -- the Crown's EquippedBy is
// the same predicate body), and the accept mints a copy of it.
func TestMoonlitChosenTokenCopy(t *testing.T) {
	t.Parallel()
	moonlit := tokenReplCorpusCard(t, "Moonlit Meditation")
	bears := tokenReplCorpusCard(t, "Grizzly Bears")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 71, moonlit, bears, maker)
	bearsID := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	// The Aura enters attached to the bear: one real logged Attach event.
	moonlitID := stageAuraCard(t, e, 0, moonlit, bearsID)
	if a := e.G.Obj(moonlitID); a == nil || a.AttachedTo != bearsID {
		t.Fatalf("precondition: Moonlit not attached to the bear (%+v)", a)
	}

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	d := drainToChooseOrEmpty(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
		d.Options[0].Kind != "decline" || d.Options[1].Obj != bearsID {
		t.Fatalf("election options = %+v, want [decline, the enchanted bear %d]", d.Options, bearsID)
	}
	submitChoices(t, e, d.Options[1].Index)
	if got := countCreatureTokenCopiesNamed(t, e, 0, "Grizzly Bears"); got != 1 {
		t.Fatalf("accepted election minted %d bear copies, want 1", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 0 {
		t.Fatalf("accepted election still made %d Squirrel Tokens", got)
	}
	replayCheck(t, e, cfg)
}

// TestEsixChosenCopyThenDivineVisitationComposes pins the CR 616.1e re-check
// of a COPY plan mint: after Esix's accepted election rewrites the squirrel
// mint into a token copy of the chosen creature (CR 706.2 -- the would-be
// token IS the copied creature's printed face), a later CreateToken
// replacement in deterministic scan order (Divine Visitation, whose
// ValidToken$ is `Creature.YouCtrl`) must still match that copy mint and
// replace it with its own Angel. Before the fix the recheck built the mint's
// event from the empty `TokenScript$ Chosen` script, so the ValidToken$
// snapshot failed closed and the bear copy stood -- this test asserts the
// composition, not either replacement alone.
func TestEsixChosenCopyThenDivineVisitationComposes(t *testing.T) {
	t.Parallel()
	esix := tokenReplCorpusCard(t, "Esix, Fractal Bloom")
	visitation := tokenReplCorpusCard(t, "Divine Visitation")
	bears := tokenReplCorpusCard(t, "Grizzly Bears")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 73, esix, visitation, bears, maker)
	moveSeededCard(t, e, 0, esix, state.ZBattlefield)
	visID := moveSeededCard(t, e, 0, visitation, state.ZBattlefield)
	bearsID := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	// Precondition: BOTH replacements are live battlefield permanents, so a
	// valid run really composes the two and a vacuous setup fails loudly.
	if o := e.G.Obj(visID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Divine Visitation not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(bearsID); o == nil || o.Zone != state.ZBattlefield || !o.Face().IsCreature() {
		t.Fatalf("precondition: Grizzly Bears not a battlefield creature: %+v", o)
	}

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	d := drainToChooseOrEmpty(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
		d.Options[0].Kind != "decline" || d.Options[1].Obj != bearsID {
		t.Fatalf("no parked Esix election over the bear: %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index) // accept: copy the bear

	// Divine Visitation comes later in scan order, re-checks the copy mint's
	// ValidToken$ snapshot (a 2/2 Bear creature token under your control) and
	// replaces it with its 4/4 Angel. The bear copy must NOT stand.
	if got := countTokensNamedOnSeat(t, e, 0, "Angel Token"); got != 1 {
		t.Fatalf("composed mint made %d Angel Tokens, want 1 (the later ValidToken$ never matched the copy mint)", got)
	}
	if got := countCreatureTokenCopiesNamed(t, e, 0, "Grizzly Bears"); got != 0 {
		t.Fatalf("composed mint still stood the %d bear copy(ies); Divine Visitation must have replaced it", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 0 {
		t.Fatalf("composed mint made %d Squirrel Tokens, want 0", got)
	}
	replayCheck(t, e, cfg)
}
