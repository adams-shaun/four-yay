package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Excess-damage recipient history (CR 120.10, wasDealtExcessDamageThisTurn).
//
// A permanent was dealt excess damage when the sources of one simultaneous
// damage batch TOGETHER dealt it more than its threshold measured before the
// batch: for a creature, lethal damage (toughness minus damage already marked,
// and any damage beyond 1 when a deathtouch source hit it); for a
// planeswalker, its loyalty; for a battle, its defense. A permanent with
// several of those card types takes the GREATEST per-type excess (CR 120.4a's
// last sentence), so it was dealt excess iff the batch total exceeds the
// SMALLEST applicable threshold.
//
// The tally is engine memory scoped to the open damage batch; the fact itself
// is published as one ExcessDamage event per recipient when the outermost
// batch closes, AFTER every Damage event of the batch, so the batch's Damage
// events stay contiguous (only their own per-hit DamageProvenance sits between
// them) and a replay re-derives the same events from the same emits.

// Type bits carried in ExcessDamage.Amount / state.ExcessDamageVictim.Type.
const (
	excessTypeCreature     int32 = 1
	excessTypePlaneswalker int32 = 2
	excessTypeBattle       int32 = 4
)

// excessBatchEntry is one recipient's tally inside the open damage batch.
type excessBatchEntry struct {
	obj        state.ObjID
	controller state.PlayerID
	types      int32
	// Pre-batch thresholds, read at the recipient's first hit in the batch.
	creatureLethal int32
	loyalty        int32
	defense        int32
	deathtouch     bool
	total          int32
}

func (en *excessBatchEntry) excessThreshold() int32 {
	th := int32(-1)
	pick := func(v int32) {
		if v < 0 {
			v = 0
		}
		if th < 0 || v < th {
			th = v
		}
	}
	if en.types&excessTypeCreature != 0 {
		l := en.creatureLethal
		if en.deathtouch && l > 1 {
			l = 1
		}
		pick(l)
	}
	if en.types&excessTypePlaneswalker != 0 {
		pick(en.loyalty)
	}
	if en.types&excessTypeBattle != 0 {
		pick(en.defense)
	}
	return th
}

// excessTally is the open damage batch's per-recipient tally (a narrow
// receiver: it holds only its own entries, never the Engine).
type excessTally struct {
	entries []excessBatchEntry
}

// excessHost is what the tally reads from the board before a hit folds.
type excessHost interface {
	damageSourceCharacteristics
	Game() *state.Game
	IsCreature(state.ObjID) bool
	Toughness(state.ObjID) int32
}

func (t *excessTally) entry(id state.ObjID) *excessBatchEntry {
	for i := range t.entries {
		if t.entries[i].obj == id {
			return &t.entries[i]
		}
	}
	return nil
}

// noteExcessHit runs before the Damage event folds: it fixes the recipient's
// pre-batch thresholds at its first hit and notes a deathtouch source.
func noteExcessHit(t *excessTally, h excessHost, lki map[state.ObjID]map[state.ObjID]effects.DamageSourceLKI, ev *events.Event, batchOpen bool) {
	if ev.Kind != events.Damage || ev.Amount <= 0 || ev.Obj == 0 || !batchOpen {
		return
	}
	en := t.entry(ev.Obj)
	if en == nil {
		o := h.Game().Obj(ev.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			return
		}
		fresh := excessBatchEntry{obj: ev.Obj, controller: o.Controller}
		// Types mirror the Damage fold (events/apply_combat.go): the
		// layer-derived creature type, the printed planeswalker/battle face
		// whose loyalty/defense exchange the fold performs.
		if h.IsCreature(ev.Obj) {
			fresh.types |= excessTypeCreature
			fresh.creatureLethal = h.Toughness(ev.Obj) - o.Damage
		}
		if f := o.Face(); f != nil && f.IsPlaneswalker() {
			fresh.types |= excessTypePlaneswalker
			fresh.loyalty = o.Counter("LOYALTY")
		}
		if f := o.Face(); f != nil && f.IsBattle() {
			fresh.types |= excessTypeBattle
			fresh.defense = o.Counter("DEFENSE")
		}
		if fresh.types == 0 {
			return
		}
		t.entries = append(t.entries, fresh)
		en = &t.entries[len(t.entries)-1]
	}
	if en.types&excessTypeCreature != 0 && damageSourceHasDeathtouch(h, lki, *ev) {
		en.deathtouch = true
	}
}

// add adds the folded (post-replacement) amount to the recipient's batch
// total.
func (t *excessTally) add(stored *events.Event) {
	if stored.Kind != events.Damage || stored.Amount <= 0 || stored.Obj == 0 {
		return
	}
	if en := t.entry(stored.Obj); en != nil {
		en.total += stored.Amount
	}
}

// flushExcessBatch publishes the closed batch's excess recipients, in
// first-hit order. It is called once the outermost batch has closed, so the
// emitted events never join a batch.
func flushExcessBatch(t *excessTally, emit func(events.Event) events.Event) {
	if len(t.entries) == 0 {
		return
	}
	batch := t.entries
	t.entries = nil
	for i := range batch {
		en := &batch[i]
		if en.total > en.excessThreshold() {
			emit(events.Event{Kind: events.ExcessDamage, Obj: en.obj, Player: en.controller, Amount: en.types})
		}
	}
	// Reuse the backing array for the next batch unless an emit above
	// started a new tally.
	if t.entries == nil {
		t.entries = batch[:0]
	}
}

