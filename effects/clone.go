package effects

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Clone", effClone) }

// effClone implements DB$ Clone (api:Clone), CR 613.1a's layer-1 copy: an
// EXISTING permanent becomes a copy of another object. It is the ONE
// primitive both clone routes call -- the standalone "CARDNAME becomes a copy
// of target creature" family (Vesuvan Doppelganger, Lazav, Body Double) and,
// once the ETB-copy replacement ticket lands, the "you may have it enter as a
// copy" family (Vizier of Many Faces), whose body is the same DB$ Clone with
// CloneTarget$ ReplacedCard.
//
// Two operands, matching Forge's CloneEffect:
//
//   - the copy SOURCE, i.e. the object whose characteristics are copied:
//     Defined$ when present, else the SA's own chosen targets (the
//     ValidTgts$ "copy target creature" shape), else a Choices$ pick.
//   - the BECOME operand, i.e. the object that turns into the copy:
//     CloneTarget$ when present, else the SA's own source (Self) -- "this
//     permanent becomes a copy".
//
// The copy itself is one events.ClonePermanent event, folded in Apply onto
// the target object's CopyFace basis. Routing the basis through
// state.Object.Face() is what makes every reader in the tree (name, types,
// keywords, colours, P/T, abilities, triggers, statics, mana production) see
// the copied characteristics by construction (CR 707.2) instead of each call
// site having to consult the layer system.
//
// The characteristic EXCEPTIONS are separate continuous effects at their own
// CR 613 layers, registered against the become object, so the copied face
// stays the source's printed face and the walk settles the exceptions in
// order: AddTypes$/RemoveCardTypes$/RemoveCreatureTypes$ are layer 4,
// SetColor$ is layer 5, AddKeywords$ is layer 6, SetPower$/SetToughness$ are
// layer 7b. NewName$ rides the event (the copy's name) and GainThisAbility$
// True keeps the resolving ability and its source face's SVar table on the copy.
//
// Duration$ is honoured through the ordinary continuous-effect lifetime: a
// permanent copy (no Duration$, or Permanent) is cleared by the become
// object leaving the battlefield (CR 400.7, Move clears the basis); an
// UntilEndOfTurn copy is cleared at that cleanup (EndOfTurnCleanup's
// clone sweep); UntilYourNextTurn / UntilTheEndOfYourNextTurn use the
// engine's turn boundary; UntilUnattached clears when the become object is no
// longer attached (the clone sweep's attached check). UntilFacedown expires
// at the become object's turn-down, and UntilTargetedUntaps at the copied
// target's actual untap. A duration this build
// cannot place gets one loud Note and the copy lasts until the object leaves
// the battlefield.
func effClone(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	cp := CloneOf(sa)
	noteUnreadParams(h, c, "Clone", cp.Unread)
	if c.CloneEnter.ETB {
		// The ETB election is answered before the move. A decline is a real
		// answer, not the deterministic Choices$ fallback.
		if !c.CloneEnter.ChoiceValid {
			// No recorded election: a non-cast entry (reanimation, blink,
			// ChangeZone) of any carrier, or a cast whose body the ETB
			// whitelist declined (an out-of-scope rider -- Vesuva's
			// IntoPlayTapped$, Cursed Mirror's Duration$). Those paths keep
			// the loud unimplemented-API fallback they had before the ETB
			// route existed -- the copy is never silently dropped (the
			// etbclone1 scope boundary).
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "unimplemented API " + sa.API})
			return
		}
		if c.CloneEnter.Choice == 0 {
			return
		}
		// The election was made while the spell was announced, but a player
		// may respond before it resolves. Recheck both battlefield presence
		// and the body selector now: the chosen creature may have left, or
		// changed controller and no longer satisfy Choices$ Creature.OppCtrl.
		// An invalidated optional template means the entering object simply
		// enters as itself, never as a copy of an object from a former zone.
		if !cloneETBTemplateLegal(g, c, cp) {
			return
		}
	}

	// Copy SOURCE. CopyFromChosenName$ uses the name recorded on the
	// equipment by NameCard, not a battlefield target. The universe is the
	// same immutable card set the name decision offered.
	chosenName := ""
	if cp.CopyFromChosenName {
		if o := g.Obj(c.Source); o != nil {
			chosenName = o.ChosenName
		}
		found := false
		for _, card := range g.NameUniverse {
			if len(card.Faces) > 0 && card.Faces[0].Name == chosenName {
				found = true
				break
			}
		}
		if !found {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "Clone CopyFromChosenName$ has no matching named card in the universe; no copy"})
			return
		}
	}
	var source []state.Target
	// chosenPick is the Choices$ pick's object id, recorded so ExcludeChosen$
	// (Forge's "each OTHER creature you control becomes a copy") can drop it
	// from the become pool. Zero on every other source route (there is no
	// chosen object to exclude).
	var chosenPick state.ObjID
	spec := cp.Defined
	switch {
	case chosenName != "":
		// A name has no source ObjID. The copied face is resolved in Apply
		// from the universe carried by the game, keyed by this chosen name.
		source = []state.Target{{Obj: c.Source}}
	case c.CloneEnter.ETB:
		source = []state.Target{{Obj: c.CloneEnter.Choice}}
	case spec != "":
		ts, ok := knownDefinedTargets(h, c, spec)
		if !ok {
			// Fail closed: a source this build cannot resolve is one loud Note
			// and NO copy, never a silent fall-through to a wrong object (the
			// CopyPermanent convention -- a wrong copy is worse than none).
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Clone source " + spec + " is not resolvable; no copy"})
			return
		}
		source = ts
	case cp.Choices != "":
		// Choices$ <filter> is Forge's mid-resolution chooser for the copy
		// source (CR 706.2): "you may have this creature enter as a copy of
		// any creature on the battlefield". A real host gets the per-player
		// pick over the eligible pool; a no-host run (an effects test double,
		// a fuzz run) keeps the deterministic first-eligible stand-in under a
		// Note (the R-9 no-ask contract).
		spec := cp.Choices
		// ChoiceZone$ names the zone the Choices$ pick draws from. An absent
		// value keeps the historical battlefield pool byte-for-byte; a value
		// this build does not implement (or a filter head that cannot resolve
		// over a plain card object) FAILS CLOSED: one loud Note and no copy,
		// never a silent fall-through to a battlefield object (the CloneZone$
		// convention -- a wrong copy is worse than none).
		zone, zoneOK := cp.ChoiceZoneKind, cp.ChoiceZoneOK
		optional := cp.ChoiceOptional
		if !zoneOK {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Clone ChoiceZone$ " + cp.ChoiceZone +
					" is not a zone this build can choose from; no copy"})
			return
		} else {
			cands := cloneChoiceCandidates(h, c, spec, zone)
			if len(cands) == 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "Clone Choices$ " + spec + " has no eligible object; no copy"})
				return
			}
			optionKind := "permanent"
			if zone != state.ZBattlefield {
				optionKind = "card"
			}
			prompt := "Choose an object to copy"
			if title := cp.ChoiceTitle; title != "" {
				prompt = title
			}
			min := 1
			if optional {
				min = 0
			}
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: min, Max: 1,
				Source: c.Source, ResumeKind: "clone_choice", ResumeSA: sa, Prompt: prompt}
			for i, t := range cands {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: optionKind,
					Label: objName(h.Game(), t.Obj), Obj: t.Obj, Player: c.Controller})
			}
			if optional {
				// ChoiceOptional$ True: an explicit decline. Option indices stay
				// the candidate indices, so the decline option rides LAST and an
				// existing carrier's permanent option order is unchanged.
				d.Options = append(d.Options, decision.Option{Index: len(cands), Kind: "decline",
					Label: "No — do not copy", Player: c.Controller})
			}
			if ans, ok := AskTape(h, d); ok {
				// Answered in place. A zero id is a real decline when the
				// ask offered one (ChoiceOptional$ True), and otherwise a
				// malformed or empty answer -- one loud Note and no copy,
				// never a silent fall-through to an object the chooser did
				// not name.
				pick := state.ObjID(0)
				if len(ans) > 0 {
					pick = ans[0].Obj
				}
				if pick == 0 {
					if optional {
						return
					}
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
						Text: "Clone Choices$ answer named no object; no copy"})
					return
				}
				source = []state.Target{{Obj: pick}}
				chosenPick = pick
			} else {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "Clone Choices$ picks the first eligible object (no engine host to ask)"})
				source = []state.Target{{Obj: cands[0].Obj}}
				chosenPick = cands[0].Obj
			}
		}
	default:
		// No Defined$/Choices$: the SA's own chosen target is the object to
		// copy (the "target creature you control becomes a copy of target
		// creature" family has one target being both source and become).
		for _, t := range c.Targets {
			if !t.IsPlayer {
				source = append(source, t)
			}
		}
		if len(source) == 0 {
			return
		}
	}

	// BECOME operand(s).
	become, ok := cloneBecome(h, c, cp)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "Clone CloneTarget$ " + cp.CloneTarget +
				" is not resolvable; no copy"})
		return
	}
	if len(become) == 0 {
		return
	}

	// ExcludeChosen$ True drops the Choices$ pick from the become pool: Forge's
	// "each OTHER creature you control becomes a copy of that creature"
	// (Sakashima's Will, Brudiclad). Without it the chosen source is paired with
	// itself and takes a replay-visible self-copy ClonePermanent plus copy
	// marker -- a wrong copy, not a harmless one. An emptied pool is no copy,
	// recorded loudly (never a silent no-op).
	if cp.ExcludeChosen && chosenPick != 0 {
		kept := become[:0]
		for _, b := range become {
			if !b.IsPlayer && b.Obj == chosenPick {
				continue
			}
			kept = append(kept, b)
		}
		become = kept
		if len(become) == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Clone ExcludeChosen$ left no become object; no copy"})
			return
		}
	}

	// Eligible (source, become) pairs, computed ONCE, before the Optional$
	// ask. A pair whose source object is gone, or whose become object is no
	// longer on the battlefield, cannot act -- the copy loop at the bottom
	// would silently skip it -- so asking the may-copy election over a board
	// where every pair is dead poses a decision whose EVERY answer does
	// nothing (Sarkhan Soul Aflame leaves the battlefield while its
	// Dragon-entry trigger waits on the stack; findings-sol1 MAJOR). Filtering
	// here means the election is only posed when a copy can actually be made,
	// and the bottom loop walks the same pre-filtered pairs the ask was built
	// from -- one eligibility home, never two.
	type clonePair struct{ src, become state.Target }
	var pairs []clonePair
	for _, t := range source {
		if t.IsPlayer {
			continue
		}
		if chosenName == "" {
			srcObj := g.Obj(t.Obj)
			if srcObj == nil || srcObj.Face() == nil {
				continue
			}
			if zone := cp.CloneZone; zone != "" && !strings.EqualFold(srcObj.Zone.String(), zone) {
				continue
			}
		}
		for _, b := range become {
			if b.IsPlayer {
				continue
			}
			if obj := g.Obj(b.Obj); obj == nil || obj.Zone != state.ZBattlefield {
				continue
			}
			pairs = append(pairs, clonePair{src: t, become: b})
		}
	}
	if len(pairs) == 0 {
		return
	}

	// Optional$ True: the copier -- the resolving controller, who for every
	// corpus carrier is also the become object's controller -- takes the real
	// may-copy election (ticket api-clone-trigger-copy; Sarkhan Soul Aflame's
	// "you may have CARDNAME become a copy of it"), answered in place via
	// AskTape; the answered decline returns without copying. A no-host run (an effects test double, a fuzz run)
	// keeps the deterministic take stand-in the pre-election build shipped,
	// byte-identical (the same convention the optional-discard family
	// records) -- a "may" that cannot ask never wedges.
	if !c.CloneEnter.ETB && cp.Optional {
		prompt := "You may have a permanent become a copy?"
		if ob := g.Obj(pairs[0].become.Obj); ob != nil && ob.Face() != nil {
			prompt = "You may have " + ob.Face().Name + " become a copy?"
		}
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
			Source:     c.Source,
			ResumeKind: "clone", ResumeSA: sa,
			Prompt: prompt,
			Options: []decision.Option{
				{Index: 0, Kind: "yes", Label: "Yes — make the copy", Player: c.Controller},
				{Index: 1, Kind: "no", Label: "No", Player: c.Controller},
			}}
		if ans, ok := AskTape(h, d); ok {
			// Answered in place: copy on a yes, decline otherwise.
			if len(ans) == 0 || ans[0].Kind != "yes" {
				return
			}
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Clone Optional$ resolved as take (no engine host to ask)"})
		}
	}

	// Collect the modifier registrations once; every become object shares
	// them. An unreadable modifier is one Note per call (never per object).
	// The compiled lists are shared by every resolution, so each one handed
	// to a continuous effect is cloned (no state object aliases the record).
	addTypes := cp.TypeAdds
	nonLegendary := cp.NonLegendary
	removeSubTypes := cp.RemoveSubTypes
	addAbilities := cp.AddAbilities
	// Resolve the named grants against the resolving face before replacing its
	// copy basis. A copied object's SVar table is not the grantor's table.
	grantTable := c.SVars
	if grantTable == nil {
		if original := g.Obj(c.Source); original != nil && original.Face() != nil {
			grantTable = original.Face().SVars
		}
	}
	grantSVars := make(map[string]string)
	for _, name := range cp.AddSVars {
		if raw, ok := grantTable[name]; ok {
			grantSVars[name] = raw
		}
	}
	var grantTriggers []*cards.Trigger
	for _, name := range cp.AddTriggers {
		if raw, ok := grantTable[name]; ok {
			if tr, ok := cards.ParseTriggerLine(raw); ok {
				linkGrantedTrigger(&tr, grantTable)
				grantTriggers = append(grantTriggers, &tr)
			}
		}
	}
	addKeywords := cp.AddKeywords
	// PumpKeywords$ is the Clone sibling of CopyPermanent's temporary-keyword
	// rider: the copy gains the named keywords for the PumpDuration$ window,
	// independent of Clone's own Duration$ (the copy's lifetime). Absent
	// PumpDuration$ means "for as long as the copy exists", so the grant rides
	// the copy unit's own lifetime; a present PumpDuration$ gets its own unit
	// key so an EOT grant can expire while a permanent copy survives (The
	// Fourteenth Doctor).
	pumpKeywords := cp.PumpKeywords
	pumpDuration := cp.PumpDuration
	newName := cp.NewName
	gainThisAbility := cp.GainThisAbility
	removeCardTypes := cp.RemoveCardTypes
	removeCreatureTypes := cp.RemoveCreatureTypes
	setPowerPresent, setPower := clonePT(h, c, cp.SetPower)
	setToughPresent, setTough := clonePT(h, c, cp.SetToughness)
	colorSpec := cp.SetColor
	var setColors []string
	var setColorPresent bool
	if colorSpec != "" {
		if !cp.SetColorOK {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Clone SetColor$ " + colorSpec + " is not a colour this build can set; the copy keeps its colours"})
		} else {
			// SetColor$ is an overwrite (CR 613.1e "becomes"); an empty parse
			// (Colorless) is an overwrite to colourless, which the layer walk
			// honours through OverwriteColors with an empty AddColors. The
			// PRESENCE bit is tracked separately from the letters for exactly
			// that case: keying the registration on len(setColors) would make
			// SetColor$ Colorless a silent no-op.
			setColors = cp.SetColors
			setColorPresent = true
		}
	}
	// Unknown rider values stay loud rather than pretending to apply.
	var unread []string
	var lostSVars, lostTriggers []string
	for _, name := range cp.AddSVars {
		if _, ok := grantSVars[name]; !ok {
			lostSVars = append(lostSVars, name)
		}
	}
	for _, name := range cp.AddTriggers {
		raw, ok := grantTable[name]
		if !ok {
			lostTriggers = append(lostTriggers, name)
		} else if _, ok := cards.ParseTriggerLine(raw); !ok {
			lostTriggers = append(lostTriggers, name)
		}
	}
	if len(lostSVars) > 0 {
		unread = append(unread, "AddSVars$ "+strings.Join(lostSVars, ","))
	}
	if len(lostTriggers) > 0 {
		unread = append(unread, "AddTriggers$ "+strings.Join(lostTriggers, ","))
	}
	if cp.IntoPlayTappedSet && !c.CloneEnter.ETB {
		unread = append(unread, "IntoPlayTapped$ "+cp.IntoPlayTapped+" (no entry)")
	}
	// Preserve every named static's original body: the event fold installs
	// it on the copy face, where ALL static readers use the printed S: path.
	var staticBodies []string
	for _, name := range cp.StaticNames {
		raw := grantTable[name]
		if CloneStaticGrantReadable(grantTable, name) {
			staticBodies = append(staticBodies, raw)
		} else {
			unread = append(unread, "AddStaticAbilities$ "+name)
		}
	}
	// Emitted once per call, after an Optional$ yes (the decline path
	// returned before this point) or on the no-host take.
	if len(unread) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "Clone does not read: " + strings.Join(unread, ", ")})
	}

	dur := cp.Duration
	// permanent is the "no Duration$/Permanent" classification; every clone
	// effect is registered with Permanent=false (see reg below), so the flag
	// itself is not carried onto the effects -- the no-duration case is simply
	// a unit with no expiry field, kept until the become object leaves.
	_, untilEOT, untilTurn, untilUnattached, durNote := cloneDuration(dur)
	// PumpKeywords$' own lifetime: an explicit PumpDuration$ wins, an absent
	// one rides the copy unit (dur/untilEOT/untilTurn). EOT maps to UntilEOT;
	// a next-turn spelling leaves UntilTurn zero for AddContinuous to derive
	// from Duration; an unresolvable spelling is one loud Note plus the
	// copy's own lifetime (never a silent over-extension past the copy).
	pumpDur, pumpEOT, pumpTurn := dur, untilEOT, untilTurn
	if len(pumpKeywords) > 0 && pumpDuration != "" {
		switch {
		case IsNextTurnDuration(pumpDuration):
			pumpDur, pumpEOT, pumpTurn = pumpDuration, false, 0
		case strings.EqualFold(pumpDuration, "EOT") || strings.EqualFold(pumpDuration, "EndOfTurn") ||
			strings.EqualFold(pumpDuration, "UntilEndOfTurn"):
			pumpDur, pumpEOT, pumpTurn = "", true, 0
		default:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Clone PumpDuration$ " + pumpDuration + " is not implemented; the keyword lasts as long as the copy"})
		}
	}
	// Same shape as the unread-modifier Note above: emitted once per call.
	if durNote != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: durNote})
	}

	cloneAbilityIndex := int32(-1)
	cloneTriggerIndex := int32(-1)
	if gainThisAbility {
		if original := g.Obj(c.Source); original != nil && original.Face() != nil {
			// Forge's CloneEffect resolves GainThisAbility$ on
			// sa.getRootAbility(): the ROOT of the resolving chain, not the
			// innermost Clone body. A body reached through SubAbility$
			// (Kimahri's RonsoCounter -> RonsoTap -> RonsoClone, Volatile
			// Chimera's activated ChooseCard -> DBClone) is a chain;
			// identifying only the innermost sa with a printed ability or a
			// trigger's Effect misses it. Recover the root by walking each
			// printed trigger's and ability's own Sub chain -- the same
			// pointer-identity contract rules.findTriggerForAbilityFace
			// uses -- and index THAT root. When the root is a trigger Forge
			// appends root.getTrigger().copy(...); when it is an
			// activated/spell ability, root.copy(...). The fold indexes the
			// become object's top-face Triggers/Abilities, the same face
			// scanned here, so emitter and fold agree by construction.
			// Merged/mutated pile under-card triggers and
			// has-all-abilities-of granted wrappers are not reached (see the
			// commit message); they stay on the old AppendNothing path.
			f := original.Face()
			for i := range f.Triggers {
				if t := f.Triggers[i].Effect; t != nil && cloneChainContains(t, sa) {
					cloneTriggerIndex = int32(i + 1)
					break
				}
			}
			if cloneTriggerIndex < 0 {
				for i, ability := range f.Abilities {
					if cloneChainContains(ability, sa) {
						cloneAbilityIndex = int32(i + 1)
						break
					}
				}
			}
		}
	}
	for _, p := range pairs {
		t := p.src
		srcObj := g.Obj(t.Obj)
		if chosenName == "" && (srcObj == nil || srcObj.Face() == nil) {
			continue
		}
		b := p.become
		obj := g.Obj(b.Obj)
		if obj == nil || obj.Zone != state.ZBattlefield {
			continue
		}
		// One ClonePermanent event per (source, become) pair; the fold
		// snapshots the source's printed face onto the become object.
		var abilitySVars map[string]string
		if f := obj.Face(); f != nil {
			abilitySVars = f.SVars
		}
		ev := events.Event{Kind: events.ClonePermanent, Obj: b.Obj,
			IDs: []state.ObjID{t.Obj}, Player: c.Controller, Text: newName}
		if chosenName != "" {
			ev.Counter = "chosen-name"
			ev.Text = chosenName
		} else if gainThisAbility {
			if cloneTriggerIndex > 0 {
				ev.Counter = "gain-this-trigger"
				ev.Amount = cloneTriggerIndex
			} else {
				ev.Counter = "gain-this-ability"
				ev.Amount = cloneAbilityIndex
			}
		}
		h.Emit(ev)
		for _, raw := range staticBodies {
			h.Emit(events.Event{Kind: events.CloneStatic, Obj: b.Obj, Text: raw})
		}

		// Modifier layers, scoped to the become object (Card.Self with
		// Source = its own id, the effPump convention). The lifetime is
		// ALWAYS the source-leaves rule (Permanent=false): CR 400.7 makes
		// the object a new object the instant it leaves the battlefield, so
		// the copy and its modifiers must not follow it. active() drops the
		// unit on the source-leaves check and effectMoveSweep removes it from
		// e.continuous when the become object leaves (the CR 611.2a
		// "Permanent" flag would keep it applying to a re-entered object).
		regDur := func(ce state.ContinuousEffect, d string, eot bool, turn int32) {
			ce.Source = b.Obj
			ce.Affects = "Card.Self"
			ce.Controller = c.Controller
			ce.Duration = d
			ce.Permanent = false
			ce.UntilEOT = eot
			ce.UntilTurn = turn
			ce.CloneTarget = b.Obj
			if strings.EqualFold(d, "UntilTargetedUntaps") {
				ce.CloneDurationTarget = t.Obj
			}
			h.AddContinuous(ce)
		}
		reg := func(ce state.ContinuousEffect) {
			regDur(ce, dur, untilEOT, untilTurn)
		}
		if ce, ok := cloneTypeEffect(cp, addTypes, addKeywords, removeCardTypes, removeCreatureTypes, nonLegendary, removeSubTypes); ok {
			reg(ce)
		}
		if len(grantSVars) > 0 || len(grantTriggers) > 0 {
			reg(state.ContinuousEffect{Layer: state.LAbilities, AddSVars: grantSVars,
				SVars: grantTable, TriggerGrantor: b.Obj})
			for _, tr := range grantTriggers {
				copy := *tr
				reg(state.ContinuousEffect{Layer: state.LAbilities, AddTrigger: &copy,
					SVars: grantTable, TriggerGrantor: b.Obj})
			}
		}
		if len(addAbilities) > 0 {
			// The source table belongs to the become object's original face,
			// not the copied face. Capture it before ClonePermanent replaces
			// that face; the grant expires with the same copy unit.
			reg(state.ContinuousEffect{Layer: state.LAbilities, AddAbilities: slices.Clone(addAbilities),
				SVars: abilitySVars})
		}
		if setColorPresent {
			reg(state.ContinuousEffect{Layer: state.LColor, AddColors: slices.Clone(setColors), OverwriteColors: true})
		}
		if len(addKeywords) > 0 {
			reg(state.ContinuousEffect{Layer: state.LAbilities, AddKeywords: slices.Clone(addKeywords)})
		}
		if len(pumpKeywords) > 0 {
			regDur(state.ContinuousEffect{Layer: state.LAbilities, AddKeywords: slices.Clone(pumpKeywords)}, pumpDur, pumpEOT, pumpTurn)
		}
		if setPowerPresent || setToughPresent {
			reg(state.ContinuousEffect{Layer: state.LPT, Sub: state.SubSet, HasSet: true,
				SetPower: setPower, SetToughness: setTough,
				SetPowerPresent: setPowerPresent, SetToughnessPresent: setToughPresent,
				StaticSet: true})
		}
		if raw := cp.AttachedTo; raw != "" {
			attached := false
			for _, target := range DefinedSpec(h, c, raw) {
				if target.IsPlayer || target.Obj == 0 {
					continue
				}
				if bear := g.Obj(target.Obj); bear != nil && bear.Zone == state.ZBattlefield {
					h.Emit(events.Event{Kind: events.Attach, Obj: b.Obj, IDs: []state.ObjID{target.Obj}})
					attached = true
					break
				}
			}
			if !attached {
				h.Emit(events.Event{Kind: events.Note, Obj: b.Obj, Text: "Clone AttachedTo$ has no battlefield bearer"})
			}
		}
		if cp.FaceDown && !obj.FaceDown {
			h.Emit(events.Event{Kind: events.TurnFaceDown, Obj: b.Obj})
		}
		if cp.KeepFacedownFalse && obj.FaceDown {
			h.Emit(events.Event{Kind: events.TurnFaceUp, Obj: b.Obj})
		}
		// A standalone copy did not enter. Entry tapping is applied by the
		// replacement body only; ordinary Clone never changes tap status.
		if c.CloneEnter.ETB && cp.IntoPlayTappedTrue {
			h.Emit(events.Event{Kind: events.Tap, Obj: b.Obj})
		}
		// The layer-1 LCopy MARKER owns the copy's lifetime. It is always
		// registered (even when no modifier effect is), so rules' clone
		// sweep has exactly one owner per copy to expire and can drop the
		// marker's sibling effects with it. UntilUnattached is enforced by
		// EndOfTurnCleanup's attached check, which reads the marker's
		// Duration; every other duration rides UntilEOT/UntilTurn or the
		// source-leaves rule.
		//
		// The marker also CARRIES the copy (source id, NewName$,
		// GainThisAbility$) so that expiring one unit on an object that
		// carries ANOTHER live unit re-bases the object onto the
		// survivor instead of clearing the shared CopyFace basis.
		_ = untilUnattached
		reg(state.ContinuousEffect{Layer: state.LCopy, CloneSource: t.Obj,
			CloneName: newName, CloneChosenName: chosenName,
			CloneStaticBodies: staticBodies, CloneGainThisAbility: gainThisAbility,
			CloneAbilityIndex: cloneAbilityIndex, CloneTriggerIndex: cloneTriggerIndex})
	}
}

// cloneChainContains reports whether sa is the root ability or any SubAbility
// beneath it. It is the pointer-identity walk that recovers Forge's
// sa.getRootAbility(): cards.SA links SubAbility$ downward only, so the root
// is found by walking each printed candidate's own chain.
func cloneChainContains(root, sa *cards.SA) bool {
	for cur := root; cur != nil; cur = cur.Sub {
		if cur == sa {
			return true
		}
	}
	return false
}

// CloneStaticGrantReadable is shared by the ETB offer gate and the clone
// resolver. A named static must parse as an actual S: body; the fold installs
// its entire mode/parameter set through the ordinary printed-static path.
func CloneStaticGrantReadable(svars map[string]string, name string) bool {
	statics, ok := cards.ParseStaticLines(svars[name])
	return ok && len(statics) > 0
}

// cloneNames splits a comma-separated SVar grant in printed order.
func cloneNames(raw string) []string {
	var names []string
	for _, name := range strings.Split(raw, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// cloneBecome resolves CloneTarget$. Absent means Self (the resolving
// ability's own source object) -- "this permanent becomes a copy". The
// named forms reuse the same Defined$ referent grammar the source half uses.
func cloneBecome(h Host, c *Ctx, cp *CloneParams) ([]state.Target, bool) {
	if c.CloneEnter.BecomeValid {
		return []state.Target{{Obj: c.CloneEnter.Become}}, true
	}
	spec := cp.CloneTarget
	if spec == "" {
		if c.Source == 0 {
			return nil, true
		}
		return []state.Target{{Obj: c.Source}}, true
	}
	if strings.EqualFold(spec, "Self") {
		return []state.Target{{Obj: c.Source}}, true
	}
	// CloneTarget$ Valid <spec>: every battlefield object the filter admits
	// (the "each other creature you control becomes a copy" shape), through
	// the same battlefield sweep Defined's Valid form uses.
	if rest, ok := strings.CutPrefix(spec, "Valid "); ok {
		return battlefieldValidTargets(h, c, strings.TrimSpace(rest)), true
	}
	return knownDefinedTargets(h, c, spec)
}

// cloneETBTemplateLegal revalidates the recorded ETB-copy template at
// replacement resolution. ETB choices are announced before the spell moves to
// the stack, so the cast-time option list is not sufficient: priority can
// remove the chosen object or change its controller before this replacement
// applies. The same CloneETBSelectorMatches used for the option list
// rechecks both the filter grammar and Mockingbird's captured cast spend.
func cloneETBTemplateLegal(g *state.Game, c *Ctx, cp *CloneParams) bool {
	o := g.Obj(c.CloneEnter.Choice)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return false
	}
	return CloneETBSelectorMatches(g, cp.Choices, c.CloneEnter.Choice, c.TableSpecContext(c.Controller))
}

// cloneChoiceZone classifies ChoiceZone$, the zone a Choices$ pick draws its
// pool from. An absent value (the historical body) and Battlefield both keep
// the battlefield pool; Graveyard and Exile are the two off-battlefield zones
// the standalone route supports. Any other value fails closed (ok == false),
// so an unimplemented zone can never silently win a battlefield pool. The two
// named zone spellings are matched case-insensitively, the same discipline
// cloneDuration keeps for Duration$.
func cloneChoiceZone(raw string) (state.Zone, bool) {
	switch cloneChoiceZoneCodes.Code(string(strings.ToLower(strings.TrimSpace(raw)))) {
	case cloneChoiceZoneBattlefield:
		return state.ZBattlefield, true
	case cloneChoiceZoneGraveyard:
		return state.ZGraveyard, true
	case cloneChoiceZoneExile:
		return state.ZExile, true
	default:
		return 0, false
	}
}

// cloneChoiceCandidates resolves a Choices$ <filter> pick to every eligible
// object of the named zone in deterministic scan order (alive players in seat
// order, each player's zone in insertion order). The first element is
// exactly the object the pre-ask build's deterministic first-eligible pick
// chose, so a no-host run keeps its byte-identical stand-in; a real host gets
// the whole pool to pose as options. Nil means nothing matched.
//
// The battlefield sweep is byte-for-byte the historical one, so every
// existing Battlefield carrier replays identically. An off-battlefield filter
// whose head is not resolvable over a plain card object (Kaya Spirits'
// `Card.TriggeredCards`, a trigger-Remembered referent this grammar cannot
// bind) matches nothing here, which the caller records as one loud Note and
// no copy -- the fail-closed landing, never a battlefield fall-through.
func cloneChoiceCandidates(h Host, c *Ctx, spec string, zone state.Zone) []state.Target {
	g := h.Game()
	filter := spec
	if !strings.Contains(filter, ".") && !strings.HasPrefix(filter, "Card") {
		// A bare type word is a Card-basis filter ("Creature.Other" is
		// already a basis; "Creature" alone is not).
		filter = "Card." + filter
	}
	sc := c.SpecContext(c.Controller)
	var out []state.Target
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(zone, p) {
			o := g.Obj(id)
			if o == nil {
				continue
			}
			if MatchesObjectCtx(g, filter, o, sc) {
				out = append(out, state.Target{Obj: id})
			}
		}
	}
	return out
}

// clonePT resolves a compiled SetPower$/SetToughness$ modifier through the
// shared numeric grammar (literal, X, or an SVar name). present reports
// whether the parameter was given at all, so a setter that names only one
// characteristic leaves the other alone (the continuous-effect StaticSet
// contract).
func clonePT(h Host, c *Ctx, p ParamText) (present bool, value int32) {
	if !p.Present {
		return false, 0
	}
	return true, numText(h, c, p, 0)
}

// cloneDuration maps a DB$ Clone Duration$ value onto the continuous-effect
// lifetime fields. untilTurn is left zero for the AddContinuous call to fill
// from the live rotation (the UntilYourNextTurn path). durNote, when
// non-empty, is the one loud Note for a duration this build cannot place.
//
// An UNKNOWN duration is deliberately NOT permanent: it gets the
// source-leaves lifetime (until the become object leaves the battlefield)
// rather than lasting for the rest of the game, so a value this build cannot
// place never silently over-extends a copy. Measured corpus values at
// FORGE_REF (raw `DB$ Clone` lines): UntilEndOfTurn 33, UntilYourNextTurn 5,
// UntilUnattached 5, one each of UntilTargetedUntaps, UntilNextEndStep,
// UntilHostLeavesPlay, UntilFacedown and EOT; 105 lines carry no Duration$
// (a permanent copy).
func cloneDuration(dur string) (permanent, untilEOT bool, untilTurn int32, untilUnattached bool, note string) {
	switch cloneDurationCodes.Code(string(strings.ToLower(strings.TrimSpace(dur)))) {
	case cloneDurationPermanent:
		return true, false, 0, false, ""
	case cloneDurationUntilEndOfCombat:
		// durationTiming's combat scope: dropped by EndOfTurnCleanup on the
		// same turn (the engine's UntilEndOfCombat reclamation).
		return false, false, 0, false, ""
	case cloneDurationUntilEndOfTurn:
		return false, true, 0, false, ""
	case cloneDurationUntilYourNextTurn:
		// AddContinuous computes the real turn boundary from Duration.
		return false, false, 0, false, ""
	case cloneDurationUntilNextEndStep:
		// The engine's until-next-end-step window is this turn's cleanup, the
		// same mapping effects.effectUntilEOT uses for this spelling (the one
		// corpus carrier is niko_light_of_hope).
		return false, true, 0, false, ""
	case cloneDurationUntilUnattached:
		return false, false, 0, true, ""
	case cloneDurationUntilHostLeavesPlay:
		// Exactly the source-leaves lifetime the default arm gives an unknown
		// duration, so no Note is needed (secret_invasion).
		return false, false, 0, false, ""
	case cloneDurationUntilEvent:
		// Settled on the actual turn-down or untap event, not at cleanup.
		return false, false, 0, false, ""
	default:
		return false, false, 0, false,
			"Clone Duration$ " + dur + " is not implemented; the copy lasts until the object leaves the battlefield"
	}
}

// splitAmp splits a Forge "&"-compound type list ("Shapeshifter & Rogue").
func splitAmp(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "&")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type cloneChoiceZoneCode uint16

const (
	cloneChoiceZoneBattlefield cloneChoiceZoneCode = iota + 1
	cloneChoiceZoneGraveyard
	cloneChoiceZoneExile
)

var cloneChoiceZoneCodes = state.NewStrCodes(
	state.StrEntry[cloneChoiceZoneCode]{Key: "", Val: cloneChoiceZoneBattlefield},
	state.StrEntry[cloneChoiceZoneCode]{Key: "battlefield", Val: cloneChoiceZoneBattlefield},
	state.StrEntry[cloneChoiceZoneCode]{Key: "graveyard", Val: cloneChoiceZoneGraveyard},
	state.StrEntry[cloneChoiceZoneCode]{Key: "exile", Val: cloneChoiceZoneExile},
)

type cloneDurationCode uint16

const (
	cloneDurationPermanent cloneDurationCode = iota + 1
	cloneDurationUntilEndOfCombat
	cloneDurationUntilEndOfTurn
	cloneDurationUntilYourNextTurn
	cloneDurationUntilNextEndStep
	cloneDurationUntilUnattached
	cloneDurationUntilHostLeavesPlay
	cloneDurationUntilEvent
)

var cloneDurationCodes = state.NewStrCodes(
	state.StrEntry[cloneDurationCode]{Key: "", Val: cloneDurationPermanent},
	state.StrEntry[cloneDurationCode]{Key: "permanent", Val: cloneDurationPermanent},
	state.StrEntry[cloneDurationCode]{Key: "untilendofcombat", Val: cloneDurationUntilEndOfCombat},
	state.StrEntry[cloneDurationCode]{Key: "untileadofturn", Val: cloneDurationUntilEndOfTurn},
	state.StrEntry[cloneDurationCode]{Key: "untilendofturn", Val: cloneDurationUntilEndOfTurn},
	state.StrEntry[cloneDurationCode]{Key: "eot", Val: cloneDurationUntilEndOfTurn},
	state.StrEntry[cloneDurationCode]{Key: "untilyournextturn", Val: cloneDurationUntilYourNextTurn},
	state.StrEntry[cloneDurationCode]{Key: "untiltheendofyournextturn", Val: cloneDurationUntilYourNextTurn},
	state.StrEntry[cloneDurationCode]{Key: "untilyournextendstep", Val: cloneDurationUntilNextEndStep},
	state.StrEntry[cloneDurationCode]{Key: "untilnextendstep", Val: cloneDurationUntilNextEndStep},
	state.StrEntry[cloneDurationCode]{Key: "untilunattached", Val: cloneDurationUntilUnattached},
	state.StrEntry[cloneDurationCode]{Key: "untilhostleavesplay", Val: cloneDurationUntilHostLeavesPlay},
	state.StrEntry[cloneDurationCode]{Key: "untilfacedown", Val: cloneDurationUntilEvent},
	state.StrEntry[cloneDurationCode]{Key: "untiltargeteduntaps", Val: cloneDurationUntilEvent},
)

// cloneTypeEffect is a Clone copy's layer-four modifier, if any. A copy
// exception "except it has changeling" (Omni-Changeling, Moritte) is a
// copiable value, so its characteristic-defining ability applies in layer four
// before EVERY ordinary layer-four effect, regardless of timestamps -- an
// earlier creature-type strip or setter must survive it (CR 707.9b,
// 613.2/613.3). It is therefore marked CDAAllCreatureTypes, which rules' type
// walk seeds ahead of the walk, not AddAllCreatureTypes, which would apply at
// the copy's timestamp after an older strip.
func cloneTypeEffect(cp *CloneParams, addTypes, addKeywords []string, removeCardTypes, removeCreatureTypes, nonLegendary, removeSubTypes bool) (state.ContinuousEffect, bool) {
	grantsChangeling := slices.ContainsFunc(addKeywords, func(k string) bool {
		return cards.KeywordHeadIDOf(k) == cards.KeywordHeadIDOf("Changeling")
	})
	if len(addTypes) == 0 && !removeCardTypes && !removeCreatureTypes && !nonLegendary && !removeSubTypes && !cp.SetCreatureTypes && !grantsChangeling {
		return state.ContinuousEffect{}, false
	}
	return state.ContinuousEffect{Layer: state.LType, AddTypes: slices.Clone(addTypes), CDAAllCreatureTypes: grantsChangeling,
		RemoveCardTypes: removeCardTypes, RemoveCreatureTypes: removeCreatureTypes, SetCreatureTypes: cp.SetCreatureTypes,
		RemoveLegendary: nonLegendary, RemoveSubTypes: removeSubTypes}, true
}
