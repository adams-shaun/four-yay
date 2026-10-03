package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// aura_entry.go is the ONE home of the non-cast Aura battlefield entry (CR
// 303.4f/g). Every route that puts a card onto the battlefield -- ChangeZone
// and ChangeZoneAll from any zone, a library search, Dig/DigUntil, a blink
// or UntilHostLeavesPlay return, a copy-token mint's battlefield move, a
// token mint -- reaches Engine.emit as a MoveZone (or TokenCreate) event, so
// the rule is enforced there rather than in each mover
// (rules/aura_entry_census_test.go pins the movers):
//
//   - CR 303.4g: "If an Aura is entering the battlefield and there is no
//     legal object or player for it to enchant, the Aura remains in its
//     current zone, unless that zone is the stack ... If the Aura is a token,
//     it isn't created." auraEntryGate swallows such an entry before any
//     replacement, fold or trigger sees it: no enters event, no ETB trigger,
//     no CR 704.5m trip to the graveyard.
//   - CR 303.4f: "If an Aura is entering the battlefield under a player's
//     control by any means other than by resolving as an Aura spell, and the
//     effect putting it onto the battlefield doesn't specify the object or
//     player the Aura will enchant, that player chooses what the Aura will
//     enchant as the Aura enters the battlefield. The player must choose a
//     legal object or player according to the Aura's enchant ability, but
//     it is not targeted." The choice is the first as-enters election of the
//     shared ETB walker (entryETBChoice) when more than one answer is legal;
//     the chosen (or only) bearer is attached right after the entry folds
//     (settleAuraEntry), so the Aura enters attached.
//
// An effect that DOES specify the bearer marks its entry with the named set
// (events.MarkNamedAttachEntry). The engine then poses no choice and
// attaches the FIRST named bearer the Aura can legally enchant; when none is
// legal the Aura stays in its zone -- CR 303.4g read through the effect's own
// restriction, which is Retether's reminder text ("Aura cards that can't
// enchant a creature on the battlefield remain in your graveyard") and the
// Boonweaver Giant / Academy Researchers rulings (the named creature has
// left: the Aura can't be put onto the battlefield). The effect finds the
// Aura already attached (or not on the battlefield) and does not attach it.
//
// The bearer test is the enchant ability's legality, never targeting: an
// opponent's hexproof or shroud creature is a legal answer (CR 702.11b and
// 702.18a speak only of targets), while protection's "can't be enchanted"
// half (CR 702.16c) does exclude a candidate -- the same protection test the
// CR 704.5m SBA applies to an attached Aura.
//
// Everything here is a free function over the engine: the logic needs the
// engine's compiled filters, keyword reads and emit, and adds no Engine
// method.

// auraEntryState is the engine's transient record of one non-cast Aura entry
// between its gate and its fold: the default, answered or effect-named
// bearer the fold attaches. multi marks an entry whose controller is asked
// (more than one legal answer, none named): its first as-enters answer is
// the "enchant" answer. Plain data, clone-copied, so a clone at the decision
// boundary carries it.
type auraEntryState struct {
	obj      state.ObjID // the entering Aura; 0 when nothing is recorded
	bearer   state.ObjID // object bearer, or 0 for a player bearer
	player   state.PlayerID
	toPlayer bool
	multi    bool
	answered bool
}

// auraEntryCand is one legal answer to the CR 303.4f choice.
type auraEntryCand struct {
	obj      state.ObjID
	player   state.PlayerID
	toPlayer bool
}

// nonCastAuraEntrant returns the object of a MoveZone that puts a face-up
// Aura card onto the battlefield other than by resolving as an Aura spell
// (CR 303.4f's "by any means other than"): the object is neither on the stack
// (a resolving Aura spell attaches through its own cast-time target) nor
// already on the battlefield. A face-down entry is a vanilla 2/2 (CR 708.5),
// not an Aura. The face read is the entering face: a Transformed$ entry has
// already flipped before its move is emitted.
func nonCastAuraEntrant(e *Engine, ev *events.Event) *state.Object {
	if ev.Kind != events.MoveZone || ev.To != state.ZBattlefield || events.IsFaceDownEntry(ev.Counter) {
		return nil
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone == state.ZBattlefield || o.Zone == state.ZStack {
		return nil
	}
	if !faceIsAura(o.Face()) {
		return nil
	}
	return o
}

// faceIsAura reports the Aura subtype on a printed face. Every Aura is an
// enchantment (CR 303.1), so the compiled-mask enchantment test screens the
// ordinary battlefield entry before any string compare.
func faceIsAura(f *cards.Face) bool {
	if f == nil || !f.IsEnchantment() {
		return false
	}
	for _, t := range f.Types {
		if t == "Aura" {
			return true
		}
	}
	return false
}

// auraEnchantSpec is the entering Aura's enchant restriction: the first
// colon field of its printed Enchant keyword ("Creature", "Creature.YouCtrl",
// "Player", "Creature.inZoneGraveyard"). An Aura with no Enchant keyword
// (none in the corpus) may enchant any permanent, the convention
// auraStillMatchesEnchant keeps for the SBA.
func auraEnchantSpec(f *cards.Face) string {
	spec := "Permanent"
	if param, ok := f.KeywordParam("Enchant"); ok && strings.TrimSpace(param) != "" {
		spec, _, _ = strings.Cut(param, ":")
	}
	return strings.TrimSpace(spec)
}

// auraEnchantCandidates lists every legal answer to the CR 303.4f choice, in
// deterministic order: living players in seat order for
// Enchant:Player/Opponent, otherwise the objects in the zone the spec names
// (the battlefield, or a graveyard-enchant Aura's inZone<X> zone) in seat
// then zone order. id is the entering object (0 for a token not yet minted:
// it is then neither a candidate itself nor a protection source) and you the
// player it enters under (a card outside the battlefield is controlled by
// its owner, which is who events.Move puts it onto the battlefield under).
// Not targeting: hexproof and shroud are not consulted; protection is (CR
// 702.16c). out is the scratch buffer the result reuses.
func auraEnchantCandidates(e *Engine, f *cards.Face, id state.ObjID, you state.PlayerID, out []auraEntryCand) []auraEntryCand {
	out = out[:0]
	spec := auraEnchantSpec(f)
	if spec == "Player" || spec == "Opponent" {
		for _, p := range e.G.AliveFrom(0) {
			if spec == "Opponent" && p == you {
				continue
			}
			if !effects.MatchesPlayerSpecFrom(e.G, spec, p, you, id) || e.playerProtectedFrom(p, id) {
				continue
			}
			out = append(out, auraEntryCand{player: p, toPlayer: true})
		}
		return out
	}
	zone := state.ZBattlefield
	for word := range strings.SplitSeq(spec, ".") {
		if z, has := strings.CutPrefix(word, "inZone"); has {
			if zn, known := effects.ParseZoneWord(z); known {
				zone = zn
			}
		}
	}
	for _, p := range e.G.AliveFrom(0) {
		for _, bid := range e.G.Zone(zone, p) {
			if bid == id || !e.matchesSpecFrom(spec, bid, you, id) {
				continue
			}
			if zone == state.ZBattlefield && e.protectedFrom(bid, id) {
				continue
			}
			out = append(out, auraEntryCand{obj: bid})
		}
	}
	return out
}

// auraEntryCandidates is the entering Aura's own candidate walk; a marked
// entry (events.NamedAttachEntry) keeps only its named bearers, in the
// effect's order.
func auraEntryCandidates(e *Engine, o *state.Object, ev *events.Event) ([]auraEntryCand, bool) {
	cands := auraEnchantCandidates(e, o.Face(), o.ID, o.Controller, e.auraEntryCands)
	e.auraEntryCands = cands
	named, ok := events.NamedAttachEntry(ev)
	if !ok {
		return cands, false
	}
	return filterAuraCandidates(cands, named), true
}

// filterAuraCandidates keeps, in named's order, the named bearers that are
// legal candidates. It rewrites cands' storage in place (the scratch
// buffer), so the result never allocates.
func filterAuraCandidates(cands []auraEntryCand, named []state.ObjID) []auraEntryCand {
	n := 0
	for _, id := range named {
		p, isPlayer := id.PlayerRef()
		for i := n; i < len(cands); i++ {
			c := cands[i]
			if (isPlayer && c.toPlayer && c.player == p) || (!isPlayer && !c.toPlayer && c.obj == id) {
				cands[n], cands[i] = cands[i], cands[n]
				n++
				break
			}
		}
	}
	return cands[:n]
}

// auraEntryGate is Engine.emit's CR 303.4g pre-pass for a battlefield entry.
// It reports true when the entry must not happen at all: a non-cast Aura with
// no legal object or player to enchant (or none among the bearers its effect
// names) stays where it is. The swallowed entry leaves one Note (Secret when
// the move was) and nothing else -- no MoveZone, no replacement, no trigger.
// Otherwise it records the bearer the fold attaches: the first legal named
// bearer, the controller's earlier answer, or the first candidate as the
// default an unasked entry keeps.
func auraEntryGate(e *Engine, ev *events.Event) bool {
	o := nonCastAuraEntrant(e, ev)
	if o == nil {
		return false
	}
	cands, named := auraEntryCandidates(e, o, ev)
	if len(cands) == 0 {
		if e.auraEntry.obj == ev.Obj {
			e.auraEntry = auraEntryState{}
		}
		text := "nothing the Aura can legally enchant: it stays in its zone (CR 303.4g)"
		if named {
			text = "nothing the effect names is something the Aura can legally enchant: it stays in its zone (CR 303.4g)"
		}
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: o.Controller, Secret: ev.Secret, Text: text})
		return true
	}
	if e.auraEntry.obj == ev.Obj && e.auraEntry.answered {
		return false
	}
	c := cands[0]
	e.auraEntry = auraEntryState{obj: ev.Obj, bearer: c.obj, player: c.player, toPlayer: c.toPlayer,
		multi: !named && len(cands) > 1, answered: named}
	return false
}

// auraTokenGate is auraEntryGate's TokenCreate half: CR 303.4g's "If the
// Aura is a token, it isn't created" when nothing on the board is a legal
// answer for the token's enchant ability. Every corpus Aura token is minted
// with an AttachedTo$ bearer (effects' auraTokenWithheld refuses the mint
// when that bearer is gone), so this is the class backstop for a mint whose
// script names no bearer at all.
func auraTokenGate(e *Engine, ev *events.Event) bool {
	def := e.G.Tokens[ev.Text]
	if def == nil || len(def.Faces) == 0 || !faceIsAura(def.Faces[0]) || int(ev.Player) >= len(e.G.Players) {
		return false
	}
	cands := auraEnchantCandidates(e, def.Faces[0], 0, ev.Player, e.auraEntryCands)
	e.auraEntryCands = cands
	if len(cands) > 0 {
		return false
	}
	e.emit(events.Event{Kind: events.Note, Player: ev.Player,
		Text: "nothing the Aura token " + ev.Text + " can legally enchant: it isn't created (CR 303.4g)"})
	return true
}

// auraEntryChoice is entryETBChoice's CR 303.4f arm: the "enchant" election
// a non-cast Aura entry poses when more than one legal answer exists and the
// entering effect named none. A single legal answer is not a choice (the
// strict-superset convention every as-enters ask keeps); auraEntryGate
// already recorded it. Object options carry the bearer in Obj; a player
// option carries Obj 0 and the seat in Player.
func auraEntryChoice(e *Engine, ev *events.Event) (etbChoice, bool) {
	o := nonCastAuraEntrant(e, ev)
	if o == nil {
		return etbChoice{}, false
	}
	cands, named := auraEntryCandidates(e, o, ev)
	if named || len(cands) < 2 {
		return etbChoice{}, false
	}
	you := o.Controller
	opts := make([]decision.Option, 0, len(cands))
	for _, c := range cands {
		if c.toPlayer {
			opts = append(opts, decision.Option{Index: len(opts), Kind: "enchant",
				Label: e.G.Players[c.player].Name, Player: c.player})
			continue
		}
		label := ""
		if b := e.G.Obj(c.obj); b != nil && b.Face() != nil {
			label = b.Face().Name
		}
		opts = append(opts, decision.Option{Index: len(opts), Kind: "enchant", Label: label, Obj: c.obj, Player: you})
	}
	return etbChoice{kind: "enchant", prompt: " what this Aura enchants", options: opts}, true
}

// promptText is the client prompt suffix of an as-enters election.
func (c etbChoice) promptText() string {
	if c.prompt != "" {
		return c.prompt
	}
	return etbChoicePrompt(c.kind)
}

// answerAuraEntry records the controller's "enchant" answer for the parked
// entry; the re-emitted move's gate keeps it and its fold attaches it. The
// answer is recognised by the entry record, not by an option kind: an
// entry with a multi record owes the enchant answer first, because the
// choice is its first as-enters election.
func answerAuraEntry(e *Engine, move *events.Event, opt *decision.Option) {
	st := &e.auraEntry
	if st.obj != move.Obj || !st.multi || st.answered {
		return
	}
	if opt.Obj != 0 {
		st.bearer, st.toPlayer = opt.Obj, false
	} else {
		st.bearer, st.player, st.toPlayer = 0, opt.Player, true
	}
	st.answered = true
}

// settleAuraEntry is Engine.emit's post-fold half: once a recorded Aura's
// entry has folded onto the battlefield it becomes attached to its bearer
// (CR 303.4f: it enters attached). Any folded move of the object clears the
// record, so a replaced or redirected entry never leaves one behind.
func settleAuraEntry(e *Engine, stored *events.Event) {
	st := e.auraEntry
	if stored.Kind != events.MoveZone || st.obj == 0 || st.obj != stored.Obj {
		return
	}
	e.auraEntry = auraEntryState{}
	o := e.G.Obj(st.obj)
	if stored.To != state.ZBattlefield || o == nil || o.Zone != state.ZBattlefield {
		return
	}
	if st.toPlayer {
		e.emit(events.Event{Kind: events.Attach, Obj: st.obj, Player: st.player, Text: "attach to player"})
		return
	}
	if e.G.Obj(st.bearer) != nil {
		e.emit(events.Event{Kind: events.Attach, Obj: st.obj, IDs: []state.ObjID{st.bearer}})
	}
}
