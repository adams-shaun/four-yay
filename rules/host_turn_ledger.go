package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// host_turn_ledger.go is the engine's implementation of
// effects.HostTurnLedger, the turn-history half of effects.Host's ledger role
// (rules-engine refactor spec W1d): life, damage, card and counter movement
// and combat history this turn and last, read from the event log.

// combatHit snapshots one landed combat-damage-to-player instance for the
// per-turn ledger. The dealing object is read through g.Obj at damage time
// (it is still on the battlefield then); its *cards.Card face pointer and
// controller are copied into the hit so a later reader can match the spec
// after the source has died, left the battlefield or been turned face down.
func (e *Engine) combatHit(player state.PlayerID, source state.ObjID, amount int32) effects.CombatDamageHit {
	hit := effects.CombatDamageHit{Player: player, Source: source, Amount: amount}
	if o := e.G.Obj(source); o != nil {
		hit.Card = o.Card
		hit.FaceIdx = o.FaceIdx
		hit.Controller = o.Controller
	}
	return hit
}

// CombatDamageToPlayersThisTurn satisfies effects.Host's
// CombatDamageToPlayersThisTurn: every combat-damage instance dealt to a
// player so far this turn, in assignment order, as captured at the combat
// damage site (runCombatAssignments). Engine-side, NO-EVENT state that every
// rebuild re-derives; emit clears it on TurnChange.
func (e *Engine) CombatDamageToPlayersThisTurn() []effects.CombatDamageHit {
	return e.combatHitsThisTurn
}

// LifeLostThisTurn satisfies effects.Host's LifeLostThisTurn for
// Count$LifeOppsLostThisTurn (Rakdos, Lord of Riots' cost reduction): the
// total life p lost this turn, summed since the last TurnChange over every
// event lifeLoss classifies as a loss -- a LifeChange below zero AND a
// player Damage event. Damage dealt to a player causes that much life loss
// (CR 120.3a, 119.3) but folds straight to the life total with no
// LifeChange (events.Apply's Damage case), so a LifeChange-only fold
// missed every point of combat and burn damage (Stromkirk Bloodthief).
// Infect-marked player damage is poison, not life loss (CR 702.90b), and
// lifeLoss already excludes it. Using the one classifier the LifeLost
// triggers and the speed check read keeps the count and those triggers
// agreeing. Derived from the event log like CastThisTurn, so a replay that
// rebuilds the game arrives at the same number. Life GAINED is not folded
// in — "lost life" is a loss even if the player ended the turn higher than
// they started (CR 118.3's distinction, and the reading Forge's own head
// takes).
func (e *Engine) LifeLostThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if q, amount, ok := lifeLoss(ev); ok && q == p {
			n += amount
		}
	}
	return n
}

// CountersRemovedThisTurn satisfies effects.Host's CountersRemovedThisTurn
// for Count$CountersRemovedThisTurn <KIND> <player> (Blaster Hulk's per-{E}
// cast discount and Izzet Generatorium's paid-or-lost-four-or-more {E}
// activation gate): the TOTAL of player counters of kind p paid or lost this
// turn, summed from every negative-Amount PlayerCounterChange naming the kind
// (case-insensitively — the grants and the pays write the same kind text a
// card's script uses, e.g. "ENERGY") since the last TurnChange. Derived from
// the event log like LifeLostThisTurn, so a replay derives the same number.
// A payment and a loss are the same event shape — rules/mana.go's PayEnergy
// settle emits exactly this fold's input — and an object-counter removal
// (Kind CounterChange, a permanent losing counters) is deliberately NOT
// folded: the head's player form counts the PLAYER's pool only.
func (e *Engine) CountersRemovedThisTurn(p state.PlayerID, kind string) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PlayerCounterChange && ev.Player == p && ev.Amount < 0 &&
			strings.EqualFold(ev.Counter, kind) {
			n += -ev.Amount
		}
	}
	return n
}

// CountersAddedThisTurn is the rules-side backing for the three-part
// Count$CountersAddedThisTurn head. It deliberately uses the pre-event
// snapshot retained by emit rather than the live object.
func (e *Engine) CountersAddedThisTurn(kind, actorSpec, objectSpec string, sc effects.SpecContext) int32 {
	var n int32
	for _, add := range e.counterAddsThisTurn {
		if !strings.EqualFold(kind, "Any") && !strings.EqualFold(add.kind, kind) ||
			!effects.MatchesPlayerSpec(e.G, actorSpec, add.actor, sc.You) ||
			!effects.MatchesObjectCtx(e.G, objectSpec, &add.object, sc) {
			continue
		}
		n += add.amount
	}
	return n
}

// DamageTakenThisTurn satisfies effects.Host's DamageTakenThisTurn for the
// TargetedPlayer$DamageThisTurn count head (Knollspine Dragon's "draw cards
// equal to the damage dealt to target opponent this turn"): the total damage
// p was dealt this turn, summed from every player-targeted Damage event
// since the last TurnChange. A player hit is Kind Damage with Player set
// and Obj 0 — an object hit sets Obj and leaves Player 0 (seat 0 is a real
// player, so the discriminator is Obj == 0, never Player != 0); a
// replacement-rewritten Note never reaches this fold, and a redirect that
// moved a hit onto a permanent reads there instead. Derived from the event
// log like LifeLostThisTurn, so a replay derives the same number.
func (e *Engine) DamageTakenThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Damage && ev.Obj == 0 && ev.Player == p && ev.Amount > 0 {
			n += ev.Amount
		}
	}
	return n
}

// LifeGainedThisTurn satisfies effects.Host's LifeGainedThisTurn for
// Count$LifeYouGainedThisTurn (the "At the beginning of each end step, if you
// gained 4 or more life this turn" family's CheckSVar$ gate — Angelic Accord,
// Resplendent Angel, Valkyrie Harbinger): the total life p gained this turn,
// summed from every LifeChange above zero since the last TurnChange. Derived
// from the event log like LifeLostThisTurn, so a replay that rebuilds the
// game arrives at the same number. Life LOST is not folded in — "gained
// life" counts only positive LifeChanges (CR 118.3's distinction, the same
// one-sided read LifeLostThisTurn takes in the other direction).
func (e *Engine) LifeGainedThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.LifeChange && ev.Player == p && ev.Amount > 0 {
			n += ev.Amount
		}
	}
	return n
}

// CardsDiscardedThisTurn satisfies effects.Host's CardsDiscardedThisTurn for
// PlayerCountPropertyYou$CardsDiscardedThisTurn (Ambergris Citadel Agent's
// "X = cards you discarded this turn"): every events.IsDiscard move since
// the last TurnChange naming p — the ordinary Discard form by its Player
// field, the cost form (events.DiscardCost, which carries no Player — every
// emitter constructs it without one, so the Player field is seat 0 regardless
// of who paid) by the discarded object's owner alone, since a cost discard is
// paid from the payer's own hand (CR 118.2a). Classifying by the marker and
// not by "Player == 0 as a fallback" is what keeps seat 0's tally from
// counting every other seat's cost discard. Derived from the event log like
// LifeLostThisTurn, so a replay derives the same number.
func (e *Engine) CardsDiscardedThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if !events.IsDiscard(ev) {
			continue
		}
		if events.IsDiscardCost(ev) {
			if o := e.G.Obj(ev.Obj); o != nil && o.Owner == p {
				n++
			}
			continue
		}
		if ev.Player == p {
			n++
		}
	}
	return n
}

// CardsDrawnThisTurn satisfies effects.Host's CardsDrawnThisTurn for the
// PlayerCount<group>$Condition<N> CardsDrawn property (Smuggler's Share's
// "draw a card for each opponent who drew two or more cards this turn"):
// every events.Draw naming p since the last TurnChange. Derived from the
// event log like CardsDiscardedThisTurn, so a replay derives the same
// number. Every draw emitter — the draw step, an effect's Draw and the
// opening hand — emits the same event kind with Player set, so the fold
// counts them all, exactly as Forge's per-turn cardsDrawn list does.
func (e *Engine) CardsDrawnThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// StartingLife satisfies effects.Host's StartingLife: the opening life total
// genesis resolved (Config.StartingLife's 0-means-20 convention already
// applied in newWithRNG). Captured at construction and copied by Clone, so a
// replay derives the same value.
func (e *Engine) StartingLife() int32 { return e.startingLife }

func (e *Engine) TurnsTaken(p state.PlayerID) int32 {
	if int(p) >= len(e.G.Players) {
		return 0
	}
	if len(e.turnsTaken) != len(e.G.Players) || e.turnsTakenEpoch != len(e.L.Events) {
		e.turnsTaken = make([]int32, len(e.G.Players))
		for _, ev := range e.L.Events {
			if ev.Kind == events.TurnChange && int(ev.Player) < len(e.turnsTaken) {
				e.turnsTaken[ev.Player]++
			}
		}
		e.turnsTakenEpoch = len(e.L.Events)
	}
	return e.turnsTaken[p]
}

// AttackersThisTurn satisfies effects.Host's AttackersThisTurn for
// Count$AttackersDeclared (the Raid family's "attacked this turn" read): the
// number of attackers declared this turn, summed from every DeclareAttackers
// event's attacker list since the last TurnChange. Derived from the event log
// like CastThisTurn, so a replay that rebuilds the game arrives at the same
// number. A DeclareAttackers event carries its declared attackers in IDs (one
// event per defender); an event with no IDs contributes nothing.
// AttackersDeclaredThisTurn satisfies effects.Host's method of the same
// name: this turn's DeclareAttackers attacker ids, de-duplicated, oldest
// first. Derived from the event log like AttackersThisTurn.
func (e *Engine) AttackersDeclaredThisTurn() []state.ObjID {
	start := len(e.L.Events)
	for start > 0 && e.L.Events[start-1].Kind != events.TurnChange {
		start--
	}
	var out []state.ObjID
	seen := map[state.ObjID]bool{}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind != events.DeclareAttackers {
			continue
		}
		for _, id := range ev.IDs {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// LifeLostLastTurn satisfies effects.Host's LifeLostLastTurn: the life
// losses (lifeLoss: negative LifeChanges and non-infect player damage)
// naming p between the second-to-last and the last TurnChange of the log --
// the previous turn's window, the LifeLostThisTurn fold one turn back.
func (e *Engine) LifeLostLastTurn(p state.PlayerID) int32 {
	var n int32
	boundaries := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			boundaries++
			if boundaries == 2 {
				break
			}
			continue
		}
		if boundaries != 1 {
			continue
		}
		if q, amount, ok := lifeLoss(ev); ok && q == p {
			n += amount
		}
	}
	if boundaries < 2 {
		// The window before the first TurnChange is the pregame, not a turn.
		return 0
	}
	return n
}

// AttackedDuringLastTurn satisfies effects.Host's method of the same name.
// The walk runs backwards over the log: the window after the LAST
// TurnChange is the turn in progress and is skipped; the first earlier
// TurnChange naming q opens q's most recent completed turn, whose events
// run up to the next TurnChange. A DeclareAttackers inside it whose
// Player (the defending seat) is defender and whose attacker list is
// non-empty answers true.
func (e *Engine) AttackedDuringLastTurn(q, defender state.PlayerID) bool {
	ev := e.L.Events
	end := len(ev)
	// Skip the turn in progress.
	for end > 0 && ev[end-1].Kind != events.TurnChange {
		end--
	}
	if end == 0 {
		return false
	}
	end-- // the current turn's TurnChange itself
	for end > 0 {
		start := end
		for start > 0 && ev[start-1].Kind != events.TurnChange {
			start--
		}
		if start == 0 {
			return false // the pregame window, before any turn
		}
		if ev[start-1].Player == q {
			for _, x := range ev[start:end] {
				if x.Kind == events.DeclareAttackers && x.Player == defender && len(x.IDs) > 0 {
					return true
				}
			}
			return false
		}
		end = start - 1
	}
	return false
}

func (e *Engine) AttackersThisTurn() int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.DeclareAttackers {
			n += len(ev.IDs)
		}
	}
	return n
}
