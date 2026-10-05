package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func damageSourceHasDeathtouch(e *Engine, ev events.Event) bool {
	if ev.Kind != events.Damage {
		return false
	}
	source := e.inFlightDamageSource()
	if source == 0 {
		return false
	}
	if cause := e.actionCause(); cause != 0 {
		if lki, ok := e.damageSourceLKI[cause][source]; ok {
			return lki.Deathtouch
		}
	}
	return e.hasKeywordH(source, kwhDeathtouch)
}

// captureExcessBaseline runs before Apply marks the damage. It remembers the
// first pre-fold lethal threshold for this creature, not an intermediate
// threshold after earlier events in the same simultaneous batch.
func captureExcessBaseline(baselines map[state.ObjID]int32, ev events.Event, deathtouch bool, board interface {
	Game() *state.Game
	Toughness(state.ObjID) int32
	IsCreature(state.ObjID) bool
}) map[state.ObjID]int32 {
	if ev.Kind != events.Damage || ev.Obj == 0 || ev.Amount <= 0 || !board.IsCreature(ev.Obj) {
		return baselines
	}
	id := ev.Obj
	o := board.Game().Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		return baselines
	}
	if _, seen := baselines[id]; seen {
		return baselines
	}
	if baselines == nil {
		baselines = make(map[state.ObjID]int32)
	}
	lethal := board.Toughness(id) - o.Damage
	// CR 120.4a: one damage from a source with deathtouch is lethal for
	// assignment purposes. Existing marked damage can make the ordinary
	// threshold smaller, so only lower a positive threshold to one.
	if deathtouch && lethal > 1 {
		lethal = 1
	}
	baselines[id] = lethal
	return baselines
}

// damageTriggerAmount leaves ordinary modes unchanged. When already lethal,
// the whole hit is excess. Each simultaneous hit compares to the same base.
func damageTriggerAmount(baselines map[state.ObjID]int32, mode cards.TriggerMode, ev events.Event) int32 {
	if mode != cards.TriggerExcessDamageAll {
		return ev.Amount
	}
	if ev.Obj == 0 || ev.Amount <= 0 {
		return 0
	}
	baseline, ok := baselines[ev.Obj]
	if !ok || ev.Amount <= baseline {
		return 0
	}
	if baseline <= 0 {
		return ev.Amount
	}
	return ev.Amount - baseline
}

func setExcessTriggerAmount(pt *pendingTrigger, mode cards.TriggerMode, amount int32) {
	if mode == cards.TriggerExcessDamageAll {
		pt.Ctx.TriggerContext.TriggerAmount = amount
	}
}
