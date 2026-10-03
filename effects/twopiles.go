package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("TwoPiles", effTwoPiles)
}

// effTwoPiles implements Forge's TwoPilesEffect, the Fact or Fiction pile
// split (task twopiles1). The card set comes from DefinedCards$ ("Remembered"
// — the cards the upstream PeekAndReveal put in the resolution's
// Remembered — or "Targeted", the Split the Spoils/Boneyard Parley exiled
// targets); the Separator$ player splits it into pile A (a KChoose subset
// pick, Min 0 — "piles can be empty", Split the Spoils' oracle) and pile B
// (the offered-order rest); the Chooser$ player (absent → Defined$) picks
// which pile is the chosen one; the ChosenPile$ SVar body then runs with the
// chosen pile as the sub's Remembered set and UnchosenPile$ with the other —
// both are DB$ ChangeZone | Defined$ Remembered bodies, so the existing
// movement path emits the MoveZone events and nothing here touches state.
//
// Two asks across one suspension: the primitive suspends at the split ask
// and again at the pile pick, the answers riding the decisions'
// ResumeRemembered (the full card set, so the re-entry re-derives pile B)
// and ResumeChoices (pile A, so the pick re-entry carries the split even
// when the split's own answer path recorded it on a Ctx this resume rebuilt)
// — the effDig re-entry shape with the fx42 consume-and-clear at walk top.
//
// The R-9 no-host stand-in: pile A is the FIRST card of the set and the
// chooser takes pile A — silent, byte-identical run to run, straight from
// the ordered card list. Botpolicy's KChoose default arm answers the split
// with option 0 (pile A = the first card) and the pick with option 0 (pile
// A), the same deterministic answers through the ordinary decision channel.
//
// Scoped out (each ONE loud Note naming the unread parameter, nothing
// moves): DefinedPiles$, Zone$, FaceDown$, LeftRightPile$, a DefinedCards$
// value other than Remembered/Targeted, a ChosenPile$/UnchosenPile$ value
// that is not an SVar name of the resolving face (the TurnFaceUp/ToHand/
// ToGrave one-offs), and a Separator$/Chooser$ selector this resolver
// cannot evaluate. AILogic$/RememberChosen$/KeepRemembered$ and the
// description params are unread display/AI metadata.
func effTwoPiles(h Host, c *Ctx, sa *cards.SA) {
	// fx42 scoping: consume the answered fields BEFORE anything else, so a
	// nested TwoPiles below this walk poses its own asks.
	splitAns := c.TwoPiles
	splitDone := c.TwoPilesDone
	pickAns := c.TwoPilesPick
	pickDone := c.TwoPilesPickDone
	c.TwoPiles, c.TwoPilesDone, c.TwoPilesPick, c.TwoPilesPickDone = nil, false, "", false

	for _, p := range []struct{ name, value string }{
		{"DefinedPiles", sa.Params["DefinedPiles"]},
		{"LeftRightPile", sa.Params["LeftRightPile"]},
	} {
		if v := strings.TrimSpace(p.value); v != "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented TwoPiles shape: " + p.name + "$ " + v})
			return
		}
	}
	spec := strings.TrimSpace(sa.ParamStr(cards.PKDefinedCards))
	if spec == "" {
		if v := strings.TrimSpace(sa.ParamStr(cards.PKZone)); v != "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented TwoPiles shape: Zone$ " + v})
			return
		}
	}
	if spec != "Remembered" && spec != "Targeted" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented TwoPiles shape: DefinedCards$ " + spec})
		return
	}
	if v := strings.TrimSpace(sa.Params["FaceDown"]); v != "" && !strings.EqualFold(v, "One") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented TwoPiles shape: FaceDown$ " + v})
		return
	}
	// The pile bodies must be SVar names on the resolving face: the core
	// carriers all point at DB$ ChangeZone/Play bodies, while the exotic
	// one-offs (TurnFaceUp, ToHand, ToGrave) name actions no SVar defines.
	// An unresolvable value emits its own Note and returns "".
	pileBodyFor := func(value, label string) string {
		v := strings.TrimSpace(value)
		if v == "" {
			return ""
		}
		if cards.ResolveSVar(c.SVars, v) == nil {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented TwoPiles shape: " + label + "$ " + v})
			return ""
		}
		return v
	}

	// The card set, in the deterministic Remembered/Targets order.
	set := objectsOf(c.Remembered)
	if spec == "Targeted" {
		set = objectsOf(c.Targets)
	}
	ids := make([]state.ObjID, 0, len(set))
	g := h.Game()
	for _, t := range set {
		if o := g.Obj(t.Obj); o != nil {
			ids = append(ids, o.ID)
		}
	}
	if len(ids) == 0 {
		// Forge returns when the card list is empty: no ask, no movement,
		// the chain continues (the fail-to-find shape must not wedge).
		return
	}

	// The players. Separator$ (absent → the Chooser$ player) splits;
	// Chooser$ (absent → Defined$) picks.
	sepSpec := strings.TrimSpace(sa.Params["Separator"])
	chooseSpec := strings.TrimSpace(sa.ParamStr(cards.PKChooser))
	if chooseSpec == "" {
		chooseSpec = strings.TrimSpace(sa.ParamStr(cards.PKDefined))
	}
	if sepSpec == "" {
		sepSpec = chooseSpec
	}
	sep, ok := twoPilesPlayer(h, c, sepSpec)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented TwoPiles shape: Separator$ " + sepSpec + " names no player this engine can resolve"})
		return
	}
	chooser, ok := twoPilesPlayer(h, c, chooseSpec)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented TwoPiles shape: Chooser$ " + chooseSpec + " names no player this engine can resolve"})
		return
	}

	// The pile bodies: ChosenPile$ must name an SVar of the resolving face
	// (Forge's core carriers all do); UnchosenPile$ is optional — Brilliant
	// Ultimatum leaves the unchosen pile in exile. An unresolvable value
	// emitted its own Note inside pileBodyFor and stops the walk.
	chosenBody := pileBodyFor(sa.Params["ChosenPile"], "ChosenPile")
	if chosenBody == "" {
		return
	}
	unchosenBody := pileBodyFor(sa.Params["UnchosenPile"], "UnchosenPile")
	if strings.TrimSpace(sa.Params["UnchosenPile"]) != "" && unchosenBody == "" {
		return
	}

	runPile := func(name string, pile []state.ObjID) {
		if name == "" || len(pile) == 0 {
			return
		}
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented TwoPiles shape: " + name + " does not resolve"})
			return
		}
		// A FRESH Ctx carrying ONLY the pile (the Remembered aliasing
		// hazard cardflow.go documents): the pile body's ChangeZone reads
		// exactly these cards and its riders land on the fresh slice, never
		// on the outer resolution's Remembered. Child keeps the script
		// table (and the layer tables Resolve rebinds on entry anyway).
		k := c.Child(c.Source, c.Controller)
		sc := &k
		sc.Remembered = make([]state.Target, 0, len(pile))
		for _, id := range pile {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: id})
		}
		Resolve(h, sc, noShuffleBody(sub))
	}

	// Stage 3: the pick was answered (or taken by the stand-in). Run the
	// bodies.
	if pickDone {
		var pileA, pileB []state.ObjID
		pileA, pileB = splitSet(ids, splitAns)
		if pickAns == "b" {
			runPile(chosenBody, pileB)
			runPile(unchosenBody, pileA)
		} else {
			runPile(chosenBody, pileA)
			runPile(unchosenBody, pileB)
		}
		return
	}

	// Stage 2: the split was answered — pile A rides splitAns (from the
	// split ask's own answer), pile B is the offered-order rest; pose the
	// pile pick. The split answer may also arrive here through the pick
	// resume arm's rp.choices when stage 2 ran on a no-host split.
	if splitDone {
		var pileA, pileB []state.ObjID
		pileA, pileB = splitSet(ids, splitAns)
		if twoPilesPosePick(h, c, sa, chooser, pileA, ids) {
			return
		}
		// No host (or the stand-in): the chooser takes pile A.
		runPile(chosenBody, pileA)
		runPile(unchosenBody, pileB)
		return
	}

	// Stage 1: pose the split ask (skipped for a one-card set — a decision
	// nobody could answer differently is never posed; pile A is that card).
	if len(ids) > 1 {
		d := &decision.Decision{Player: sep, Kind: decision.KChoose,
			Min: 0, Max: len(ids), Source: c.Source,
			ResumeKind: "twopiles_split", ResumeSA: sa,
			ResumeRemembered: twoPilesTargets(ids),
			Prompt:           "Separate these cards into two piles: pick the cards of the first pile (the rest form the second)"}
		for _, id := range ids {
			name := "a card"
			if o := g.Obj(id); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "twopiles", Label: name, Obj: id, Player: sep})
		}
		if Ask(h, d) == AskAsked {
			return
		}
		// R-9 no-host stand-in: pile A is the first card of the set, silent
		// and byte-identical run to run.
		pileA := []state.ObjID{ids[0]}
		if twoPilesPosePick(h, c, sa, chooser, pileA, ids) {
			return
		}
		runPile(chosenBody, pileA)
		runPile(unchosenBody, restOf(ids, idSet(pileA)))
		return
	}
	// One-card set: the split ask is skipped, the chooser is still asked.
	if twoPilesPosePick(h, c, sa, chooser, ids[:1], ids) {
		return
	}
	runPile(chosenBody, ids[:1])
	runPile(unchosenBody, nil)
}

// noShuffleBody is the pile body a pile movement actually resolves: the SVar
// SA with NoShuffle$ True forced on, unless the script already spoke about
// the shuffle itself.
//
// Every core carrier's pile bodies are library-origin movements (Fact or
// Fiction's DBHand/DBGrave, Sphinx of Uthuun, Steam Augury, Epiphany at the
// Drownyard, Intrude on the Mind, Unesh, Jace Architect of Thought's
// DBLibraryBottom), and the shared Origin$ Library tail
// (moveDefinedLibraryObjects / the ChangeZoneAll equivalent) fires the
// default CR 701.23d *search* shuffle on the owner's library unless the SA
// opts out. A pile split is not a search: not one of these oracle texts says
// to shuffle, so an unsuppressed tail would shuffle the caster's remaining
// library once per pile body — measured as two extra Secret Shuffle events
// on the real Fact or Fiction flow before this suppression.
//
// The opt-out is only forced when the body itself is silent: Phyrexian
// Portal's ChosenPile$ DBHand carries an explicit Shuffle$ True ("then
// shuffle the rest of that pile into your library" is in its oracle text),
// and a script that named either flag keeps what it named. Suppressing the
// shuffle cannot disturb placement: placeLibraryObjects (the UnchosenPile$
// DBLibraryBottom / LibraryPosition$ -1 shape) runs after the shuffle point,
// so the bottom-of-library pile still lands in order.
func noShuffleBody(sub *cards.SA) *cards.SA {
	if sub == nil {
		return nil
	}
	if strings.TrimSpace(sub.ParamStr(cards.PKShuffle)) != "" || strings.TrimSpace(sub.ParamStr(cards.PKNoShuffle)) != "" {
		return sub
	}
	cp := *sub
	cp.Params = make(map[string]string, len(sub.Params)+1)
	for k, v := range sub.Params {
		cp.Params[k] = v
	}
	cp.Params["NoShuffle"] = "True"
	return &cp
}

// twoPilesPosePick poses the two-option pile pick (KChoose Min 1 Max 1,
// options "pile-a"/"pile-b" — no new decision.Kind). ResumeRemembered rides
// the full card set (the re-entry re-derives pile B), ResumeChoices rides
// pile A (the pick resume arm hands it back as Ctx.TwoPiles, covering both
// the split-answered and the no-host-split paths with one channel). True
// when the ask suspended.
func twoPilesPosePick(h Host, c *Ctx, sa *cards.SA, chooser state.PlayerID, pileA, ids []state.ObjID) bool {
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
		Min: 1, Max: 1, Source: c.Source,
		ResumeKind:       "twopiles_pick",
		ResumeSA:         sa,
		ResumeRemembered: twoPilesTargets(ids),
		ResumeChoices:    twoPilesTargets(pileA),
		Prompt:           "Choose a pile"}
	d.Options = append(d.Options,
		decision.Option{Index: 0, Kind: "pile-a", Label: "First pile", Player: chooser},
		decision.Option{Index: 1, Kind: "pile-b", Label: "Second pile", Player: chooser})
	return Ask(h, d) == AskAsked
}

// twoPilesPlayer resolves one Separator$/Chooser$ selector to a single
// seat. "You" is the controller; everything else goes through the shared
// knownDefinedTargets resolver (Opponent → the first alive opponent in seat
// order, ChosenPlayer/TriggeredPlayer when bound) and takes the first player
// entry, deterministic in that order. An unresolvable spec reports false and
// the caller goes loud.
func twoPilesPlayer(h Host, c *Ctx, spec string) (state.PlayerID, bool) {
	if spec == "You" {
		return c.Controller, true
	}
	ts, ok := knownDefinedTargets(h, c, spec)
	if !ok {
		return 0, false
	}
	for _, t := range ts {
		if t.IsPlayer && int(t.Player) < len(h.Game().Players) && !h.Game().Players[t.Player].Lost {
			return t.Player, true
		}
	}
	return 0, false
}

// splitSet splits the offered-order card set on the answered pile-A ids:
// pile A keeps the ANSWER order, pile B the offered order minus A.
func splitSet(ids, pileA []state.ObjID) (a, b []state.ObjID) {
	a = make([]state.ObjID, 0, len(pileA))
	for _, id := range pileA {
		a = append(a, id)
	}
	return a, restOf(ids, idSet(pileA))
}

// idSet is the membership set a pile split reads.
func idSet(ids []state.ObjID) map[state.ObjID]bool {
	in := make(map[state.ObjID]bool, len(ids))
	for _, id := range ids {
		in[id] = true
	}
	return in
}

// restOf is the offered-order complement of the picked set. A stray answer
// id that is no longer in the card set is simply dropped.
func restOf(ids []state.ObjID, in map[state.ObjID]bool) []state.ObjID {
	out := make([]state.ObjID, 0, len(ids))
	for _, id := range ids {
		if !in[id] {
			out = append(out, id)
		}
	}
	return out
}

// twoPilesTargets is the object-Target copy a decision's ResumeRemembered /
// ResumeChoices ride.
func twoPilesTargets(ids []state.ObjID) []state.Target {
	out := make([]state.Target, 0, len(ids))
	for _, id := range ids {
		out = append(out, state.Target{Obj: id})
	}
	return out
}
