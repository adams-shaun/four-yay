// Event folds for player-level state: life, losses, counters, control, targets, attributes, imprint.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"sort"

	"github.com/adams-shaun/gorge/state"
)

// foldPlayerLost folds Kind PlayerLost into state.
func foldPlayerLost(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		g.Players[e.Player].Lost = true
		// CR 723.4/723.5: a player-controlling effect ends when either
		// participant leaves the game, so a lost seat's control folds are
		// cleared here, in the one fold that observes the loss. Clear both
		// directions: the lost seat as the CONTROLLED player (its own
		// entry) and the lost seat as the CONTROLLER (every entry naming
		// it). Leaving the controller half behind would rewrite the still-
		// active controlled seat's decisions to a departed seat (see
		// rules/engine.go controlPlayerRedirect); leaving the controlled
		// half behind would strand a dead seat's fold. Collected and sorted
		// so the clear is deterministic, exactly like every other map walk
		// that can reach a projected fact.
		delete(g.ControlledBy, e.Player)
		delete(g.ControlArmedTurn, e.Player)
		var orphaned []state.PlayerID
		for subj, ctl := range g.ControlledBy {
			if ctl == e.Player {
				orphaned = append(orphaned, subj)
			}
		}
		sort.Slice(orphaned, func(i, j int) bool { return orphaned[i] < orphaned[j] })
		for _, subj := range orphaned {
			delete(g.ControlledBy, subj)
			delete(g.ControlArmedTurn, subj)
		}
	}
}

// foldGameOver folds Kind GameOver into state.
func foldGameOver(g *state.Game, e *Event) {
	// Ruling T22-g (fix round 1): the first GameOver wins; a later one
	// on an already-finished game is a no-op. Without this guard, a log
	// carrying two GameOver events (a duplicate, a replay quirk, a
	// tampered log) produced a game simultaneously won (Winner set by
	// the first) and drawn (Draw set by the second) -- Over alone
	// cannot express "this event doesn't apply", so the guard has to
	// live here, ahead of everything else in this case.
	if g.Over {
		return
	}
	// Ruling T22-a (Amount is the draw/winner discriminator, the same
	// trick TargetsChosen already uses to tell its two target shapes
	// apart), pinned down fully by T22-g and, for Amount == 0's own
	// Player validity, T22-l: exactly two shapes are defined, and
	// everything else is not a GameOver this build recognizes at all.
	//   Amount == 0: a win, but ONLY when Player validates. Ruling
	//     T22-l (fix round 2): a fix-round-1 build of this let an
	//     invalid Player under Amount 0 still set Over, with Winner
	//     left at its untouched zero value -- an invalid seat silently
	//     reading as seat 0 winning, the exact defect class this whole
	//     discriminator exists to remove. For an untrusted log,
	//     refusing to end the game on a malformed win claim is the safe
	//     response: it is detectable (Over stays false) rather than
	//     manufacturing a draw the log never actually carried.
	//   Amount == 1: CR 104.4a's draw. Player is irrelevant; Winner is
	//     never touched and Draw says so explicitly, since PlayerID(0)
	//     is both Winner's zero value and a real seat and so cannot
	//     mean "nobody" on its own.
	//   anything else (including Amount == 0 with an invalid Player):
	//     not a shape this Kind defines. Previously any other Amount
	//     still set Over unconditionally and, since Winner's own guard
	//     only ever checked validPlayer, a tampered event naming an
	//     out-of-range Player under some third Amount read as "seat 0
	//     won" regardless. Changing nothing at all is the safe response
	//     to a shape this build cannot interpret.
	switch {
	case e.Amount == 0 && validPlayer(g, e.Player):
		g.Over = true
		g.Winner = e.Player
	case e.Amount == 1:
		g.Over = true
		g.Draw = true
	}
}

// foldControlChange folds Kind ControlChange into state.
func foldControlChange(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		if o := g.Obj(e.Obj); o != nil {
			// CR 701.54b: a Ring-bearer designation ends "until another
			// player gains control of it" — the old controller is still
			// on o.Controller here, so the seat losing the designation is
			// the one naming the object that is not the new controller.
			// (The new controller gaining control of their OWN bearer is
			// not a change of controller for the designation and keeps it.)
			for i := range g.Players {
				if g.Players[i].RingBearer == o.ID && state.PlayerID(i) != e.Player {
					g.Players[i].RingBearer = 0
				}
			}
			// CR 702.157b: the suspected designation ends the moment
			// ANOTHER player gains control of the permanent -- the same
			// condition the Ring-bearer designation keys on, evaluated on
			// the still-old controller (o.Controller is still the old seat
			// here). A same-controller ControlChange (no real change of
			// controller) keeps the designation.
			if o.Controller != e.Player {
				o.Suspected = false
			}
			changeControl(g, o, e.Player)
			// An AsLongAsControl goad ends the moment its controller
			// condition fails; pruning here keeps a later return of
			// control from reviving it.
			pruneGoads(g)
		}
	}
}

// foldControlPlayerChange folds Kind ControlPlayerChange into state.
func foldControlPlayerChange(g *state.Game, e *Event) {
	// CR 720: Player is the controlling seat and IDs[0] the controlled
	// seat. +1 grants (and stamps g.Turn as the armed turn), -1 expires at
	// the end of the controlled player's next turn. Both are folded so a
	// log-only reconstruction re-derives the same interval.
	if !validPlayer(g, e.Player) || len(e.IDs) == 0 {
		return
	}
	subj, isPlayer := e.IDs[0].PlayerRef()
	if !isPlayer || !validPlayer(g, subj) {
		return
	}
	ctl := e.Player
	switch {
	case e.Amount > 0:
		// CR 720.6: a player cannot control themselves, and control of
		// one player by another is not a chain the rules build. Keeping
		// the payer's own seat out of the map is the fail-closed guard.
		if ctl == subj {
			break
		}
		if g.ControlledBy == nil {
			g.ControlledBy = map[state.PlayerID]state.PlayerID{}
		}
		if g.ControlArmedTurn == nil {
			g.ControlArmedTurn = map[state.PlayerID]int32{}
		}
		g.ControlledBy[subj] = ctl
		g.ControlArmedTurn[subj] = g.Turn
	case e.Amount < 0:
		delete(g.ControlledBy, subj)
		delete(g.ControlArmedTurn, subj)
	}
}

// foldLifeChange folds Kind LifeChange into state.
func foldLifeChange(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		g.Players[e.Player].Life += e.Amount
	}
}

// foldPlayerCounterChange folds Kind PlayerCounterChange into state.
func foldPlayerCounterChange(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		g.Players[e.Player].AddCounter(e.Counter, e.Amount)
	}
}

// foldCounterChange folds Kind CounterChange into state.
func foldCounterChange(g *state.Game, e *Event) {
	if e.Text != EntryCounterNotice {
		if o := g.Obj(e.Obj); o != nil {
			o.AddCounter(e.Counter, e.Amount)
		}
	}
}

// SubTargetNotice is the Text a TargetsChosen event carries when the target
// belongs to a SubAbility$ link of the spell or ability rather than to its
// root declaration (CR 601.2c: the whole chain's targets are chosen on cast).
// The event still IS a targeting -- ward, "becomes the target" and the crime
// check all match it by Kind -- but its fold appends to Object.SubTargets and
// leaves the root's Targets untouched. Amount keeps the append shapes'
// meaning (2 object, 3 player), so a reader that tells a player target from
// an object target by Amount needs no second rule.
const SubTargetNotice = "sub-target"

// foldTargetsChosen folds Kind TargetsChosen into state.
func foldTargetsChosen(g *state.Game, e *Event) {
	// Amount discriminates the target shape (Ruling T14-b's own
	// discriminator, extended by Task 4 with two more shapes that
	// APPEND rather than replace -- a spell can gain a second target
	// from a later effect without losing the first):
	//   0 (default): replace with object targets, read from IDs.
	//   1: replace with a single player target, read from Player.
	//   2: append one object target per entry in IDs.
	//   3: append a single player target, read from Player.
	if o := g.Obj(e.Obj); o != nil {
		// CR 707.10c: recording chosen targets on a COPY consumes its
		// one-shot MayChooseTarget$ election. The only TargetsChosen a copy
		// can receive is the copy-target ask's own answer (a copy is minted
		// after its original was cast, so no cast-flow target records onto
		// it), so this clear cannot swallow an unrelated choice; replay
		// re-runs the same fold.
		if o.IsCopy {
			o.CopyMayChooseTarget = false
		}
		if e.Text == SubTargetNotice {
			// A SubAbility$ link's cast-time target (CR 601.2c): it joins
			// SubTargets, never the root's own Targets. Only the two APPEND
			// shapes are meaningful -- a chain target never replaces
			// anything -- and any other Amount folds nothing.
			switch e.Amount {
			case 2:
				for _, id := range e.IDs {
					o.SubTargets = append(o.SubTargets, state.Target{Obj: id})
				}
			case 3:
				if validPlayer(g, e.Player) {
					o.SubTargets = append(o.SubTargets, state.Target{Player: e.Player, IsPlayer: true})
				}
			}
			return
		}
		switch e.Amount {
		case 1:
			if validPlayer(g, e.Player) {
				o.Targets = []state.Target{{Player: e.Player, IsPlayer: true}}
			}
		case 2:
			for _, id := range e.IDs {
				o.Targets = append(o.Targets, state.Target{Obj: id})
			}
		case 3:
			if validPlayer(g, e.Player) {
				o.Targets = append(o.Targets, state.Target{Player: e.Player, IsPlayer: true})
			}
		default:
			var targets []state.Target
			for _, id := range e.IDs {
				targets = append(targets, state.Target{Obj: id})
			}
			o.Targets = targets
		}
	}
}

// foldAlterAttribute folds Kind AlterAttribute into state.
func foldAlterAttribute(g *state.Game, e *Event) {
	// The AlterAttribute fold (task alterattr1): the engine models the
	// "Saddled" (CR 702.171), "Suspected" (CR 702.157) and "Plotted"
	// (CR 701.34, task kw-plot)
	// attributes. Text names the attribute so a future modelled one
	// extends this switch without an event-schema change; an unmodelled
	// name never reaches Apply (the effect emits its loud
	// unsupported-attribute Note instead of an event), so the fall
	// through to no fold is replay-safe. Amount 1 grants, -1 removes.
	if o := g.Obj(e.Obj); o != nil {
		switch e.Text {
		case "Saddled":
			// CR 702.171b: the designation expires at end of turn. Read the
			// turn from the game, never from the event, so replay derives it.
			if e.Amount >= 1 {
				o.SaddledTurn = g.Turn
			} else {
				o.SaddledTurn = 0
			}
		case "Suspected":
			o.Suspected = e.Amount >= 1
		case "Prepared":
			// CR 722.3a: the prepared designation may only be granted to a
			// permanent that has a prepare spell, and a permanent already
			// prepared cannot gain it again; the false->true transition is
			// what mints CR 722.3c's exile copy. Amount -1 removes it (an
			// unprepare effect or the copy's cast). The copy is minted here,
			// inside Apply, so a log-only replay mints the identical object.
			if e.Amount >= 1 {
				if !o.Prepared && o.HasPrepareSpell() {
					o.Prepared = true
					grantPreparedCopy(g, o)
				}
			} else {
				o.Prepared = false
			}
		case "Monstrous":
			// CR 701.31b's monstrous designation (Giggling Skitterspike's
			// `{5}: Monstrosity 5`, task agent-20260919T190014Z): Amount is
			// the monstrosity COUNT the resolving ability carried (the
			// BecomeMonstrous triggers' `SVar:MonstrosityX:TriggerCount$Amount`
			// reads it back), and Amount >= 1 sets the mark. CR 701.31 gives
			// the designation no controller-change end -- the only clear is
			// the Move fold's leaving-battlefield block below.
			o.Monstrous = e.Amount >= 1
		case "Renowned":
			// CR 702.112b: renowned persists only for this battlefield
			// permanent; Amount carries the Renown count for listeners.
			o.Renowned = e.Amount >= 1
		case "Suspend":
			o.SuspendGranted = e.Amount >= 1
		case "Plotted":
			// CR 701.34c: the plotted designation on an exiled card. The
			// grant stamps PlottedTurn with the CURRENT turn so the free
			// cast's "on a later turn" gate (rules/legal.go's exile walk)
			// can compare e.G.Turn against it; a removal (-1) clears it.
			// The turn is read from the game, never carried on the event,
			// exactly like Enlist's EnlistedTurn stamp -- a log-only
			// replay re-derives the same value.
			if e.Amount >= 1 {
				o.PlottedTurn = g.Turn
			} else {
				o.PlottedTurn = 0
			}
		case "CantUntapNextStep":
			// CR 611.2b: the one-shot "doesn't untap during its controller's
			// next untap step" window stamped at grant time (Frost Lynx's
			// runtime `KW$ HIDDEN ...` Pump). Amount >= 1 grants, -1 is the
			// untap step's consume (rules/turn.go finishUntapStep). Like
			// ExertSkipUntap it spans the turn boundary and is therefore NOT
			// reset at TurnChange; it is cleared on leaving the battlefield
			// with ExertSkipUntap (events/apply_zone.go).
			o.CantUntapNextStep = e.Amount >= 1
		}
	}
}

// foldImprint folds Kind Imprint into state.
func foldImprint(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil {
		if e.Text == "clear" {
			o.Imprinted = nil
			o.ImprintTokens = nil
			o.SeekFound = nil
			o.EncodedCards = nil
		} else if e.Text == "forget" {
			// ForgetImprinted$ (Pump's Chrome Mox body): remove exactly the
			// named ids from the persistent Imprinted list, keeping the
			// rest -- a bad or partial payload degrades to a smaller
			// forget, never a wider one.
			drop := make(map[state.ObjID]bool, len(e.IDs))
			for _, id := range e.IDs {
				drop[id] = true
			}
			kept := make([]state.ObjID, 0, len(o.Imprinted))
			for _, id := range o.Imprinted {
				if !drop[id] {
					kept = append(kept, id)
				}
			}
			o.Imprinted = kept
			keptTokens := make([]state.ObjID, 0, len(o.ImprintTokens))
			for _, id := range o.ImprintTokens {
				if !drop[id] {
					keptTokens = append(keptTokens, id)
				}
			}
			o.ImprintTokens = keptTokens
			keptFound := make([]state.ObjID, 0, len(o.SeekFound))
			for _, id := range o.SeekFound {
				if !drop[id] {
					keptFound = append(keptFound, id)
				}
			}
			o.SeekFound = keptFound
			keptEncoded := make([]state.ObjID, 0, len(o.EncodedCards))
			for _, id := range o.EncodedCards {
				if !drop[id] {
					keptEncoded = append(keptEncoded, id)
				}
			}
			o.EncodedCards = keptEncoded
		} else {
			// Text is an in-kind discriminator, not a new Event field:
			// ImprintCards$ records Forge's imprintedCards list while a
			// ChangeZone-to-exile records the separate exiledCards list.
			// Both associations survive replay, but only the latter is
			// pruned when its card leaves exile (in Move below).
			// Text "until-host-leaves" records ChangeZone's Duration$
			// UntilHostLeavesPlay association on the SOURCE object: the
			// exiled cards (IDs) come back to the zone carried in Amount
			// when the source leaves the battlefield (rules sweepExileReturn).
			if e.Text == "until-host-leaves" {
				from := state.Zone(e.Amount)
				if from.Valid() {
					for _, id := range e.IDs {
						if g.Obj(id) == nil {
							continue
						}
						dupe := false
						for _, en := range o.ExileReturn {
							if en.Obj == id {
								dupe = true
								break
							}
						}
						if !dupe {
							o.ExileReturn = append(o.ExileReturn, state.ExileReturnEntry{Obj: id, From: from})
						}
					}
				}
			} else {
				list := &o.Imprinted
				if e.Text == "exiled-with" {
					list = &o.ExiledCards
				} else if e.Text == "imprint-tokens" {
					// ImprintTokens$ records the created TOKENS here, the
					// association `Defined$ Imprinted` resolves while they sit
					// on the battlefield (state.Object.ImprintTokens).
					list = &o.ImprintTokens
				} else if e.Text == "seek-found" {
					// Seek's ImprintFound$ records the cards it moved to a
					// hand here; `Defined$ Imprinted` resolves them wherever
					// they currently sit (state.Object.SeekFound), so the
					// ordinary Imprinted list's exile-only reader keeps its
					// CR 607.2a contract.
					list = &o.SeekFound
				} else if e.Text == "encoded" {
					// Cipher (CR 702.99a): the resolving spell card is exiled
					// ENCODED on this creature. state.Object.EncodedCards is
					// the creature-side link the combat-damage trigger reads;
					// it is pruned when the card leaves exile and cleared when
					// this object leaves the battlefield (both in Move), so
					// only text without that cleanup names a different list.
					list = &o.EncodedCards
				}
				for _, id := range e.IDs {
					if g.Obj(id) != nil {
						*list = append(*list, id)
					}
				}
			}
		}
	}
}
