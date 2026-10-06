// Shared fold helpers for persistent mana, goad expiry, zone removal,
// pair joins, extra-phase matching and mana adds. Split out of
// events/apply.go: code moved verbatim, no behaviour change.
package events

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

func manaClearKeepSlots(text string) [6]bool {
	var keep [6]bool
	for i := 0; i < len(text); i++ {
		if s := state.ManaIndex(text[i]); s >= 0 && s < len(keep) {
			keep[s] = true
		}
	}
	return keep
}

// cutManaPersistent splits a ManaAdd event's Text encoding into the
// restriction encoding it may also carry and whether the mana is persistent
// (PersistentMana$ True — the trailing " pm" suffix ManaPersistentText
// appends) and, for the shorter-lived CR 702.189a Firebending form, whether
// it also expires at end of combat (the trailing " pmc" suffix
// ManaCombatPersistentText appends). The combat suffix is checked FIRST
// because it ends in neither the plain suffix nor the empty string it would
// otherwise be mistaken for. Ordinary historical events never carry either.
func cutManaPersistent(text string) (rest string, persistent, combat bool) {
	if rest, ok := strings.CutSuffix(text, manaCombatPersistentSuffix); ok {
		return rest, true, true
	}
	rest, ok := strings.CutSuffix(text, manaPersistentSuffix)
	return rest, ok, false
}

// clearNonPersistent drains n ordinary (non-persistent) units from slot i of
// the pool ManaClear is emptying. The tagged tallies drain first (the typed
// units in their fixed Treasure > Cave > Desert order, then snow), then the
// plain remainder absorbs the rest with no tally to drain — so every
// SURVIVING unit's tag is attributed to the persistent share, which is where
// it belongs: every measured PersistentMana carrier produces plain mana, so
// the ordinary share drained at a boundary is the tagged one when one
// exists. The persistent tally is left alone: the surviving units are
// exactly the persistent ones (the caller cleared exactly Pool minus
// PersistentMana), so PersistentMana[i] keeps naming them. A persistent unit
// that is ALSO tagged (snow/typed) is the one attribution this drain cannot
// see — the final clamp keeps every tally within the pool in that
// unmeasured corner instead of letting the Snow/Typed <= Pool invariant
// break.
func clearNonPersistent(p *state.Player, i int, n int32) {
	p.Pool[i] -= n
	for t := range p.TypedMana {
		if n == 0 {
			break
		}
		d := min(n, p.TypedMana[t][i])
		p.TypedMana[t][i] -= d
		if t < 3 {
			// Drain the artifact subset with its parent type tally.
			p.ArtifactTyped[t][i] -= min(d, p.ArtifactTyped[t][i])
		}
		n -= d
	}
	if n > 0 {
		d := min(n, p.Snow[i])
		p.Snow[i] -= d
		n -= d
	}
	// n's remainder (if any) was plain ordinary units — no tally carries
	// them. Clamp the corner: a persistent unit that is also snow or typed
	// may have had its tag drained by the order above; restore the invariant
	// every reader (takeUnit, the typed count heads) relies on.
	if p.Snow[i] > p.Pool[i] {
		p.Snow[i] = p.Pool[i]
	}
	total := p.Snow[i]
	for t := range p.TypedMana {
		if p.TypedMana[t][i] > p.Pool[i] {
			p.TypedMana[t][i] = p.Pool[i]
		}
		if t < 3 && p.ArtifactTyped[t][i] > p.TypedMana[t][i] {
			p.ArtifactTyped[t][i] = p.TypedMana[t][i]
		}
		total += p.TypedMana[t][i]
	}
	if over := total - p.Pool[i]; over > 0 {
		if d := min(over, p.Snow[i]); d > 0 {
			p.Snow[i] -= d
			over -= d
		}
		for t := range p.TypedMana {
			if over == 0 {
				break
			}
			d := min(over, p.TypedMana[t][i])
			p.TypedMana[t][i] -= d
			if t < 3 {
				p.ArtifactTyped[t][i] -= min(d, p.ArtifactTyped[t][i])
			}
			over -= d
		}
	}
}

// clearRingBearers drops every seat's Ring-bearer designation naming id
// (CR 701.54b/701.54e: the designation ends when the permanent leaves the
// battlefield or another player gains control of it — the derived clear
// events.Apply's ControlChange and battlefield-leave paths share).
func clearRingBearers(g *state.Game, id state.ObjID) {
	if id == 0 {
		return
	}
	for i := range g.Players {
		if g.Players[i].RingBearer == id {
			g.Players[i].RingBearer = 0
		}
	}
}

// expireTurnGoads drops only default-duration relationships made by p.
func expireTurnGoads(in []state.GoadEffect, p state.PlayerID) []state.GoadEffect {
	out := in[:0]
	for _, ge := range in {
		if ge.Player == p && (ge.Duration == "" || ge.Duration == "UntilYourNextTurn") {
			continue
		}
		out = append(out, ge)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// pruneGoads enforces source/control conditions from replayable state.
// ArenaSkipVerify makes the arena walks the flags skip run anyway and panic
// on anything they would have had to do: the blocker-tombstone walks while no
// blocker is live (state.Game.BlockersLive) panic on any non-empty BlockedBy,
// and pruneGoads before any goad was seen (state.Game.GoadsSeen) panics on
// any non-nil Goads -- the empirical check that every write sets its flag. The events and rules test binaries set it; production leaves it
// false.
var ArenaSkipVerify bool

func pruneGoads(g *state.Game) {
	if !g.GoadsSeen() {
		// No object has ever gained a goad (the Goad fold above is the only
		// place a goad is added), so every Goads list is nil: skip the
		// whole-arena walk, which a mass departure otherwise ran once per
		// moved object.
		if ArenaSkipVerify {
			for i := range g.Objs {
				if g.Objs[i].Goads != nil {
					panic(fmt.Sprintf("events: obj %d has Goads %v before any goad was seen (a Goads write skipped state.Game.NoteGoad)", g.Objs[i].ID, g.Objs[i].Goads))
				}
			}
		}
		return
	}
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Goads == nil {
			// Nothing to prune, and the rewrite below would store nil over
			// nil: skip the per-object write (a pointer store with its GC
			// write barrier, on every object of the arena, on every move).
			continue
		}
		out := o.Goads[:0]
		for _, ge := range o.Goads {
			active := o.Zone == state.ZBattlefield
			switch ge.Duration {
			case "AsLongAsInPlay":
				src := g.Obj(ge.Source)
				active = active && src != nil && src.Zone == state.ZBattlefield
			case "AsLongAsControl":
				active = o.Zone == state.ZBattlefield && o.Controller == ge.Controller
			}
			if active {
				out = append(out, ge)
			}
		}
		if len(out) == 0 {
			o.Goads = nil
		} else {
			o.Goads = out
		}
	}
}

// zoneOwner picks whose zone list an object belongs to: the battlefield and the
// stack are keyed by controller, every private zone by owner.
func zoneOwner(o *state.Object, z state.Zone) state.PlayerID {
	if z == state.ZBattlefield || z == state.ZStack {
		return o.Controller
	}
	return o.Owner
}

func remove(g *state.Game, id state.ObjID, z state.Zone, p state.PlayerID) {
	src := g.Zone(z, p)
	for i, x := range src {
		if x == id {
			out := make([]state.ObjID, 0, len(src)-1)
			out = append(out, src[:i]...)
			out = append(out, src[i+1:]...)
			g.SetZone(z, p, out)
			return
		}
	}
}

// applyPair writes a Soulbond pairing (CR 702.103): both permanents' Paired
// fields are set to each other's id when both are on the battlefield. Neither
// half is written for an object that has left the battlefield (its own Move
// already reset it).
func applyPair(g *state.Game, srcID, partnerID state.ObjID) {
	src := g.Obj(srcID)
	partner := g.Obj(partnerID)
	if src != nil && src.Zone == state.ZBattlefield && partner != nil && partner.Zone == state.ZBattlefield {
		src.Paired = partnerID
		partner.Paired = srcID
	}
}

// matchExtraPhase finds the queue entry a consume (-1) or complete (-2)
// event names: the FIRST entry (creation order, deterministic) whose
// identity matches the event's carried fields. consumed=false matches only
// un-consumed grants (a consume marks), consumed=true only consumed ones (a
// complete removes). ok=false when no entry matches -- a malformed or
// stale message, a no-op by the case's totality stance.
func matchExtraPhase(g *state.Game, e *Event, consumed bool) (int, bool) {
	wantEntry := state.Step(0)
	wantEntryOK := false
	if len(e.IDs) > 0 {
		wantEntry = state.Step(e.IDs[0])
		wantEntryOK = wantEntry.Valid()
	}
	for i := range g.ExtraPhases {
		ep := &g.ExtraPhases[i]
		if ep.Consumed != consumed || ep.Player != e.Player || ep.AfterStep != e.Step ||
			ep.Source != e.Obj || ep.Execute != e.Counter || ep.Entry != wantEntry {
			continue
		}
		if !wantEntryOK {
			continue
		}
		return i, true
	}
	return 0, false
}

// countActivation folds one non-mana activation onto its source's
// per-turn census (state.Object.ActivatedThisTurn), under AbilityPush's
// condition: only a battlefield source counts. It must run BEFORE the
// caller's AddObject, which may reallocate g.Objs under the src pointer.
func countActivation(g *state.Game, id state.ObjID) {
	if src := g.Obj(id); src != nil && src.Zone == state.ZBattlefield {
		src.ActivatedThisTurn++
	}
}

// applyManaAdd folds one ManaAdd event (the ManaAdd case of Apply); the
// ManaUndo case reuses it with the amount negated so a reversal walks the
// identical slot and snow/typed tally arithmetic.
func applyManaAdd(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		player := &g.Players[e.Player]
		// PersistentMana$ True (task persistentmana): the suffix rides Text
		// after every other encoding, so cut it before the restriction
		// parse and mark the tally/batch below.
		rest, persistent, combat := cutManaPersistent(e.Text)
		// One event moves the pool and its parallel producer tally, so a
		// tally can never drift from the pool it partitions. Three counter
		// forms exist, and all three land in the colour's pool slot:
		//
		//   "S<colour>"       -- a SNOW mana unit (CR 107.4h), tallied in
		//                        Player.Snow so a {S} pip can be paid only
		//                        from it. The historical two-char form stays
		//                        first and exact: recorded games carry it.
		//   "<Tag><colour>"   -- a TYPED mana unit (task castfilter2),
		//                        tallied in Player.TypedMana[tag] so the
		//                        filtered Count$CastTotalManaSpent
		//                        Treasure/Cave/Desert heads can read how much
		//                        of a cast's spend came from a producer of
		//                        that type.
		//   a bare WUBRGC letter (or the empty default) -- plain pool mana.
		idx := manaAddSlot(e.Counter)
		player.Pool[idx] += e.Amount
		if e.Amount > 0 && persistent {
			player.PersistentMana[idx] += e.Amount
			if combat {
				player.CombatMana[idx] += e.Amount
			}
		}
		if len(e.Counter) == 2 && e.Counter[0] == 'S' {
			player.Snow[idx] += e.Amount
		} else if tag, slot, ok := state.TypedManaCounter(e.Counter); ok {
			base, artifact := state.ManaUnitTypes(tag)
			player.TypedMana[base][slot] += e.Amount
			if artifact && base != state.TypedArtifact {
				player.ArtifactTyped[base][slot] += e.Amount
			}
		}
		// The RestrictValid$/AddsNoCounter$ provenance is registered for
		// EVERY counter form, never only a plain one: a tagged restricted
		// unit (Echoing Cavern's Cave mana, Sunken Citadel's, Bucolic
		// Ranch's) keeps its restriction exactly like a plain one, and the
		// matching spend event (which carries r.Color verbatim) consumes
		// it here. Registering before the pool write would be equivalent
		// for the ADD path; the consume path needs the batch list, which
		// this block owns.
		if valid, srcID, cond, addsCounters, restricted := ManaRestrictionFromText(rest); restricted {
			if e.Amount > 0 {
				player.RestrictedMana = append(player.RestrictedMana, state.ManaRestriction{
					Color: e.Counter, Amount: e.Amount, Valid: valid, Source: srcID,
					NoCounter: cond, Persistent: persistent, Combat: persistent && combat, AddsCounters: addsCounters,
				})
			} else if e.Amount < 0 {
				// A restricted spend event names exactly the restriction batch it
				// consumes. Walk insertion order so two matching additions replay
				// identically, and tolerate a malformed historical event that
				// over-spends its batch without making Pool negative here.
				//
				// The consumed batches' own Persistent flags move the persistent
				// tally: the payment path carved a MATCHING batch, so the units
				// this spend took are the batch's, and the flag — not raw slot
				// arithmetic — is what keeps the tally on the units that
				// actually survived. The " pm" marker is meaningless on a
				// restricted spend; the batch flags are authoritative.
				need := -e.Amount
				perUsed, combatUsed := int32(0), int32(0)
				for i := 0; i < len(player.RestrictedMana) && need > 0; {
					r := &player.RestrictedMana[i]
					if r.Color != e.Counter || r.Valid != valid {
						i++
						continue
					}
					used := r.Amount
					if used > need {
						used = need
					}
					r.Amount -= used
					need -= used
					if r.Persistent {
						perUsed += used
					}
					if r.Combat {
						combatUsed += used
					}
					if r.Amount == 0 {
						player.RestrictedMana = append(player.RestrictedMana[:i], player.RestrictedMana[i+1:]...)
						continue
					}
					i++
				}
				if perUsed > 0 {
					d := perUsed
					if player.PersistentMana[idx] < d {
						d = player.PersistentMana[idx]
					}
					player.PersistentMana[idx] -= d
				}
				// The combat-persistent subset follows the same batch
				// flags: a spent Combat batch's units leave CombatMana, so
				// the end-of-combat demotion cannot strip an unrelated
				// turn-persistent unit of the same colour instead.
				if combatUsed > 0 {
					player.CombatMana[idx] -= min(player.CombatMana[idx], combatUsed)
				}
			}
		} else if e.Amount < 0 && persistent {
			// A marked PLAIN spend event names the persistent share the
			// payment consumed: the payment path (payManaForSpent) attributes
			// the slot's units ordinary-first over the VISIBLE pool and marks
			// the persistent remainder with this suffix, so the tally follows
			// the units that actually survived instead of raw slot arithmetic.
			// (The old fresh-rule — decrement past Pool minus PersistentMana —
			// misattributed whenever a payment's visible pool differed from
			// the raw slot: a hidden restricted batch, or a carve that consumed
			// the persistent batch first, made ordinary mana wrongly survive a
			// step boundary. An unmarked negative event consumes ordinary
			// units only, by the same attribution convention.)
			d := -e.Amount
			if player.PersistentMana[idx] < d {
				d = player.PersistentMana[idx]
			}
			player.PersistentMana[idx] -= d
			// The combat-persistent subset is consumed by the same attribution:
			// the payment cannot distinguish a combat unit from an ordinary
			// persistent one (they are interchangeable in the pool), so a spend
			// that reduced the persistent tally reduces the combat tally too,
			// bounded by what it still holds. The convention only decides WHICH
			// surviving units are emptied at end of combat when both classes are
			// simultaneously in the pool; the CR draws no such distinction.
			if cd := player.CombatMana[idx]; cd > 0 {
				if cd > d {
					cd = d
				}
				player.CombatMana[idx] -= cd
			}
		}
	}
}

// manaAddSlot is the pool slot a ManaAdd counter credits: a snow "S<colour>",
// a typed "<Tag><colour>", a bare WUBRGC letter, or C for the empty default.
func manaAddSlot(counter string) int {
	idx := state.MC
	if len(counter) == 2 && counter[0] == 'S' {
		idx = state.ManaIndex(counter[1])
	} else if _, slot, ok := state.TypedManaCounter(counter); ok {
		idx = slot
	} else if counter != "" {
		idx = state.ManaIndex(counter[0])
	}
	return idx
}
