package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effDestroy is a single-target removal effect: exactly the shape CR 608.2b
// target rechecking exists for. Today the only recheck is "does the target
// still exist, and is it still on the battlefield" -- a target that stayed on
// the battlefield but became newly ineligible some other way (e.g. it gained
// Indestructible in response, or protection from the source) between
// targeting and resolution is not rechecked. See the Task 18 report.
func effDestroy(h Host, c *Ctx, sa *cards.SA) {
	// Forge's ForgetOtherTargets$ replaces the prior remembered set before
	// this Destroy, while RememberTargets$ records only objects that actually
	// leave the battlefield (not targets spared by regeneration or
	// indestructibility).  Keep both the resolution-local and event-backed
	// halves in sync, as the chained sub-ability may read either one.
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKForgetOtherTargets)), "True") {
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	// RememberTargets$ records only objects that actually leave the
	// battlefield. RememberDestroyed$ True (Transforming Flourish) is Forge's
	// spelling of the same "this permanent was destroyed this way" record.
	// RememberLKI$ True (Noxious Gearhulk) does BOTH -- it records the object
	// and captures its last-known-information snapshot (CR 603.10 look-back),
	// because the chained read (RememberedLKI$CardToughness) needs the
	// battlefield P/T that events.Apply's Move clears. The snapshot rides
	// Ctx.LKI/LKIPower/LKIToughness, the same fields rules' triggerLKI publishes
	// and evalRefProperty reads for a zone-change trigger; evalRefProperty
	// applies it only when the snapshot names the referenced object, so no
	// other remembered read is affected.
	rememberTargets := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberTargets)), "True")
	rememberDestroyed := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberDestroyed)), "True")
	rememberLKI := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberLKI)), "True")
	// Same pre-batch discipline as effDestroyAll: the targets Defined
	// resolves are destroyed as one simultaneous batch (a multi-target
	// Destroy over a lifelink Equipment and its bearer must not make the
	// bearer's LKI depend on battlefield order), so the snapshot covers all
	// of them before the first move.
	var victims []state.ObjID
	for _, t := range Defined(h, c, sa) {
		o := h.Game().Obj(t.Obj)
		if t.IsPlayer || o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		if h.HasKeyword(o.ID, "Indestructible") {
			continue
		}
		victims = append(victims, o.ID)
	}
	if len(victims) > 0 {
		h.BatchDepartures(victims)
		defer h.EndBatchDepartures()
	}
	for _, id := range victims {
		o := h.Game().Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		// NoRegen$ is compared against "True", not against empty: an explicit
		// NoRegen$ False PERMITS regeneration, and reading it as "set, so
		// suppress" would invert the card. The corpus splits 144 True / 1
		// False (creepy_doll.txt), and that one is unreachable today because
		// cards/link.go auto-links only SubAbility$, not the WinSubAbility$ it
		// hangs off -- so this is correctness insurance for when that changes,
		// not a live fix.
		if sa.ParamStr(cards.PKNoRegen) != "True" && ReplaceDestruction(h, id) {
			continue
		}
		// Umbra armor (CR 702.90) applies even when NoRegen$ suppresses
		// regeneration — it is its own replacement, not a shield. Consuming
		// the Aura leaves it in the graveyard; when the loop reaches the Aura
		// itself (a DestroyAll that named it too) the zone guard above skips it.
		if ReplaceUmbraArmor(h, id) {
			continue
		}
		// Capture the last-known information BEFORE the Move folds: the
		// snapshot must see the battlefield permanent (its counters, pump
		// layers and printed toughness), not the graveyard card the move
		// leaves behind. The zone guard above proved o is on the battlefield.
		var lki *state.Object
		var lkiPower, lkiToughness int32
		var lkiValid bool
		if rememberLKI {
			cp := o.CloneDeep()
			lki = &cp
			lkiPower, lkiToughness = h.Power(id), h.Toughness(id)
			lkiValid = true
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
			From: state.ZBattlefield, To: state.ZGraveyard, Text: "destroyed"})
		// Host.Emit applies move replacements before folding the move. Only
		// remember a permanent that actually ended up in the graveyard; a
		// replacement such as exile must not feed a later IsRemembered search.
		if rememberTargets || rememberDestroyed || rememberLKI {
			if moved := h.Game().Obj(id); moved != nil && moved.Zone == state.ZGraveyard {
				if rememberLKI {
					c.LKI = lki
					c.LKIPower, c.LKIToughness, c.LKIPTValid = lkiPower, lkiToughness, lkiValid
				}
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
	}
}

func effDestroyAll(h Host, c *Ctx, sa *cards.SA) {
	spec := sa.ParamStr(cards.PKValidCards)
	if spec == "" {
		spec = "Permanent"
	}
	zone := state.ZBattlefield
	if raw := strings.TrimSpace(sa.ParamStr(cards.PKZone)); raw != "" {
		var ok bool
		zone, ok = ParseZoneWord(raw)
		if !ok {
			return
		}
	}
	g := h.Game()
	remember := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberDestroyed)), "True")
	// One pre-batch victim list across every player, then ONE departure
	// snapshot, then the emit loop (CR 704.3 simultaneity, as far as the
	// sequential emit model can express it): the CR 603.10a lifelink LKI a
	// later victim's departure capture reads must be the state from
	// immediately before the FIRST move -- a destroy-all over a
	// lifelink-granting Equipment and its bearer must not make the bearer's
	// own lifelink LKI depend on battlefield order.
	var victims []state.ObjID
	sc := c.SpecContext(c.Controller)
	sc.CombatDamageHits = h.CombatDamageToPlayersThisTurn()
	for _, p := range g.AliveFrom(0) {
		ids := append([]state.ObjID(nil), g.Zone(zone, p)...)
		for _, id := range ids {
			if zone == state.ZBattlefield && h.HasKeyword(id, "Indestructible") {
				continue
			}
			if MatchesSpecCtx(g, spec, id, sc) {
				victims = append(victims, id)
			}
		}
	}
	if len(victims) > 0 && zone == state.ZBattlefield {
		h.BatchDepartures(victims)
		defer h.EndBatchDepartures()
	}
	for _, id := range victims {
		if g.Obj(id) == nil || g.Obj(id).Zone != zone {
			continue
		}
		if zone == state.ZBattlefield {
			// NoRegen$ != "True", not == "": see effDestroy's note above.
			if sa.ParamStr(cards.PKNoRegen) != "True" && ReplaceDestruction(h, id) {
				continue
			}
			// Umbra armor after the shield: see effDestroy's note.
			if ReplaceUmbraArmor(h, id) {
				continue
			}
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
			From: zone, To: state.ZGraveyard, Text: "destroyed"})
		if remember {
			// Forge's RememberDestroyed$ adds only cards that actually
			// reached the graveyard; a move replacement may redirect it.
			if moved := h.Game().Obj(id); moved != nil && moved.Zone == state.ZGraveyard {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
	}
}

// sacTargetCardReferent resolves a SacValid$ spec that names the resolution's
// card TARGET rather than the resolving source. Forge writes this referent as
// the base `TargetedCard` with the `.Self` property (“the card this ability
// targeted”), e.g. Enchanter's Bane's `SacValid$ TargetedCard.Self`: the
// sacrifice is aimed at the targeted enchantment's controller, and the only
// eligible permanent is that enchanted enchantment itself. The engine's filter
// grammar reads a bare `Self` relative to the resolving SOURCE, so this base
// is unrecognised there and fails closed; resolving it here, against the
// resolution's object targets, is what admits the actual targeted card. Any
// other base (including `Self`/`Card.Self`, whose subject IS the source) and any
// qualifier other than `.Self` are not this referent and return ok=false,
// leaving MatchesSpecCtx's own reading in place -- fail closed, never widened.
// The caller checks the returned id against the pool object, so a referent that
// is not in the asked player's battlefield never becomes eligible.
func sacTargetCardReferent(spec string, c *Ctx) (state.ObjID, bool) {
	base, qual, _ := strings.Cut(strings.TrimSpace(spec), ".")
	if base != "TargetedCard" || qual != "Self" {
		return 0, false
	}
	for _, t := range c.Targets {
		if !t.IsPlayer && t.Obj != 0 {
			return t.Obj, true
		}
	}
	return 0, false
}

// effSacrifice moves permanents to the graveyard. Sacrifice ignores
// Indestructible: sacrificing is not destruction (CR 701.16), so no
// HasKeyword/Indestructible gate and no ReplaceDestruction/regeneration
// consultation -- a regenerated creature does not survive being sacrificed.
// Same CR 608.2b caveat as effDestroy: only existence-and-zone is rechecked.
//
// A sacrifice aimed at a PLAYER now asks that player (CR 701.21a: "its
// controller chooses one") through a real KChoose over their matching
// permanents: Amount$ (default 1) sizes the ask, Optional$ True makes it
// "may sacrifice" (Min 0), and a hand of fewer eligible permanents than
// Amount$ sacrifices everything it has without asking (there is no choice
// to record, the effDiscard TgtChoose strict-supersets rule). The answer
// re-enters this effect through ResumeKind "sacrifice" with Ctx.SacPicks
// set, one suspension per Defined$ target (the cursor mirrors effDig's
// per-library asks). An Optional$ ask whose no-host fallback runs takes the
// first Amount$ eligible permanents — the same pick the pre-ask engine made
// — so games that never reach a real player answer replay byte-identically
// up to the pick the answer names.
func effSacrifice(h Host, c *Ctx, sa *cards.SA) {
	// UnlessCost$ is handled by the shared unlessProceed gate in Resolve,
	// exactly as it is for every other API — including the Vexing Devil
	// damage-payment offer (UnlessCost$ DamageYou<N>, UnlessPayer$ Opponent,
	// UnlessSwitched$ True), whose "pay" is taking the damage. When the gate
	// consumed the resolution — an ask was posed (suspended), the answered
	// choice spared the permanent, or every opponent declined the offer —
	// this body does not run at all.
	g := h.Game()
	// SacValid$ narrows WHAT may be sacrificed ("Creature.nonToken",
	// "Artifact"). With no SacValid$ at all the default is "Permanent" (any
	// permanent). The older justification -- that the self-sacrifice and
	// at-end-of-step lines need "Permanent" because the object they sacrifice
	// may be an artifact, a land or a creature -- is empirically false: those
	// lines carry no Defined$ and no ValidTgts$, so Defined() resolves them
	// to the SOURCE object (effects/context.go) and they take the object-target
	// path below.
	//
	// ValidCard$ is the corpus's second narrowing spelling: three Sacrifice
	// SAs carry `ValidCard$ Card.Self` and no SacValid$. Exactly one of them is
	// player-targeted -- Expert-Level Safe's
	// `DB$ Sacrifice | Defined$ You | ValidCard$ Card.Self` -- so before this
	// read its controller handed over whichever permanent sat first in zone
	// order (an artifact, a land, anything) rather than "this artifact". The
	// other two, Departed Deckhand and Dream Strix, carry no Defined$ and no
	// ValidTgts$, so Defined() resolves them to their own source object and
	// they take the object-target path below (where that object already IS the
	// self the spec names). The two spellings never co-occur in the corpus
	// (measured: 3 ValidCard$ lines, 437 SacValid$ lines, 0 carrying both), so
	// applying both as a conjunction reads every line exactly once. Card.Self
	// resolves through SpecContext.Source, so the player-targeted line can only
	// hand over the source itself.
	spec := sa.ParamStr(cards.PKSacValid)
	if spec == "" {
		spec = "Permanent"
	}
	validCard := strings.TrimSpace(sa.ParamStr(cards.PKValidCard))
	// RememberSacrificed$ True drives the task's effect-driven sacrifice
	// capture: it makes effSacrifice record the LKI snapshot (power,
	// toughness, mana value) of each object it sacrifices, so a SubAbility$
	// chained after it can resolve Sacrificed$CardPower/CardManaCost/Amount
	// against what THIS ability just sacrificed. Without the flag nothing is
	// remembered -- and nothing is, because the flag is read nowhere else in
	// this package (the sacrifice_audit test only counts its occurrence), so
	// the absence is the conservative same-as-before no-op, not a regression.
	remember := sa.ParamStr(cards.PKRememberSacrificed) != ""
	// rememberLKICapture captures the sacrificed object's LKI (before the
	// MoveZone resets its counters) into c.Sacrificed, when the flag asks it
	// to. Idempotent per call site; called exactly once per sacrificed object.
	rememberLKICapture := func(id state.ObjID) {
		if remember {
			c.Sacrificed = append(c.Sacrificed, SacrificedLKI(h, id))
			// Forge's RememberSacrificed$ also remembers the card, which is
			// what a following ConditionDefined$ Remembered, Remembered$Amount
			// or RememberedCard reads (Braids, Scapeshift, Victimize).
			c.Remembered = append(copyTargets(c.Remembered), state.Target{Obj: id})
			eventRemember(h, c, id)
		}
	}
	// fx42 scoping: capture and clear the answered per-player pick BEFORE the
	// target loop, so a nested sacrifice below this walk poses its own ask.
	// SacTarget identifies the exact target that asked: earlier targets
	// completed before suspension and must be skipped, that target consumes
	// the answer, and later targets pose their own asks (Dig's per-library
	// ask shape).
	sacAns := c.SacPicks
	sacDone := c.SacDone
	sacTarget := c.SacTarget
	c.SacPicks, c.SacDone, c.SacTarget = nil, false, 0
	amount := sacrificeAmount(h, c, sa)
	// An Amount$ of zero has no legal sacrifice and, crucially, no meaningful
	// answer. Do not produce a 0..0 KChoose merely because eligible cards
	// happen to exist (an Optional$ Amount$ X trigger with X=0 has this shape).
	if amount <= 0 {
		return
	}
	optional := sa.ParamStr(cards.PKOptional) == "True"
	strict := optional && sa.ParamStr(cards.PKStrictAmount) == "True"
	// Optional + StrictAmount is not a 0..Amount range: it is specifically
	// "none, or exactly Amount". The KModes answer is consumed below before a
	// possible exact-batch KChoose; keeping it separate prevents a partial
	// sacrifice from taking the card's "if you do" continuation.
	sacOptional, sacOptionalTarget := c.SacOptional, c.SacOptionalTarget
	c.SacOptional, c.SacOptionalTarget = "", 0
	who := Defined(h, c, sa)
	// ShowSacrificedCards$ True (Demonic Covenant's own sacrifice line): the
	// sacrificed cards are REVEALED publicly — one ids-Note naming everything
	// this call sacrificed, the same payload shape effMill's ShowMilledCards$
	// arm emits. Collected across every path below (the answered batch, the
	// re-entry batch and the plain object path) so one Note covers the call.
	show := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKShowSacrificedCards)), "True")
	var sacrificed []state.ObjID
	// sacrificeAnswered sacrifices an answered pick's objects that still sit
	// on the battlefield, in answer order: the answer re-entry's branch, and
	// the resolution kernel's served answer alike.
	sacrificeAnswered := func(ids []state.ObjID) {
		for _, id := range ids {
			if o := g.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				continue
			}
			rememberLKICapture(id)
			sacrificed = append(sacrificed, id)
			h.Emit(events.Sacrifice(id))
		}
	}
	// tapeServed marks a resolution kernel's served answer: the legacy
	// re-entry it stands for starts this walk over with nothing sacrificed
	// yet, so ShowSacrificedCards$' Note names only what is sacrificed from
	// the answered target on -- the same set the re-entry's Note names.
	tapeServed := func() { sacrificed = sacrificed[:0] }
	// A Sacrifice that names neither Defined$ nor ValidTgts$ but a SacValid$
	// other than itself is Forge's default Defined$ You: its controller
	// sacrifices a matching permanent (Braids's "you may sacrifice an
	// artifact, creature, ..."). Only a SacValid$ Self/Card.Self line (or no
	// SacValid$ at all) sacrifices the source object itself. Corpus: 66 such
	// lines, which previously sacrificed the source whatever its type.
	if !TargetsOf(sa).Has(TgtValidPresent) && !DefinedRefOf(sa).Set() {
		if v := strings.TrimSpace(sa.ParamStr(cards.PKSacValid)); v != "" && v != "Self" && v != "Card.Self" {
			who = []state.Target{{Player: c.Controller, IsPlayer: true}}
		}
	}
	for targetIndex, t := range who {
		if sacOptional != "" {
			if targetIndex < sacOptionalTarget {
				continue
			}
			if targetIndex == sacOptionalTarget && sacOptional == "decline" {
				continue
			}
		}
		if sacDone {
			// Re-entry after some target's ask suspended: earlier targets
			// completed on the first pass and must be skipped (re-running
			// them would sacrifice a second batch); the asking target
			// applies its answer; later targets fall through to the normal
			// paths below and pose their own asks (Dig's per-library shape).
			if targetIndex < sacTarget {
				continue
			}
			if targetIndex == sacTarget {
				if t.IsPlayer {
					// Sacrifice exactly the answered cards that still sit on
					// this player's battlefield (a zone check keeps a stray
					// answer from moving an object that left meanwhile), in
					// the player's answer order. One departure snapshot for
					// the whole answered batch (BatchDepartures).
					if len(sacAns) > 0 {
						h.BatchDepartures(sacAns)
						defer h.EndBatchDepartures()
					}
					sacrificeAnswered(sacAns)
				} else if len(sacAns) > 0 {
					// The object-optional ask's sole option was answered
					// "sacrifice it": the object was already zone-checked on
					// the first pass, but re-check here in case it moved.
					sacrificeAnswered([]state.ObjID{t.Obj})
				}
				continue
			}
		}
		if t.IsPlayer {
			// Bounds guard: g.Zone indexes g.zones[zoneIndex(z, p)] and
			// zoneIndex has no bounds check, so an out-of-range target-supplied
			// player id would panic with "index out of range" and halt the
			// table. Player targets normally come from askTarget or AliveFrom
			// and are bounded, but the package's idiom (see cardflow.go and
			// count.go) is not to trust a target blindly.
			if int(t.Player) >= len(g.Players) {
				continue
			}
			// The multi-permanent count defaults to `amount` -- 1 for an
			// ordinary Sacrifice line, or the Amount$ the primitive carries.
			// The Annihilator expansion's generated SA carries its own count
			// in its Annihilator$ marker (cards/keywords.go) and overrides it;
			// the two contexts never coincide in the corpus.
			n := amount
			if ann := sa.ParamStr(cards.PKAnnihilator); ann != "" {
				if v, err := strconv.Atoi(ann); err == nil && v >= 0 {
					n = int32(v)
				}
			}
			ids := append([]state.ObjID(nil), g.Zone(state.ZBattlefield, t.Player)...)
			eligible := make([]state.ObjID, 0, len(ids))
			sc := c.SpecContext(t.Player)
			// SacValid$/ValidCard$ can carry a greatestPower comparison
			// (Consume, Consumed by Greed), which must size the whole pool
			// with layer-derived power, exactly as the Choices$ matcher does;
			// the shared builder returns nil for every other spec. The two
			// spellings never co-occur, so appending both cannot double-bind.
			sc.DerivedPTs = append(sc.DerivedPTs, GreatestPowerDerivedPTs(g, spec, h)...)
			if validCard != "" {
				sc.DerivedPTs = append(sc.DerivedPTs, GreatestPowerDerivedPTs(g, validCard, h)...)
			}
			for _, id := range ids {
				if h.SacrificeBlocked(id, false) {
					// A CantSacrifice restriction (Call for Aid) or face static:
					// the permanent is not a sacrifice candidate at all — not
					// offered, never taken (Annihilator rides this same pool).
					continue
				}
				// ValidCard$, when present, narrows the same pool: a permanent
				// must match BOTH spellings (they never co-occur, so this is
				// just SacValid$ and ValidCard$ in turn).
				matchesSacValid := MatchesSpecCtx(g, spec, id, sc)
				if ref, ok := sacTargetCardReferent(spec, c); ok {
					// A SacValid$ that names the resolution's card TARGET
					// (TargetedCard.Self) must admit exactly that object, not the
					// resolving source the filter grammar's bare Self reads. The
					// player-targeted pool is already this player's battlefield,
					// so id == ref is also the "belongs to the player asked"
					// check. Unknown referent forms stay with MatchesSpecCtx,
					// which fails closed.
					matchesSacValid = id == ref
				}
				if matchesSacValid &&
					(validCard == "" || MatchesSpecCtx(g, validCard, id, sc)) {
					eligible = append(eligible, id)
				}
			}
			minv, maxv := int32(0), int32(0)
			ask := false
			// n is the deterministic/no-host batch. An optional strict batch
			// with too few eligible permanents cannot be paid partially, so it
			// starts at zero rather than falling through to the old first-N path.
			if optional {
				if strict {
					switch {
					case sacOptional == "sacrifice" && targetIndex == sacOptionalTarget:
						// The player accepted the first yes/no step. If there is a
						// genuine identity choice, ask for EXACTLY Amount; when every
						// eligible permanent is required, there is nothing left to ask.
						if int32(len(eligible)) > amount {
							ask = true
							minv, maxv = amount, amount
						}
					case int32(len(eligible)) >= amount:
						// KChoose can express a range but not the disjoint set
						// {0, Amount}, so ask yes/no first and only then (above)
						// choose the exact batch.
						d := &decision.Decision{Player: t.Player, Kind: decision.KModes,
							Min: 1, Max: 1, Source: c.Source, ResumeKind: "sacrifice_optional",
							ResumeSA: sa, ResumeTarget: targetIndex,
							Prompt: "Sacrifice " + strconv.Itoa(int(amount)) + " permanent(s)?",
							Options: []decision.Option{
								{Index: 0, Kind: "mode", Label: "Sacrifice " + strconv.Itoa(int(amount)) + " permanent(s)", Obj: c.Source, Player: t.Player},
								{Index: 1, Kind: "mode", Label: "Don't sacrifice", Obj: c.Source, Player: t.Player},
							}}
						if ans, ok := AskTape(h, d); ok {
							// The resolution kernel's answer in hand: a decline
							// skips this player, an acceptance takes the
							// "sacrifice_optional" re-entry's accepted arm above.
							tapeServed()
							if len(ans) == 0 || ans[0].Index != 0 {
								continue
							}
							if int32(len(eligible)) > amount {
								ask = true
								minv, maxv = amount, amount
							}
						} else if Ask(h, d) == AskAsked {
							return
						} else {
							// R-9 no-host fallback: preserve the old deterministic pick,
							// but only as a complete strict batch.
							h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: t.Player,
								Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
						}
					case int32(len(eligible)) < amount:
						n = 0
					}
				} else if len(eligible) > 0 {
					// A non-strict optional sacrifice permits any number through
					// Amount$, including none.
					ask = true
					maxv = amount
					if maxv > int32(len(eligible)) {
						maxv = int32(len(eligible))
					}
				}
			} else if int32(len(eligible)) > n {
				// Mandatory with a choice: exactly the batch count of the
				// eligible — Amount$, or the Annihilator$ marker's count when
				// the generated expansion carries one.
				ask = true
				minv, maxv = n, n
			}
			// (the remaining mandatory shape — eligible <= amount — sacrifices
			// everything eligible without asking: no choice to record, the
			// effDiscard TgtChoose strict-supersets rule.)
			if ask {
				d := &decision.Decision{Player: t.Player, Kind: decision.KChoose,
					Min:          int(minv),
					Max:          int(maxv),
					Source:       c.Source,
					ResumeKind:   "sacrifice",
					ResumeSA:     sa,
					ResumeTarget: targetIndex,
					Prompt:       sacrificePrompt(optional && !strict, maxv)}
				for _, id := range eligible {
					name := "a permanent"
					if o := g.Obj(id); o != nil && o.Face() != nil {
						name = o.Face().Name
					}
					d.Options = append(d.Options, decision.Option{Index: len(d.Options),
						Kind: "sacrifice", Label: name, Obj: id, Player: t.Player})
				}
				if ans, ok := AskTape(h, d); ok {
					// The resolution kernel's answer in hand: the "sacrifice"
					// re-entry's answered batch.
					tapeServed()
					picks := tapeAnswerObjs(ans)
					if len(picks) > 0 {
						h.BatchDepartures(picks)
						defer h.EndBatchDepartures()
					}
					sacrificeAnswered(picks)
					continue
				}
				if Ask(h, d) == AskAsked {
					return // resolution suspended; the answer re-enters with Ctx.SacPicks set.
				}
				// Fuzz/no-engine host: the deterministic stand-in (R-9) keeps
				// the pre-ask behaviour — the first Amount$ eligible permanents
				// in zone order, so an Optional$ "may sacrifice" plays "do".
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: t.Player,
					Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
				n = maxv
			}
			// One departure snapshot per emitted batch (BatchDepartures): a
			// sacrifice sweep over a lifelink-granting Equipment and its bearer
			// must not make the bearer's CR 603.10a lifelink LKI depend on
			// battlefield order. Asks suspend before any emission, so every
			// suspend-then-resume path still re-collects its batch here.
			batch := make([]state.ObjID, 0, n)
			for i := int32(0); i < n && int(i) < len(eligible); i++ {
				batch = append(batch, eligible[i])
			}
			if len(batch) > 0 {
				h.BatchDepartures(batch)
				defer h.EndBatchDepartures()
			}
			for _, id := range batch {
				rememberLKICapture(id)
				sacrificed = append(sacrificed, id)
				h.Emit(events.Sacrifice(id))
			}
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		if h.SacrificeBlocked(o.ID, false) {
			// A CantSacrifice restriction (or face static): this specific
			// object cannot be sacrificed at all — neither offered to its
			// Optional$ ask nor emitted. The targeting already picked it; the
			// restriction is what stops the pick.
			continue
		}
		// A specific object target is sacrificed as-is: the choice of which
		// object was already made by the effect's targeting, so SacValid$'
		// "which one may be sacrificed" step does not re-filter a concrete
		// object (and would misfire on the corpus's SacValid$ Self lines,
		// where "Self" is not a type the filter grammar knows).
		//
		// Optional$ True on an object target is a real yes/no ("you may
		// sacrifice this artifact"): a 0..1 ask over the object, answered
		// through the same "sacrifice" resume. A host that cannot ask keeps
		// the mandatory sacrifice (the pre-ask behaviour).
		if optional {
			// A concrete target cannot satisfy a strict batch greater than one:
			// it may decline, but it must not sacrifice this one object as a
			// partial payment.
			if strict && amount != 1 {
				continue
			}
			d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose,
				Min: 0, Max: 1, Source: c.Source,
				ResumeKind: "sacrifice", ResumeSA: sa, ResumeTarget: targetIndex,
				Prompt: sacrificePrompt(true, 1)}
			name := "a permanent"
			if o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: 0,
				Kind: "sacrifice", Label: name, Obj: o.ID, Player: o.Controller})
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: the "sacrifice"
				// re-entry's object-optional arm.
				tapeServed()
				if len(tapeAnswerObjs(ans)) > 0 {
					sacrificeAnswered([]state.ObjID{o.ID})
				}
				continue
			}
			if Ask(h, d) == AskAsked {
				return
			}
			// No-host stand-in: the mandatory sacrifice the pre-ask engine
			// made, with the Note that records why the richer path did not run.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: o.Controller,
				Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
		}
		rememberLKICapture(o.ID)
		sacrificed = append(sacrificed, o.ID)
		h.Emit(events.Sacrifice(o.ID))
	}
	if show && len(sacrificed) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, IDs: sacrificed})
	}
}

// ParseDamageUnlessCost reports whether an UnlessCost$ value is the
// damage-payment offer form "DamageYou<N>" and returns N. Recognised: the
// exact spelling (case-insensitive) with a non-negative integer N; anything
// else is not the offer (and falls to the shared gate's pricing).
func ParseDamageUnlessCost(cost string) (int, bool) {
	_, n, ok := strings.Cut(strings.TrimSpace(cost), "DamageYou<")
	if !ok || !strings.HasSuffix(n, ">") {
		return 0, false
	}
	n = strings.TrimSuffix(n, ">")
	v, err := strconv.Atoi(n)
	// N must be a POSITIVE literal: DamageYou<0> (no damage) is not an offer
	// anyone could answer differently, so it fails closed to the ordinary
	// pricing path like every other non-offer spelling.
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// sacrificePrompt renders the player-targeted sacrifice ask's prompt.
func sacrificePrompt(optional bool, n int32) string {
	if optional {
		return "Choose up to " + strconv.Itoa(int(n)) + " permanent(s) to sacrifice, or none"
	}
	return "Choose " + strconv.Itoa(int(n)) + " permanent(s) to sacrifice"
}

// sacrificeAmount resolves Amount$ (default 1). Literals pass through; a
// non-literal resolves through the count evaluator (an SVar name, an inline
// Count$ expression, or {X}). The "X" shape deserves its own arm: on a
// triggered ability Ctx.X is the ability object's own X -- zero, a trigger
// was never paid an X -- so an Amount$ X on a permanent's trigger (Meathook
// Massacre II's "each player sacrifices X creatures") must read the paid X
// off the SOURCE permanent, which CastInfo carried out of the cast onto the
// battlefield object. When that cast X is also zero but an SVar named X
// exists, the evaluator resolves it: Dralnu, Lich Lord's replacement body
// carries SVar:X:ReplaceCount$DamageAmount, naming the replaced event's own
// amount rather than any paid X. An Amount$ that is none of literal, SVar,
// Count$, Sacrificed$ or X is an unknown shape and keeps the
// pre-Amount$-reading behaviour (1) rather than degrading to zero.
func sacrificeAmount(h Host, c *Ctx, sa *cards.SA) int32 {
	raw, ok := sa.Param(cards.PKAmount)
	if !ok {
		return 1
	}
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil {
		return int32(n)
	}
	if raw == "X" {
		if c.X != 0 {
			return c.X
		}
		if o := h.Game().Obj(c.Source); o != nil && o.X != 0 {
			return o.X
		}
		if c.SVars != nil {
			// Dralnu, Lich Lord: Amount$ X with SVar:X:ReplaceCount$DamageAmount
			// — the damage-replacement body's count is the amount of the event
			// being replaced, which main's replacement machinery carries in
			// Ctx.ReplacementAmount and EvalCount's ReplaceCount$ head reads.
			if body, has := c.SVars["X"]; has {
				if v := EvalCount(h, c, body); v != 0 {
					return v
				}
			}
		}
		return 0
	}
	known := strings.HasPrefix(raw, "Count$") || strings.HasPrefix(raw, "Sacrificed$") ||
		strings.HasPrefix(raw, "TriggerCount$") || strings.HasPrefix(raw, "TriggerCountMax$")
	if !known && c.SVars != nil {
		_, known = c.SVars[raw]
	}
	v := Num(h, c, sa, "Amount", 0)
	if !known && v == 0 {
		return 1 // unknown shape: today's fixed-one behaviour, not a silent zero
	}
	return v
}
