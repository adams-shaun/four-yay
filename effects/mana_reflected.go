package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("ManaReflected", effManaReflected) }

// manaReflectedOrder is the fixed candidate order. Never a map, so the
// offered option list and deterministic single-colour pick are stable.
const manaReflectedOrder = "WUBRGC"

var manaReflectedReplacer = strings.NewReplacer("{", "", "}", "", " ", "")

// ManaReflectedCandidates resolves which mana symbols an "AB$ ManaReflected"
// ability can add, in the fixed manaReflectedOrder order. Two params drive it:
//
//   - Valid$ names the objects the ability reflects FROM. A "Defined.<name>"
//     value resolves that name through the ordinary Defined machinery (the
//     resolving context's Remembered/Targets/Source), so Chrome Mox's
//     "Defined.Imprinted" resolves to whatever imprint record the engine
//     keeps; every other value is an object spec matched against the
//     battlefield in AliveFrom(0) x zone order via MatchesSpecFrom, with the
//     resolving source as the filter's "Self" and its controller as "You".
//
//   - ReflectProperty$ decides what is read: "Produce" unions what the
//     matching objects' mana abilities could produce; "Is" reads the matching
//     objects' colours; "Produced" reads the mana types captured from the
//     mana event that caused a TapsForMana trigger. Produced intentionally
//     does not scan Defined$ objects: those values name the player receiving
//     the additional mana, not an object (Mana Flare/Kinnan).
//
// ColorOrType$ "Color" keeps coloured symbols. "Type" additionally admits
// colourless, but only when a reflected mana ability can actually produce
// {C}; it never manufactures colourless merely because the parameter appears.
//
// An unknown element in either parameter degrades to an empty set, which the
// executor reports as a Note rather than inventing mana.
func ManaReflectedCandidates(h Host, c *Ctx, sa *cards.SA) []string {
	property := strings.TrimSpace(sa.Params["ReflectProperty"])
	widenType := strings.TrimSpace(sa.Params["ColorOrType"]) == "Type"
	if property == "Produced" {
		set := map[byte]bool{}
		for _, r := range c.TriggerMana {
			if strings.ContainsRune("WUBRGC", r) && (widenType || r != 'C') {
				set[byte(r)] = true
			}
		}
		var out []string
		for _, symbol := range manaReflectedOrder {
			if set[byte(symbol)] {
				out = append(out, string(symbol))
			}
		}
		return out
	}
	if property != "Produce" && property != "Is" {
		return nil
	}
	spec := strings.TrimSpace(sa.Params["Valid"])
	if spec == "" {
		return nil
	}
	var objs []state.ObjID
	if rest, ok := strings.CutPrefix(spec, "Defined."); ok {
		sel := strings.TrimSpace(rest)
		if extras, handled := reflectedDefinedExtras(h, c, sel); handled {
			objs = extras
		} else {
			// A "Defined.<name>" spec: resolve <name> through the ordinary
			// Defined resolver against this resolution's own context. The scan
			// copy carries only the Defined$ param, so the resolver cannot read
			// anything else off the script line.
			scan := *sa
			scan.Params = map[string]string{"Defined": sel}
			for _, t := range Defined(h, c, &scan) {
				if !t.IsPlayer {
					objs = append(objs, t.Obj)
				}
			}
		}
	} else {
		g := h.Game()
		for _, p := range g.AliveFrom(0) {
			for _, id := range g.Zone(state.ZBattlefield, p) {
				if c.MatchSpec(g, spec, id, c.Controller) {
					objs = append(objs, id)
				}
			}
		}
	}
	produce := property == "Produce"
	set := map[string]bool{}
	for _, id := range objs {
		o := h.Game().Obj(id)
		if o == nil {
			continue
		}
		var syms string
		if produce {
			syms = producibleSymbols(o)
		} else {
			syms = ColorsOf(o)
		}
		for _, r := range syms {
			set[string(r)] = true
		}
	}
	if !widenType {
		delete(set, "C")
	}
	var out []string
	for _, s := range manaReflectedOrder {
		if set[string(s)] {
			out = append(out, string(s))
		}
	}
	return out
}

// producibleSymbols unions the mana symbols one permanent's mana abilities
// could produce. It mirrors effMana's own classifier exactly: a blank or
// "Any"/"Combo Any" Produced$ is every colour; a "Combo <colours>" shape is
// exactly those colours (effects.ComboColours rejects every other combo
// word); a plain brace/space-stripped rune list contributes its coloured and
// colourless runes. Amount$ and RestrictValid$ belong to the mana the
// reflected ability itself produces, not the source ability it examines, so
// they intentionally do not affect this candidate census.
func producibleSymbols(o *state.Object) string {
	f := o.Face()
	if f == nil {
		return ""
	}
	var set uint8
	for _, ma := range f.ManaAbilities() {
		p := strings.TrimSpace(ma.Params["Produced"])
		switch {
		case p == "" || p == "Any" || p == "Combo Any":
			for _, r := range "WUBRG" {
				set |= 1 << uint(strings.IndexRune(manaReflectedOrder, r))
			}
			continue
		}
		if cols, ok := ComboColours(p); ok {
			for _, col := range cols {
				set |= 1 << uint(strings.IndexByte(manaReflectedOrder, col[0]))
			}
			continue
		}
		runes := manaReflectedReplacer.Replace(p)
		for _, r := range runes {
			if i := strings.IndexRune(manaReflectedOrder, r); i >= 0 {
				set |= 1 << uint(i)
			}
		}
	}
	var b strings.Builder
	for i := range manaReflectedOrder {
		if set&(1<<uint(i)) != 0 {
			b.WriteByte(manaReflectedOrder[i])
		}
	}
	return b.String()
}

// reflectedDefinedExtras resolves the "Defined.<selector>" shapes the
// ordinary Defined resolver does not carry, for the reflected-mana family
// only; handled=false hands every other selector to the ordinary resolver
// (Self, Imprinted, Remembered, the rest), so the two cannot disagree about
// who answers a selector both know. "ValidGraveyard <spec>" scans every
// alive seat's graveyard in AliveFrom(0) order (Urborg's "a color among cards
// in your graveyard"; the optional trailing spec narrows the scan);
// "Sacrificed" reads the resolution's Remembered list (the land the
// Sac<1/Land> cost paid -- Squandered Resources); "Untapped" scans the
// controller's untapped battlefield permanents (Benthic Explorers); and
// "ExiledWith" scans exile for cards whose ExiledWith provenance names the
// source (Pit of Offerings' imprint family).
func reflectedDefinedExtras(h Host, c *Ctx, sel string) ([]state.ObjID, bool) {
	g := h.Game()
	spec := ""
	if i := strings.IndexByte(sel, ' '); i >= 0 {
		spec = strings.TrimSpace(sel[i+1:])
		sel = strings.TrimSpace(sel[:i])
	}
	switch sel {
	case "ValidGraveyard":
		var out []state.ObjID
		for _, q := range g.AliveFrom(0) {
			for _, id := range g.Zone(state.ZGraveyard, q) {
				if spec != "" && !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
					continue
				}
				out = append(out, id)
			}
		}
		return out, true
	case "Sacrificed":
		var out []state.ObjID
		for _, t := range c.Remembered {
			out = append(out, t.Obj)
		}
		return out, true
	case "Untapped":
		var out []state.ObjID
		for _, q := range g.AliveFrom(0) {
			for _, id := range g.Zone(state.ZBattlefield, q) {
				if o := g.Obj(id); o != nil && o.Controller == c.Controller && !o.Tapped {
					out = append(out, id)
				}
			}
		}
		return out, true
	case "ExiledWith":
		var out []state.ObjID
		for _, q := range g.AliveFrom(0) {
			for _, id := range g.Zone(state.ZExile, q) {
				if o := g.Obj(id); o != nil && o.ExiledWith == c.Source {
					out = append(out, id)
				}
			}
		}
		return out, true
	}
	return nil, false
}

// effManaReflected implements "AB$ ManaReflected": add Amount$ (default one)
// mana of a colour the reflected set offers. The rules engine
// (rules/mana_activation.go) asks the colour whenever more than one is
// available and re-enters this effect with Produced$ overridden to the chosen
// letter, so the override is the authoritative single-colour path.
// RestrictValid$ is retained on each ManaAdd event, rather than discarded when
// the pool coalesces equal-colour mana; rules payment consumes that provenance
// only for a matching spell or activated ability.
//
// An empty set adds nothing and records a Note: Exotic Orchard tapped with no
// opponent land in play, or Chrome Mox with no imprinted card recorded, must
// not invent a colour.
func effManaReflected(h Host, c *Ctx, sa *cards.SA) {
	produced := strings.TrimSpace(sa.Params["Produced"])
	amount := Num(h, c, sa, "Amount", 1)
	if amount < 0 {
		amount = 0
	}
	restriction := strings.TrimSpace(sa.Params["RestrictValid"])
	// Producer-type provenance (task ctms): the tag is the ABILITY SOURCE's
	// printed Treasure/Cave/Desert/Snow types -- the same tag effMana stamps --
	// because "mana from a <Type>" is mana PRODUCED BY a permanent of that
	// type (Marut's official ruling defines "mana from a Treasure" exactly
	// that way, and CR 107.4h's snow mana is the same producer-side
	// definition), and the producer of a ManaReflected body's mana is the
	// permanent resolving the body. Neither the object the body reflects FROM
	// (Cactus Preserve reflecting a Mountain's red produces DESERT mana; Exotic
	// Orchard reflecting a snow-covered mountain produces PLAIN red) nor the
	// recipient the body pays TO (the Produced trigger's Defined$
	// TriggeredActivator -- the ManaAdd event's Player, who owns the unit at
	// spend time) changes who produced it. Pinned in both directions by
	// rules/mana_reflected_attribution_test.go. Read ONCE per resolution -- the
	// source cannot change across the colour branches.
	tag, snow := ManaProducerTag(h, c.Source)
	manaAdd := func(player state.PlayerID, color string) {
		if amount == 0 {
			return
		}
		counter := color
		switch {
		case tag != "":
			counter = tag + color
		case snow:
			counter = "S" + color
		}
		ev := events.Event{Kind: events.ManaAdd, Player: player, Counter: counter, Amount: amount}
		if restriction != "" {
			ev.Text = events.ManaRestrictionText(restriction, 0)
		}
		h.Emit(ev)
	}
	recipient := c.Controller
	if strings.TrimSpace(sa.Params["ReflectProperty"]) == "Produced" {
		// On this corpus shape Defined$ names who receives the additional
		// mana (TriggeredActivator, TriggeredCardController, or You), unlike
		// Produce/Is where Valid$ names reflected objects. Resolve those three
		// roles locally so the ordinary Defined grammar is not widened for
		// unrelated effects.
		switch strings.TrimSpace(sa.Params["Defined"]) {
		case "TriggeredActivator":
			if c.TriggerPlayer.IsPlayer {
				recipient = c.TriggerPlayer.Player
			}
		case "TriggeredCardController":
			if o := h.Game().Obj(c.TriggerCard); o != nil {
				recipient = o.Controller
			}
		}
	}
	// The rules-engine colour-ask path re-enters with Produced$ set to one
	// plain letter.
	if len(produced) == 1 && strings.ContainsRune(ManaSymbols, rune(produced[0])) {
		manaAdd(recipient, produced)
		return
	}
	cols := ManaReflectedCandidates(h, c, sa)
	// The rules mana-activation path resolves this ability with Produced$
	// already rewritten to one letter (the guard above), so it never reaches
	// the ask. Reached standalone -- a DB$/SP$ ManaReflected body resolved on
	// its own -- a multi-colour set is a REAL mid-resolution colour choice: a
	// real host is asked, and only a host that cannot answer (or an empty
	// option list) keeps the deterministic first-candidate stand-in with its
	// R-9 Note.
	if answered := c.ManaReflectedColor; answered != "" {
		// The answered ask's re-entry. Consume and clear the transport (fx42
		// scoping: a nested ManaReflected below poses its own ask), accept the
		// colour only when this resolution still offers it, and degrade a
		// malformed/off-list answer to the first candidate rather than
		// inventing a colour the candidates never named.
		c.ManaReflectedColor = ""
		// The answer is carried as the structured mana symbol, not the option
		// label: labels are presentation-only (ManaSymbol on decision.Option).
		col := answered
		for _, cand := range cols {
			if cand == col {
				manaAdd(recipient, col)
				return
			}
		}
		if len(cols) > 0 {
			manaAdd(recipient, cols[0])
		}
		return
	}
	switch len(cols) {
	case 0:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ManaReflected found no mana to reflect"})
	case 1:
		manaAdd(recipient, cols[0])
	default:
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
			ResumeKind: "manareflected", ResumeSA: sa,
			Prompt: "Choose a colour of mana to reflect", Source: c.Source}
		for i, col := range cols {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "mana", Obj: c.Source, Label: "Add " + col, ManaSymbol: col})
		}
		if Ask(h, d) == AskAsked {
			return
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "chose first reflected colour " + cols[0] + " (no ask possible)"})
		manaAdd(recipient, cols[0])
	}
}
