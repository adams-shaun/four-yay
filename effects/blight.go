package effects

import (
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Blight", effBlight) }

// effBlight implements Forge's Blight (CR 701.60): for each player the
// Defined$ spec names — including the ValidTgts$ fallback, which makes
// Champion of the Weird's TARGETED OPPONENT the blighting player, not the
// ability's controller — that player chooses a creature they control and puts
// Num$ −1/−1 counters on it. A player controlling no creature blights nothing
// (CR 701.60's "a creature you control" never fails open onto someone else's
// creature). The choice is a real KChoose only when the player controls two or
// more eligible creatures — the strict-supersets rule every asking primitive
// here shares (effDiscard's TgtChoose, effSacrifice's player branch): with
// exactly one creature the deterministic answer IS the only legal answer, so
// the counters are placed silently; with none there is nothing to place.
//
// This is not a PutCounter variant: blight's Defined$ resolves to PLAYERS who
// each choose an OBJECT. The structural precedent is effSacrifice's
// player-target branch: the per-target cursor (BlightTarget) travels through
// the decision's ResumeTarget, the answer re-enters through ResumeKind
// "blight" with Ctx.BlightPicks set, and re-entry skips the targets already
// processed before the suspension so a multi-player Defined$ (High Perfect
// Morcant's Defined$ Opponent) asks each opponent in turn.
func effBlight(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// fx42 scoping: capture and clear the answered per-player pick BEFORE the
	// target walk, so a nested blight below this walk poses its own ask
	// instead of inheriting the outer answer (the SacPicks discipline).
	blightPicks := ([]state.ObjID)(nil)

	blightDone := false
	blightTarget := int(0)

	// Loud-fail-closed on any parameter outside the whitelist (the census
	// case-whitelist shape): Defined$/Num$ read here, ValidTgts$ through the
	// ordinary Defined() target fallback, Cost$/SorcerySpeed$ as activation
	// metadata on the AB$ spelling (Champion of the Weird), UnlessCost$/
	// UnlessPayer$ consumed by the shared unlessProceed gate in Resolve
	// (Chaos Spewer), ConditionCheckSVar$ consumed by the shared SVar-condition
	// gate (Dose of Dawnglow), and the display-only description keys. One Note
	// names the first unknown key in sorted order (a map range must never
	// reach an event unsorted) and the whole body no-ops — the effManifest
	// out-of-scope pattern, so an unmodelled shape degrades loudly rather
	// than silently guessing.
	var unknown []string
	for k := range sa.Params {
		switch k {
		case "Defined", "Num", "ValidTgts",
			"Cost", "SorcerySpeed",
			"ConditionCheckSVar", "UnlessCost", "UnlessPayer",
			"SpellDescription", "StackDescription", "TriggerDescription",
			"Description", "Secondary":
		default:
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented Blight shape: " + unknown[0]})
		return
	}

	n := Num(h, c, sa, "Num", 1)
	if n <= 0 {
		return
	}
	for targetIndex, t := range Defined(h, c, sa) {
		if !t.IsPlayer {
			// Blight's targets are players; an object target is not a shape
			// the corpus carries and is not one this primitive invents.
			continue
		}
		// Bounds guard (the effSacrifice idiom): never trust a
		// target-supplied player id against g.Players' indexing.
		if int(t.Player) >= len(g.Players) {
			continue
		}
		p := t.Player
		if blightDone {
			// Re-entry after some target's ask suspended: earlier targets
			// completed on the first pass and must be skipped; the asking
			// target applies its answer; later targets fall through and
			// pose their own asks (the effSacrifice/Dig per-target shape).
			if targetIndex < blightTarget {
				continue
			}
			if targetIndex == blightTarget {
				blightApply(h, p, n, blightPicks)
				continue
			}
		}
		var eligible []state.ObjID
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if MatchesSpecCtx(g, "Creature", id, c.SpecContext(c.Controller)) {
				eligible = append(eligible, id)
			}
		}
		switch {
		case len(eligible) == 0:
			// CR 701.60: no creature they control — nothing happens for
			// this player, never a blight of someone else's creature.
		case len(eligible) == 1:
			// Strict-supersets: one eligible creature is no choice. Place
			// the counters silently — no decision, no stand-in Note.
			h.Emit(events.Event{Kind: events.CounterChange, Obj: eligible[0],
				Counter: "M1M1", Amount: n})
		default:
			d := &decision.Decision{Player: p, Kind: decision.KChoose,
				Min: 1, Max: 1, Source: c.Source,
				ResumeKind:   "blight",
				ResumeSA:     sa,
				ResumeTarget: targetIndex,
				Prompt:       "Choose a creature to blight"}
			for _, id := range eligible {
				name := "a creature"
				if o := g.Obj(id); o != nil && o.Face() != nil {
					name = o.Face().Name
				}
				d.Options = append(d.Options, decision.Option{Index: len(d.Options),
					Kind: "blight", Label: name, Obj: id, Player: p})
			}
			if ans, ok := AskTape(h, d); ok {
				// The "blight" answer in hand: this player's counters,
				// then the walk goes on to the next target.
				blightApply(h, p, n, counterAnswerObjs(ans))
				continue
			}
			_ = Ask(h, d)

			// R-9 no-host stand-in: the first eligible creature in zone
			// order — the exact pick botpolicy's clamp fallback answers, so
			// a bot-answered ask emits the same events this silent path does.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
				Text: "blights the first matching creature (no engine host to ask)", Secret: true})
			h.Emit(events.Event{Kind: events.CounterChange, Obj: eligible[0],
				Counter: "M1M1", Amount: n})
		}
	}
}

// blightApply puts n -1/-1 counters on each answered creature for player p.
// Zone and controller checks keep a stray or stale answer from counting a
// creature the chooser no longer controls (the same guard the sacrifice
// re-entry uses).
func blightApply(h Host, p state.PlayerID, n int32, picks []state.ObjID) {
	g := h.Game()
	for _, id := range picks {
		o := g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != p {
			continue
		}
		h.Emit(events.Event{Kind: events.CounterChange, Obj: id,
			Counter: "M1M1", Amount: n})
	}
}
