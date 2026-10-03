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
// and ChangeZoneAll from any zone, a library search, DigUntil, a blink
// return, a sweepExileReturn, a copy-token mint's battlefield move, a token
// mint -- reaches Engine.emit as a MoveZone (or TokenCreate) event, so the
// rule is enforced there rather than in each mover:
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
//     shared ETB walker (entryETBChoice's "enchant" kind) when there is more
//     than one legal answer; the chosen (or only) bearer is attached right
//     after the entry folds (settleAuraEntry).
//
// An effect that DOES specify the bearer (ChangeZone's AttachedTo$ /
// AttachedToPlayer$, DigUntil's revealed-Aura bearer, a copy token's
// AttachedTo$) announces it through Host.ExpectAttachedEntry before it emits
// the move; the entry then skips the choice and the effect's own Attach
// follows, exactly as before. The CR 303.4g legality gate still applies to
// it: with nothing legal anywhere, the named bearer is not legal either.
//
// The bearer test is the enchant ability's legality, never targeting: an
// opponent's hexproof or shroud creature is a legal answer (CR 702.11b and
// 702.18a speak only of targets), while protection's "can't be enchanted"
// half (CR 702.16c) does exclude a candidate -- the same protection test the
// CR 704.5m SBA applies to an attached Aura.

// auraEntryState is the engine's transient record of one non-cast Aura entry
// between its gate and its fold: the default (or answered) bearer the fold
// attaches. Plain data, clone-copied, so a clone at the "enchant" decision
// boundary carries it.
type auraEntryState struct {
	obj      state.ObjID // the entering Aura; 0 when nothing is recorded
	bearer   state.ObjID // object bearer, or 0 for a player bearer
	player   state.PlayerID
	toPlayer bool
	answered bool // the controller's "enchant" answer, kept across the re-emit
}

// auraEntryCand is one legal answer to the CR 303.4f choice.
type auraEntryCand struct {
	obj      state.ObjID
	player   state.PlayerID
	toPlayer bool
}

// ExpectAttachedEntry implements effects.HostEmit: the effect about to move
// obj onto the battlefield attaches it itself, so its entry poses no CR 303.4f
// choice and records no default bearer. Cleared by obj's next folded move.
func (e *Engine) ExpectAttachedEntry(obj state.ObjID) { e.attachedEntry = obj }

// nonCastAuraEntrant returns the object of a MoveZone that puts a face-up
// Aura card onto the battlefield other than by resolving as an Aura spell
// (CR 303.4f's "by any means other than"): the object is neither on the stack
// (a resolving Aura spell attaches through its own cast-time target) nor
// already on the battlefield. A face-down entry is a vanilla 2/2 (CR 708.5),
// not an Aura. The face read is the entering face: a Transformed$ entry has
// already flipped before its move is emitted.
func (e *Engine) nonCastAuraEntrant(ev events.Event) *state.Object {
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
func auraEnchantSpec(o *state.Object) string {
	spec := "Permanent"
	if param, ok := o.Face().KeywordParam("Enchant"); ok && strings.TrimSpace(param) != "" {
		spec, _, _ = strings.Cut(param, ":")
	}
	return strings.TrimSpace(spec)
}

// auraEntryCandidates lists every legal answer to the CR 303.4f choice for
// the entering Aura o, in deterministic order: living players in seat order
// for Enchant:Player/Opponent, otherwise the objects in each named zone
// (the battlefield, or a graveyard-enchant Aura's inZone<X> zone) in seat
// then zone order. The controller is the player the Aura enters under (a
// card outside the battlefield is controlled by its owner, which is who
// events.Move puts it onto the battlefield under). Not targeting: hexproof
// and shroud are not consulted; protection is (CR 702.16c).
func (e *Engine) auraEntryCandidates(o *state.Object, out []auraEntryCand) []auraEntryCand {
	out = out[:0]
	you := o.Controller
	spec := auraEnchantSpec(o)
	if spec == "Player" || spec == "Opponent" {
		for _, p := range e.G.AliveFrom(0) {
			if spec == "Opponent" && p == you {
				continue
			}
			if !effects.MatchesPlayerSpecFrom(e.G, spec, p, you, o.ID) || e.playerProtectedFrom(p, o.ID) {
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
		for _, id := range e.G.Zone(zone, p) {
			if id == o.ID {
				continue
			}
			if !e.matchesSpecFrom(spec, id, you, o.ID) {
				continue
			}
			if zone == state.ZBattlefield && e.protectedFrom(id, o.ID) {
				continue
			}
			out = append(out, auraEntryCand{obj: id})
		}
	}
	return out
}

// auraEntryGate is Engine.emit's CR 303.4g pre-pass for a battlefield entry.
// It reports true when the entry must not happen at all: a non-cast Aura with
// no legal object or player to enchant stays where it is. The swallowed entry
// leaves one Note (Secret when the move was) and nothing else -- no MoveZone,
// no replacement, no trigger. Otherwise it records the default bearer the
// fold will attach (the first candidate), unless the entering effect named
// its own bearer (ExpectAttachedEntry) or the controller already answered.
func (e *Engine) auraEntryGate(ev events.Event) bool {
	o := e.nonCastAuraEntrant(ev)
	if o == nil {
		return false
	}
	cands := e.auraEntryCandidates(o, e.auraEntryCands)
	e.auraEntryCands = cands
	if len(cands) == 0 {
		if e.attachedEntry == ev.Obj {
			e.attachedEntry = 0
		}
		if e.auraEntry.obj == ev.Obj {
			e.auraEntry = auraEntryState{}
		}
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: o.Controller, Secret: ev.Secret,
			Text: "nothing the Aura can legally enchant: it stays in its zone (CR 303.4g)"})
		return true
	}
	if e.attachedEntry == ev.Obj {
		return false
	}
	if e.auraEntry.obj == ev.Obj && e.auraEntry.answered {
		return false
	}
	c := cands[0]
	e.auraEntry = auraEntryState{obj: ev.Obj, bearer: c.obj, player: c.player, toPlayer: c.toPlayer}
	return false
}

// auraEntryChoice is entryETBChoice's CR 303.4f arm: the "enchant" election
// a non-cast Aura entry poses when more than one legal answer exists and the
// entering effect named none. A single legal answer is not a choice (the
// strict-superset convention every as-enters ask keeps); auraEntryGate
// already recorded it.
func (e *Engine) auraEntryChoice(ev events.Event) ([]decision.Option, bool) {
	o := e.nonCastAuraEntrant(ev)
	if o == nil || e.attachedEntry == ev.Obj {
		return nil, false
	}
	cands := e.auraEntryCandidates(o, e.auraEntryCands)
	e.auraEntryCands = cands
	if len(cands) < 2 {
		return nil, false
	}
	you := o.Controller
	opts := make([]decision.Option, 0, len(cands))
	for _, c := range cands {
		if c.toPlayer {
			opts = append(opts, decision.Option{Index: len(opts), Kind: "enchant_player",
				Label: e.G.Players[c.player].Name, Player: c.player})
			continue
		}
		label := ""
		if b := e.G.Obj(c.obj); b != nil && b.Face() != nil {
			label = b.Face().Name
		}
		opts = append(opts, decision.Option{Index: len(opts), Kind: "enchant", Label: label, Obj: c.obj, Player: you})
	}
	return opts, true
}

// answerAuraEntry records the controller's "enchant" answer for the parked
// entry; the re-emitted move's gate keeps it and its fold attaches it.
func (e *Engine) answerAuraEntry(move events.Event, opt decision.Option) {
	st := auraEntryState{obj: move.Obj, answered: true}
	if opt.Kind == "enchant_player" {
		st.player, st.toPlayer = opt.Player, true
	} else {
		st.bearer = opt.Obj
	}
	e.auraEntry = st
}

// settleAuraEntry is Engine.emit's post-fold half: once a recorded Aura's
// entry has folded onto the battlefield it becomes attached to its chosen
// bearer (CR 303.4f: it enters attached). Any folded move of the object
// clears both transient records, so a replaced or redirected entry never
// leaves one behind for a later move.
func (e *Engine) settleAuraEntry(stored events.Event) {
	if stored.Kind != events.MoveZone {
		return
	}
	if e.attachedEntry == stored.Obj {
		e.attachedEntry = 0
	}
	st := e.auraEntry
	if st.obj == 0 || st.obj != stored.Obj {
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
	if b := e.G.Obj(st.bearer); b != nil {
		e.emit(events.Event{Kind: events.Attach, Obj: st.obj, IDs: []state.ObjID{st.bearer}})
	}
}
