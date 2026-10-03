package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("Attach", effAttach)
	// kw:For Mirrodin (CR 702.159) and kw:Job select (CR 702.182) expand to
	// exactly the Living Weapon shape in cards/kw_*.go -- an
	// enters-the-battlefield trigger that mints a token, remembers it and
	// chains the Attach above -- so they are supported by the same code
	// paths and must be registered here or the report's coverage still
	// counts every carrier as missing a primitive.
	RegisterNonAPI("kw:Equip", "kw:Enchant", "kw:Living Weapon", "kw:For Mirrodin", "kw:Job select", "kw:Reconfigure", "kw:Fortify")
}

// attachChoiceIDs is the "attach_choice" answer's id list: every chosen
// option naming an object.
func attachChoiceIDs(chosen []decision.Option) []state.ObjID {
	out := make([]state.ObjID, 0, len(chosen))
	for _, o := range chosen {
		if o.Obj != 0 {
			out = append(out, o.Obj)
		}
	}
	return out
}

// emitAttach publishes "obj becomes attached to bearer" as events.Attach,
// first publishing events.Unattached when obj was already attached to a
// DIFFERENT permanent. CR 701.3b makes "becomes unattached" a real event
// (the Mode$ Unattached family: Captain's Hook, Grafted Exoskeleton, Grafted
// Wargear, Stitcher's Graft), and an Attach overwriting state.Object.AttachedTo
// would otherwise drop the former bearer's detach entirely -- leaving the
// Grafted Exoskeleton trigger silent when its Equipment is re-equipped from
// one creature to another. Every Attach emit in this package goes through
// here, so a future re-attaching site cannot reintroduce the omission.
//
// The former bearer is carried on Unattached.IDs[0], the same field
// rules/attach.go's attachmentSBAs detach arms use and the referent
// rules/trigger_match.go's triggerRemembered reads for
// Defined$ TriggeredObjectLKICopy. A fresh attach (AttachedTo == 0) or a
// redundant re-attach to the SAME bearer emits no Unattached: nothing became
// unattached, so no trigger may fire.
func emitAttach(h Host, obj, bearer state.ObjID) {
	if o := h.Game().Obj(obj); o != nil && o.AttachedTo != 0 && o.AttachedTo != bearer {
		h.Emit(events.Event{Kind: events.Unattached, Obj: obj,
			IDs:  []state.ObjID{o.AttachedTo},
			Text: "reattached to a new permanent"})
	}
	h.Emit(events.Event{Kind: events.Attach, Obj: obj, IDs: []state.ObjID{bearer}})
}

// playerAttachPool resolves a DB$ Attach PlayerChoices$ spec to the living
// seats it admits, in the deterministic AliveFrom seat walk, evaluated with
// the resolving controller as You and the resolving source bound (so a
// source-anchored spec such as `Player.!IsRemembered` reads the source's own
// list). It is the ONE pool derivation: the asking pass offers these seats as
// the decision's options and the answered re-entry re-checks the chosen seat
// against the same helper, so the bot's own answer can never be outside what
// the re-entry accepts (the one-home rule for a decision's legal answers).
func playerAttachPool(h Host, c *Ctx, spec string) []state.PlayerID {
	g := h.Game()
	var out []state.PlayerID
	for _, p := range g.AliveFrom(0) {
		if MatchesPlayerSpecFrom(g, spec, p, c.Controller, c.Source) {
			out = append(out, p)
		}
	}
	return out
}

// emitPlayerAttach emits the player-destination Attach event and, when
// RememberAttached$ True, the two-half remember the object path's attachTo
// makes (Lynde's DBDraw conditions on Remembered). The event is the raw
// player-attach shape events.apply's Attach branch folds into
// AttachedPlayer/HasAttachedPlayer; it is deliberately not routed through
// emitAttach, whose Unattached detach half reads the permanent AttachedTo link
// only.
func emitPlayerAttach(h Host, c *Ctx, p *AttachParams, attachObj state.ObjID, seat state.PlayerID) {
	h.Emit(events.Event{Kind: events.Attach, Obj: attachObj, Player: seat, Text: "attach to player"})
	if p.RememberAttached {
		c.Remembered = append(c.Remembered, state.Target{Obj: attachObj})
		eventRemember(h, c, attachObj)
	}
}

// Attachable reports whether obj may legally be attached to target. Task 14
// leaves this always-true: the full check includes "the target is not
// protected from the attachment's colours" (CR 702.16e for being attached
// despite protection). That half is NOT added here, and is deliberately NOT
// a host hook on this interface: the protection tasks keep enforcement
// inside rules' emit, where an Attach event is re-checked against its
// target, and effects.Host must never host a protection decision. The hook
// exists so effAttach never hard-codes "always attach" — the one decision
// the attachment SBA does not own on its own.
func Attachable(g *state.Game, obj state.ObjID, target state.ObjID) bool {
	_ = g
	return true
}

// attachSpecAdmitsOffBattlefield is effAttach's CR 704.5m counterpart for the
// graveyard-enchant Aura family (Animate Dead, Dance of the Dead): the
// attaching object's current enchant spec -- at cast time the printed face,
// because the Aura is still on the stack and layer-6 grants live rules-side --
// admits an off-battlefield bearer exactly when the spec matches the bearer
// AND carries an inZone<X> word whose X names the bearer's current zone. The
// zone word is mandatory: MatchesSpecFrom's bare type words are zone-blind
// (spec `Creature` matches a graveyard bear), so without it every Aura would
// be attachable to every zone. An attaching object with no Enchant keyword
// (Equip's kw:Equip expansion, Living Weapon) fails closed to the
// battlefield-only rule.
func attachSpecAdmitsOffBattlefield(g *state.Game, attachObj state.ObjID, tg *state.Object, controller state.PlayerID) bool {
	o := g.Obj(attachObj)
	if o == nil || o.Face() == nil || tg == nil {
		return false
	}
	param, ok := o.Face().KeywordParam("Enchant")
	if !ok || strings.TrimSpace(param) == "" {
		return false
	}
	spec, _, _ := strings.Cut(param, ":")
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	zoneNamed := false
	for word := range strings.SplitSeq(spec, ".") {
		z, has := strings.CutPrefix(word, "inZone")
		if !has {
			continue
		}
		if zn, known := ParseZoneWord(z); known && zn == tg.Zone {
			zoneNamed = true
		}
	}
	if !zoneNamed {
		return false
	}
	return MatchesSpecFrom(g, spec, tg.ID, controller, attachObj)
}

// attachSpecAdmitsPlayer reports whether the attaching object's own Enchant
// spec admits this particular player bearer. Use the same player-spec matcher
// as the attachment SBA: checking only that the keyword names some player
// would let an Enchant:Opponent Aura attach to its controller through an
// ordinary Defined$ destination (for example Ardenn's Targeted player).
func attachSpecAdmitsPlayer(g *state.Game, attachObj state.ObjID, bearer state.PlayerID) bool {
	o := g.Obj(attachObj)
	if o == nil || o.Face() == nil {
		return false
	}
	param, ok := o.Face().KeywordParam("Enchant")
	if !ok || strings.TrimSpace(param) == "" {
		return false
	}
	spec, _, _ := strings.Cut(param, ":")
	return MatchesPlayerSpecFrom(g, strings.TrimSpace(spec), bearer, o.Controller, o.ID)
}

// effAttach implements "Attach": it fastens obj (Object$ Self by default --
// Aura's SP$ Attach is cast with Object$ Self so the STILL-ON-THE-STACK aura
// is the object being attached, Living Weapon's SVar is also Object$ Self
// with Defined$ Remembered naming the germ) onto the first Defined$ target
// that is a legal point of attachment: an object currently on the
// battlefield, not obj itself, and one Attachable accepts -- or, for an
// object whose OWN Enchant spec names a player (Enchant:Player /
// Enchant:Opponent), a living seat. The very first legal target wins, which
// is what makes the living-weapon shape work (the freshly minted germ is
// Remembered[0]).
//
// When no Defined$ target qualifies -- a seat the attaching object cannot
// enchant, a non-battlefield object, obj itself, or nothing legal at all --
// it refuses with a Note rather than emitting an Attach. The refusal is how
// the effect stays deterministic and observable while it has nothing legal
// to do.
//
// Choices$ names the pool the RESOLVING CONTROLLER picks the unspecified
// side from: with no Object$ key it names the OBJECT to attach (Goldwardens'
// Gambit's "you may attach an Equipment you control to it",
// unexpected_request's same shape); with Object$ present it names the
// DESTINATION (Breath of Fury's "attach CARDNAME to a creature you
// control"). The pool is the battlefield sweep the filter admits, evaluated
// with the resolving controller as You; the choice poses a real KChoose
// (Min 0 when Optional$ True -- picking nothing is the decline -- else Min
// 1), the answered ids ride Ctx.AttachChoice (consumed and cleared, fx42
// scoping), and the asking pass's resolved destination list rides the ask so
// a RepeatEach body's Defined$ Imprinted binding -- which does not survive a
// suspension -- never has to be re-derived. A mandatory Min-1 pool with
// exactly one candidate takes it without an ask (the strict-supersets
// convention: a decision nobody could answer differently is never emitted);
// an Optional pool with nothing eligible declines silently, and a mandatory
// one falls to the same Note refusal an illegal destination gets.
//
// An Optional$ True Attach WITHOUT Choices$ (Ajani's Chosen's "you may attach
// it to the token", Cori-Steel Cutter's "you may attach this Equipment to
// it") asks its controller a yes/no KChoose before attaching, through the
// shared Ask boundary with the "attach_optional" resume arm; a decline emits
// no Attach and no refusal Note, and the chained SubAbility$ still runs (the
// suspension plumbing in effects.Resolve owns the chain). A host that
// cannot ask takes the deterministic decline stand-in (R-9); botpolicy's
// clamp fallback answers option 0, which is the "yes" option, so bots
// attach. With NO legal target the existing Note refusal fires and no ask
// is ever posed (a decision nobody could answer differently).
//
// RememberAttached$ True (all seven corpus carriers) puts the permanent just
// attached into the ability's Remembered, both halves -- the ctx list the
// chained SubAbility / the RepeatEach loop's folding reads (the filter
// exclusion Goldwardens' next iteration's !IsRemembered needs) and the
// source's event-backed persistent list eventRemember writes (the same
// two-half discipline RememberTokens$ on effToken and RememberTargets$ on
// effPumpAll apply).
func effAttach(h Host, c *Ctx, sa *cards.SA) {
	ap := AttachOf(sa)
	noteUnreadParams(h, c, "Attach", ap.Unread)
	// kw:Reconfigure's unattach half (cards/kw_reconfigure.go): the minted
	// ability carries Unattach$ True and resolves to the no-IDs Attach
	// event -- the detach encoding rules/attach.go's CR 704.5n SBA and the
	// bestowed detach already fold. An unattached source (the offer gate in
	// rules/legal.go withholds the ability, so this is only reachable on a
	// stale or malformed answer) refuses with the same Note convention the
	// illegal-destination refusals use, deterministically and observably.
	if ap.Unattach {
		if src := h.Game().Obj(c.Source); src == nil || src.AttachedTo == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "cannot unattach: not attached"})
			return
		}
		h.Emit(events.Event{Kind: events.Attach, Obj: c.Source})
		return
	}
	// fx42 scoping: consume and clear the answered attach_choice fields at
	// the top, so a nested Attach in the same chain poses its own ask.
	answered := ([]state.ObjID)(nil)

	answerDests := ([]state.ObjID)(nil)

	answeredDone := false
	answerPlayer := state.PlayerID(0)

	answeredPlayerDone := false

	obj := c.Source
	// objs is the resolved Object$ list. It names one object for every
	// single-object carrier (Equip/Enchant/Living Weapon, Memory's Journey,
	// Ajani's Chosen) and EVERY object the resolved selector admits for the
	// plural carriers (Fumble's `Object$ AttachedTo Targeted.Aura,Equipment`,
	// Rhuk's and Cass's `Object$ AttachedTo ...Equipment`), whose card text
	// attaches THEM ALL. obj stays the first entry for the branches that name
	// a single object (the Enchant:Player destination, the Choices$ pool's
	// object-side pick); the destination walks below loop over objs.
	objs := []state.ObjID{c.Source}
	switch ap.objectKind {
	case attachObjectSelf:
		// Equip's kw:Equip expansion, Enchant's kw:Enchant and Living
		// Weapon's Object$ Self all name the source: today's default, kept
		// byte-identical.
	case attachObjectRemembered:
		if ts := objectsOf(c.Remembered); len(ts) > 0 {
			obj = ts[0].Obj
			objs = []state.ObjID{obj}
		}
	default:
		// TriggeredCardLKICopy (Ajani's Chosen -- the ENTERING Aura, not the
		// source), Targeted and every other object spec the shared resolver
		// definedSpec already supports. A spec it cannot resolve keeps the
		// today default (obj = c.Source) -- EXCEPT the dotted `AttachedTo
		// <referent>` family below, whose whole meaning is "the attachments":
		// a bound-but-empty resolution (no object is attached to the referent)
		// keeps NO source fallback, and an unbound one (absent or plural
		// bearer) fails closed the same way, so the card can never attach
		// itself in place of the attachments (Fumble on a bare creature).
		//
		// Only the dotted `AttachedTo` family is PLURAL: it is the one Object$
		// spelling whose card text names a whole set ("attach them"). Every
		// other selector -- TriggeredCardLKICopy, Remembered, Targeted and
		// friends -- keeps the historical first-take (os[0]), because its
		// referent can resolve several entries for unrelated reasons (Ajani's
		// Chosen remembers BOTH the entering Aura and the Cat token, and only
		// the Aura is the object to attach). Widening the first-take to every
		// selector would attach the attachments to each other.
		plural := ap.objectKind == attachObjectAttachedTo
		if ts, ok := definedSpec(h, c, ap.Object); ok {
			os := objectsOf(ts)
			if len(os) > 0 {
				obj = os[0].Obj
			}
			objs = objs[:0]
			if plural {
				for _, t := range os {
					objs = append(objs, t.Obj)
				}
			} else if len(os) > 0 {
				objs = append(objs, obj)
			}
		} else if plural {
			// The dotted selector resolved unknown (an absent or plural
			// referent binding): fail closed to no objects, never the source.
			objs = objs[:0]
		}
	}
	// An Aura with Enchant:Player or Enchant:Opponent attaches to a seat, not
	// a permanent. The ordinary destination walker below intentionally accepts
	// only battlefield objects; keep other Attach bodies on that existing path.
	//
	// Enchant:Opponent (Archnemesis, Maddening Hex, Overencumbered, Psychic
	// Possession, Tenuous Truce) is the same player destination with a cast-
	// time restriction the target offer already enforced (kwEnchant mints
	// `ValidTgts$ Opponent`); this branch re-checks the restriction so a
	// stale/malformed resolution can never enchant the controller.
	if ap.EnchantSelf {
		if aura := h.Game().Obj(obj); aura != nil && aura.Face() != nil {
			if param, ok := aura.Face().KeywordParam("Enchant"); ok {
				spec, _, _ := strings.Cut(param, ":")
				if spec == "Player" || spec == "Opponent" {
					for _, dest := range Defined(h, c, sa) {
						if !dest.IsPlayer || int(dest.Player) >= len(h.Game().Players) || h.Game().Players[dest.Player].Lost {
							continue
						}
						if spec == "Opponent" && dest.Player == c.Controller {
							continue
						}
						h.Emit(events.Event{Kind: events.Attach, Obj: obj, Player: dest.Player, Text: "attach to player"})
						return
					}
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "cannot attach: no legal player"})
					return
				}
			}
		}
	}
	// PlayerChoices$ (Curse of Leeches' `DB$ Attach | Object$ Self |
	// PlayerChoices$ Player`, Lynde's `DB$ Attach | Object$ ChosenCard |
	// PlayerChoices$ Opponent`): the destination is a player chosen from the
	// named pool. The value names the DESTINATION POOL, not the chooser --
	// `PlayerChoices$ Player` admits every living seat, `PlayerChoices$
	// Opponent` the resolving controller's opponents. The chooser is the
	// resolving controller (Forge's Chooser$ override is not carried by either
	// corpus carrier and is not invented here). Keyed on the param, NOT on the
	// Enchant:Player branch above: neither carrier carries a Keyword$ param.
	if spec := ap.PlayerChoices; spec != "" {
		pool := playerAttachPool(h, c, spec)
		if answeredPlayerDone {
			// The answered seat is re-checked against the live pool
			// (recomputed here with the SAME helper the asking pass used), so
			// a stale or malformed answer is refused with no Attach (the
			// malformed-answer conservative read) and the chain continues via
			// Resolve. Deriving ask and re-check from one helper is what keeps
			// the bot's own option-0 answer inside the validator.
			for _, p := range pool {
				if p == answerPlayer {
					emitPlayerAttach(h, c, ap, obj, p)
					return
				}
			}
			return
		}
		if len(pool) == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: "cannot attach: no legal player"})
			return
		}
		if len(pool) == 1 {
			// The only legal answer is not a decision anybody could answer
			// differently: attach without an ask (the object path's auto-take
			// convention).
			emitPlayerAttach(h, c, ap, obj, pool[0])
			return
		}
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
			Source: c.Source, ResumeKind: "attach_player_choice", ResumeSA: sa,
			ResumeRemembered: copyTargets(c.Remembered),
			Prompt:           ap.ChoicePrompt}
		for i, p := range pool {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "player", Player: p})
		}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: what the
			// "attach_player_choice" re-entry does -- it re-runs effAttach
			// from its first line (so the unread-parameter Note again), then
			// attaches to the answered seat if the live pool still holds it.
			noteUnreadParams(h, c, "Attach", ap.Unread)
			for _, o := range ans {
				if o.Kind != "player" {
					continue
				}
				for _, p := range pool {
					if p == o.Player {
						emitPlayerAttach(h, c, ap, obj, p)
						return
					}
				}
				break
			}
			return
		}
		return
	}
	var legalT []state.Target
	destCandidatesFor := func(attachObj state.ObjID) []state.Target {
		var out []state.Target
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				// A player is a legal bearer only when the attaching object's
				// OWN Enchant spec names a player (an Enchant:Player /
				// Enchant:Opponent Aura). Without that gate a `Defined$` that
				// happens to resolve a player for an unrelated reason (a
				// ChoosePlayer upstream, a triggered attacking player) could
				// fasten an Equipment to a seat. Archnemesis, Maddening Hex and
				// Ardenn are the three corpus carriers this admits; a departed
				// seat is refused exactly as a departed object bearer is.
				if !attachSpecAdmitsPlayer(h.Game(), attachObj, t.Player) {
					continue
				}
				if int(t.Player) >= len(h.Game().Players) || h.Game().Players[t.Player].Lost {
					continue
				}
				out = append(out, t)
				continue
			}
			target := t.Obj
			if target == attachObj {
				continue
			}
			tg := h.Game().Obj(target)
			if tg == nil {
				continue
			}
			if tg.Zone != state.ZBattlefield {
				// The graveyard-enchant Aura family (Animate Dead, Dance of the
				// Dead): the attaching object's CURRENT enchant spec (at cast
				// time the printed face -- the Aura is still on the stack, so
				// no rules-side layer read is available here) zone-positively
				// admits an off-battlefield bearer. Equip and every non-Aura
				// attach keep the battlefield-only rule and the Note refusal.
				if !attachSpecAdmitsOffBattlefield(h.Game(), attachObj, tg, c.Controller) {
					continue
				}
			} else if !Attachable(h.Game(), attachObj, target) {
				continue
			}
			out = append(out, t)
		}
		return out
	}
	// attachTo emits the Attach event for one object and, when
	// RememberAttached$ True, the two-half remember (see the function comment).
	rememberAttached := ap.RememberAttached
	attachTo := func(attachObj, target state.ObjID) {
		emitAttach(h, attachObj, target)
		if rememberAttached {
			c.Remembered = append(c.Remembered, state.Target{Obj: attachObj})
			eventRemember(h, c, attachObj)
		}
	}
	// The plural-object walk (Fumble's `Object$ AttachedTo Targeted.Aura,
	// Equipment`, Rhuk's and Cass's `Object$ AttachedTo ...Equipment`):
	// attachableBy reports whether a destination is legal for AT LEAST one
	// resolved object (the offer rule), and attachAll attaches every object
	// whose OWN legality admits it, returning the count. With a single
	// resolved object both reduce exactly to the old single-object reads, so
	// every single-object carrier stays byte-identical.
	attachAll := func(target state.ObjID) int {
		n := 0
		for _, o := range objs {
			if o == target {
				continue
			}
			if !Attachable(h.Game(), o, target) {
				continue
			}
			attachTo(o, target)
			n++
		}
		return n
	}
	attachableBy := func(target state.ObjID) bool {
		for _, o := range objs {
			if o == target {
				continue
			}
			if Attachable(h.Game(), o, target) {
				return true
			}
		}
		return false
	}
	// A Choices$ pool the controller picks from: the battlefield sweep the
	// filter admits, evaluated with the resolving controller as You. With
	// Object$ present the pool is the DESTINATION side; without it, the
	// OBJECT side. One sweep serves both the asking pass and the answered
	// re-entry (a pure read: no event, and a suspension between the two
	// changes no state, so the sweep is the same both times).
	//
	// Known limitation: the sweep is battlefield-only -- ChoiceZone$ (e.g.
	// SVar:DBAttach:DB$ Attach | Choices$ Instant | ChoiceZone$ Graveyard)
	// and Chooser$ (e.g. Chooser$ TriggeredCardController) are unread, so a
	// card carrying either takes this branch, finds its wrong-zone or
	// wrong-chooser pool empty, and emits the deterministic "cannot attach:
	// no legal target" refusal -- silently inert, not working.
	if spec := ap.Choices; spec != "" {
		pool := battlefieldValidTargets(h, c, spec)
		// The answered attach_choice re-entry (fx42 scoping: already consumed
		// and cleared at the top). With no Object$ the answer names the
		// OBJECT to attach and the asking pass's resolved destination list
		// rode Ctx.AttachDests; with Object$ present it names the
		// DESTINATION. An empty answer is the Min-0 Optional decline -- or a
		// malformed mandatory answer, whose conservative read is the same
		// no-attach -- and the chain continues via Resolve either way.
		if answeredDone {
			if len(answered) == 0 {
				return
			}
			if ap.ObjectPresent {
				// Object$ present: the answer names the DESTINATION. It is
				// re-checked against the legal destination list recomputed
				// here (the same rejections the asking pass applies), so a
				// stale or malformed answer -- including one naming obj
				// itself, which a raw battlefield sweep CAN admit -- is
				// refused with no Attach (the malformed-answer conservative
				// read) and the chain continues via Resolve.
				legal := attachableBy(answered[0])
				if !legal {
					return
				}
				attachAll(answered[0])
				return
			}
			// No Object$: the answer names the OBJECT to attach, and the
			// asking pass's resolved destination list rode Ctx.AttachDests
			// (a RepeatEach body's Defined$ Imprinted binding does not
			// survive the suspension, so the re-entry never re-derives it).
			if len(answerDests) == 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "cannot attach: no legal target"})
				return
			}
			obj = answered[0]
			objs = []state.ObjID{obj}
			attachTo(obj, answerDests[0])
			return
		}
		if ap.ObjectPresent {
			var dest []state.ObjID
			for _, t := range pool {
				if !attachableBy(t.Obj) {
					continue
				}
				dest = append(dest, t.Obj)
			}
			if len(dest) == 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "cannot attach: no legal target"})
				return
			}
			max := 1
			if ap.OptionalTrue {
				max = 0
			}
			if max == 1 && len(dest) == 1 {
				attachAll(dest[0])
				return
			}
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: max, Max: 1,
				Source: c.Source, ResumeKind: "attach_choice", ResumeSA: sa,
				ResumeRemembered: copyTargets(c.Remembered),
				Prompt:           ap.ChoicePrompt}
			// The options are the LEGAL destinations, not the raw pool sweep:
			// the pool can hold objects destCandidates' rejection would refuse,
			// including obj itself (aura_graft's Choices$ Permanent admits the
			// attaching Aura, which IS a battlefield Permanent), and offering
			// the object as its own attachment point would self-attach on a
			// bot's option-0 answer (AttachChoice carries Option.Obj, so the
			// re-entry's attachTo(answered[0]) would emit Attach{IDs:[obj]}).
			// Indexing over dest keeps the auto-take above and the re-entry in
			// agreement -- the source can never be offered or selected.
			for i, t := range dest {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: "card", Obj: t, Player: c.Controller})
			}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: the
				// "attach_choice" re-entry (after its unread-parameter Note):
				// a decline attaches nothing; a DESTINATION still legal for
				// an object gets them all.
				noteUnreadParams(h, c, "Attach", ap.Unread)
				if picked := attachChoiceIDs(ans); len(picked) > 0 && attachableBy(picked[0]) {
					attachAll(picked[0])
				}
				return
			}
			return
		}
		// Object-side pool (Goldwardens' Gambit, unexpected_request, and
		// Yuffie, Materia Hunter): each Choices$ object is a possible object
		// to attach, so destinations must be checked against those candidates,
		// not against the resolving source. Keep only destinations legal for
		// every offered object; the chosen candidate can then use the saved
		// destination list safely after the ask suspends resolution.
		optional := ap.OptionalTrue
		var eligiblePool []state.Target
		for _, candidate := range pool {
			candidateDests := destCandidatesFor(candidate.Obj)
			// The object-side Choices$ pool compares destinations by ObjID (a
			// player target carries Obj==0), so a player bearer has no
			// identity here and is deliberately left to the no-Choices
			// ordinary walk below, which dispatches on IsPlayer.
			var objDests []state.Target
			for _, d := range candidateDests {
				if !d.IsPlayer {
					objDests = append(objDests, d)
				}
			}
			candidateDests = objDests
			if len(candidateDests) == 0 {
				continue
			}
			eligiblePool = append(eligiblePool, candidate)
			if len(eligiblePool) == 1 {
				legalT = candidateDests
				continue
			}
			var common []state.Target
			for _, dest := range legalT {
				for _, candidateDest := range candidateDests {
					if candidateDest.Obj == dest.Obj {
						common = append(common, dest)
						break
					}
				}
			}
			legalT = common
		}
		pool = eligiblePool
		if len(pool) == 0 {
			if optional {
				return
			}
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "cannot attach: no eligible choice"})
			return
		}
		var legal []state.ObjID
		for _, t := range legalT {
			legal = append(legal, t.Obj)
		}
		if len(legal) == 0 {
			// The existing refusal convention: no legal destination, no ask
			// (a decision nobody could answer differently), one Note.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "cannot attach: no legal target"})
			return
		}
		min := 1
		if optional {
			min = 0
		}
		if min == 1 && len(pool) == 1 {
			obj = pool[0].Obj
			objs = []state.ObjID{obj}
			attachTo(obj, legal[0])
			return
		}
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: min, Max: 1,
			Source: c.Source, ResumeKind: "attach_choice", ResumeSA: sa,
			ResumeRemembered: copyTargets(c.Remembered),
			// The resolved destination list rides the ask: a RepeatEach
			// body's Defined$ Imprinted binding does not survive the
			// suspension, so the re-entry never re-derives it.
			ResumeChoices: PayloadTargets(legalT),
			Prompt:        ap.ChoicePrompt}
		for i, t := range pool {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "card", Obj: t.Obj, Player: c.Controller})
		}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the "attach_choice"
			// re-entry (after its unread-parameter Note): a decline
			// attaches nothing; the answered OBJECT goes to the first
			// destination of the list the ask carried.
			noteUnreadParams(h, c, "Attach", ap.Unread)
			picked := attachChoiceIDs(ans)
			if len(picked) == 0 {
				return
			}
			if len(legal) == 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "cannot attach: no legal target"})
				return
			}
			attachTo(picked[0], legal[0])
			return
		}
		return
	}
	// No Choices$: the destination is the first legal Defined$ target. A
	// plural Object$ list (Rhuk's and Cass's AttachedTo Equipment selectors)
	// gets each object its OWN first legal destination -- the card attaches
	// every one of them. With a single resolved object the union below is
	// exactly the old destCandidatesFor(obj) list, so the single-object
	// carriers take the same first legal target byte-identically.
	legalT = legalT[:0]
	// A destination's identity is (player?, seat/obj): two distinct player
	// seats both carry Obj==0, so the dedup key must include IsPlayer+Player
	// or the first seat would mask every later one.
	type destKey struct {
		isPlayer bool
		player   state.PlayerID
		obj      state.ObjID
	}
	seenDest := make(map[destKey]bool)
	for _, o := range objs {
		for _, t := range destCandidatesFor(o) {
			k := destKey{isPlayer: t.IsPlayer, player: t.Player, obj: t.Obj}
			if seenDest[k] {
				continue
			}
			seenDest[k] = true
			legalT = append(legalT, t)
		}
	}
	var legal []state.ObjID
	for _, t := range legalT {
		legal = append(legal, t.Obj)
	}
	if ap.OptionalTrue {
		// fx42 scoping: consume and clear the answered election at the top,
		// so a nested Attach in the same chain poses its own ask.
		ans := string("")

		switch {
		case ans == "yes":
			// Answered "attach": fall through to the ordinary attach loop.
		case ans != "":
			// Answered "no" (or any non-affirmative marker): the decline. No
			// Attach, no refusal Note; the chain continues via Resolve.
			return
		default:
			if len(legal) == 0 {
				break // unanswered AND nothing legal: the Note refusal below.
			}
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
				Source: c.Source, ResumeKind: "attach_optional", ResumeSA: sa,
				ResumeRemembered: copyTargets(c.Remembered),
				Prompt:           "Attach it?",
				Options: []decision.Option{
					{Index: 0, Kind: "yes", Label: "Yes — attach", Player: c.Controller},
					{Index: 1, Kind: "no", Label: "No", Player: c.Controller},
				}}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: the
				// "attach_optional" re-entry (after its unread-parameter
				// Note) attaches on a yes and declines otherwise.
				noteUnreadParams(h, c, "Attach", ap.Unread)
				if len(ans) == 0 || ans[0].Kind != "yes" {
					return
				}
				break
			}
			// AskAsked suspends; the answer re-enters with Ctx.AttachOpt set.
			// AskNoHost is the deterministic decline stand-in (R-9) — the
			// same class the search_mayshuffle confirm falls back to (the
			// clamp-answered bot path below answers option 0 = "yes").
			return
		}
	}
	attached := 0
	for _, o := range objs {
		for _, t := range destCandidatesFor(o) {
			if t.IsPlayer {
				// The player destination (Archnemesis, Maddening Hex, Ardenn):
				// the raw player-attach emit folds into
				// AttachedPlayer/HasAttachedPlayer and carries the two-half
				// remember, exactly like the PlayerChoices$ branch above.
				emitPlayerAttach(h, c, ap, o, t.Player)
			} else {
				attachTo(o, t.Obj)
			}
			attached++
			break
		}
	}
	if attached == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "cannot attach: no legal target"})
	}
}
