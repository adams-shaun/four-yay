package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

var manaRuneNormalizer = strings.NewReplacer("{", "", "}", "", " ", "")

var manaChoiceColours = []string{"W", "U", "B", "R", "G"}

func validManaChoice(s string) bool {
	return len(s) == 1 && strings.ContainsRune("WUBRG", rune(s[0]))
}

// substituteManaChosen replaces the source's as-enters colour in a raw
// Produced$ value.  This is deliberately local to effects: the activation
// path has its own equivalent in rules, while a triggered or nested Mana
// effect reaches effMana without passing through that activation code.
func substituteManaChosen(produced, chosen string) string {
	if !validManaChoice(chosen) {
		return produced
	}
	if produced == "ComboChosen" {
		return "Combo " + chosen
	}
	parts := strings.Fields(produced)
	for i, part := range parts {
		if part == "Chosen" || part == "ChosenColor" {
			parts[i] = chosen
		}
	}
	return strings.Join(parts, " ")
}

// askManaChoice gives a resolution-time Mana effect the same colour-choice
// boundary as an activated mana ability. A Combo's Amount$ is an allocation:
// each selected option is one unit, allowing {U}{R} from Combo Any Amount 2.
// A false Ask is the R-9 no-host path: Any remains the historical colourless
// fallback and a raw Combo list remains the historical full-listed-colours
// fallback. A real host gets a KChoose and rules carries the answer back in
// Ctx.ManaChoice or Ctx.ManaChoices. It returns the production (the
// resolution kernel's answer when it served one), whether that answer is a
// one-unit-per-symbol allocation, and whether the legacy ask suspended.
func askManaChoice(h Host, c *Ctx, sa *cards.SA, produced string) (string, bool, bool) {
	var colours []string
	switch produced {
	case "Any", "Combo Any":
		colours = manaChoiceColours
	default:
		if parsed, ok := ComboColours(produced); ok && len(parsed) > 1 {
			colours = parsed
		}
	}
	if len(colours) <= 1 {
		return produced, false, false
	}
	amount := ManaOf(sa).AmountNum(h, c, 1)
	allocation := strings.HasPrefix(produced, "Combo ") && amount > 1
	if amount <= 0 {
		return produced, false, false
	}
	min, max := 1, 1
	if allocation {
		min, max = int(amount), int(amount)
	}
	chooser := c.Controller
	if recipients := ManaRecipients(h, c, sa); len(recipients) == 1 {
		chooser = recipients[0]
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: min, Max: max,
		Source: c.Source, ResumeKind: "mana_color", ResumeSA: sa,
		Prompt: "Choose a color for the mana"}
	for unit := 0; unit < max; unit++ {
		for _, colour := range colours {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana",
				Label: "Add " + colour, ManaSymbol: colour, Obj: c.Source, Player: chooser})
		}
	}
	// The resolution kernel's answer in hand (lasagna spec §7): the
	// "mana_color" arm's binding, applied here so effMana simply continues.
	// An answer naming no valid colour is the arm's no-binding, on which the
	// legacy re-entry poses the same ask again; so does this loop.
	for {
		ans, ok := AskTape(h, d)
		if !ok {
			break
		}
		if answered, units, ok := manaChoiceProduced(ans); ok {
			return answered, units, false
		}
	}
	if Ask(h, d) == AskAsked {
		return produced, false, true
	}
	return produced, false, false
}

// manaChoiceProduced is the "mana_color" answer's production, exactly as
// the rules arm binds it and effMana's re-entry consumes it: one chosen
// option is a single colour (Ctx.ManaChoice), several are an allocation of
// one unit each (Ctx.ManaChoices). Options whose ManaSymbol is not one
// colour are skipped; ok is false when nothing valid was chosen.
func manaChoiceProduced(chosen []decision.Option) (produced string, allocation, ok bool) {
	if len(chosen) == 1 {
		if validManaChoice(chosen[0].ManaSymbol) {
			return chosen[0].ManaSymbol, false, true
		}
		return "", false, false
	}
	for _, option := range chosen {
		if validManaChoice(option.ManaSymbol) {
			produced += option.ManaSymbol
		}
	}
	return produced, true, produced != ""
}

// ManaProducerTag encodes producer provenance in one exclusive unit tag.
// The Artifact bit composes with Treasure/Cave/Desert rather than replacing
// their historical type: a Treasure Artifact emits ArtifactTreasure, while
// Sol Ring emits Artifact. All mana producers use this same helper (effMana,
// effManaReflected, and cumulative-upkeep AddMana). Snow/typed overlap is
// unmeasured at the corpus pin and retains typed precedence.
func ManaProducerTag(h Host, source state.ObjID) (tag string, snow bool) {
	o := h.Game().Obj(source)
	if o == nil || o.Face() == nil {
		return "", false
	}
	artifact := false
	for _, t := range o.Face().Types {
		if t == "Artifact" {
			artifact = true
			break
		}
	}
	for _, tagWord := range state.TypedManaTags {
		if tagWord == "Artifact" {
			continue
		}
		for _, t := range o.Face().Types {
			if t == tagWord {
				if artifact {
					return "Artifact" + tagWord, false
				}
				return tagWord, false
			}
		}
	}
	if artifact {
		return "Artifact", false
	}
	for _, t := range o.Face().Types {
		if t == "Snow" {
			return "", true
		}
	}
	return "", false
}

func effMana(h Host, c *Ctx, sa *cards.SA) {
	mp := ManaOf(sa)
	noteUnreadParams(h, c, "Mana", mp.Unread)
	produced := mp.Produced
	// A resumed Combo allocation supplies one concrete symbol per unit.
	// Consume it before walking the SA so the same choice is not posed again;
	// its units carry Amount 1 below rather than being multiplied again.
	allocation := len(c.ManaChoices) > 0
	if allocation {
		produced = strings.Join(c.ManaChoices, "")
		c.ManaChoices = nil
		// A resumed colour ask supplies one concrete symbol. Consume the answer
		// before walking the SA so the same choice is not posed again on re-entry.
	} else if validManaChoice(c.ManaChoice) {
		produced = c.ManaChoice
		c.ManaChoice = ""
	} else if o := h.Game().Obj(c.Source); o != nil {
		// Chosen is normally stamped by an as-enters ChooseColor event.  A
		// triggered/nested Mana effect does not pass through rules' activation
		// substitution, so read that same event-backed value here.
		produced = substituteManaChosen(produced, o.ChosenColor)
	}
	if produced == "Any" || produced == "Combo Any" {
		answered, units, asked := askManaChoice(h, c, sa, produced)
		if asked {
			return
		}
		produced, allocation = answered, allocation || units
		if produced == "Any" || produced == "Combo Any" {
			// R-9: an effects host without a decision channel retains the
			// historical deterministic colourless result.
			produced = "C"
		}
	} else if _, ok := ComboColours(produced); ok {
		answered, units, asked := askManaChoice(h, c, sa, produced)
		if asked {
			return
		}
		produced, allocation = answered, allocation || units
	}
	if produced == "" {
		produced = "C"
	}
	// A Combo head is a colour choice, not a request to add every named
	// colour.  A host that could not ask has already taken the R-9 fallback;
	// a resumed answer has been rewritten to one plain symbol above.  Leave a
	// raw Combo list intact here only for the no-host fallback below.
	//
	// Special LastNotedType (Jeweled Amulet: "Add one mana of CARDNAME's
	// last noted type"): the production resolves to the colour the source's
	// last RememberCostMana$ activation paid with (events.Choose's
	// "noted-mana" marker folded into state.Object.LastNotedMana). With no
	// note yet the executor fails closed — the loud Note and no mana the
	// unhandled-Produced arm emits — which for the Amulet is unreachable
	// (its production cost removes the charge counter the noted activation
	// created).
	if strings.EqualFold(produced, "Special LastNotedType") {
		noted := ""
		if o := h.Game().Obj(c.Source); o != nil {
			noted = strings.TrimSpace(o.LastNotedMana)
		}
		if noted == "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unhandled Produced$ Special LastNotedType: no mana noted yet"})
			return
		}
		produced = noted
	}
	// Special EachColorAmong_Valid <spec> (Faeburrow Elder, Tarnation Vista's
	// second ability: "For each color among [matching] permanents you control,
	// add one mana of that color"): a deterministic BATCH, not a choice. The
	// matching permanents' colours (ColorsOf: mana cost or Colors: line,
	// Devoid-aware) are unioned and rendered in fixed WUBRG order, and the
	// tail's per-rune loop adds one unit per distinct colour. The spec is
	// evaluated over the battlefield exactly as ManaReflectedCandidates
	// evaluates its Valid$: MatchesSpecFrom with the resolving source as
	// Self and its controller as You. An empty colour set is a legitimate
	// deterministic no-op ("for each color" over none adds nothing) -- no
	// Note, no mana, like ChangeNum$ 0 Dig. Special EachColorAmong_ExiledWith
	// (Sunbird Effigy: the colours among the cards exiled with the source)
	// resolves the same way over the exiledWithSet. DoubleManaInPool is
	// handled just below. Every OTHER Special selector (EnchantedManaCost,
	// EachColoredManaSymbol_Milled) still falls through to the rune gate's
	// loud Note below.
	// Special DoubleManaInPool (Doubling Cube: "Double the amount of each
	// type of unspent mana you have"): the activator's pool AFTER the cost
	// was paid (a mana ability resolves right after its payment), rendered
	// as one symbol per unit in fixed WUBRGC order, so the per-rune tail
	// adds exactly one more of each unit. An empty pool is a deterministic
	// no-op. The doubled units are ordinary mana: a spend restriction,
	// snow/typed producer provenance or persistence on the original units is
	// a property of how THOSE units were produced, not of their type.
	if strings.EqualFold(produced, "Special DoubleManaInPool") {
		g := h.Game()
		if int(c.Controller) >= len(g.Players) {
			return
		}
		pool := g.Players[c.Controller].Pool
		var b strings.Builder
		for i := 0; i < len(ManaSymbols); i++ {
			for n := pool[state.ManaIndex(ManaSymbols[i])]; n > 0; n-- {
				b.WriteByte(ManaSymbols[i])
			}
		}
		if b.Len() == 0 {
			return
		}
		produced = b.String()
	}
	if strings.EqualFold(produced, "Special EachColorAmong_ExiledWith") {
		syms := eachColorAmongExiledWith(h, c)
		if syms == "" {
			// An empty set is a deterministic no-op, exactly like the
			// EachColorAmong_Valid empty set: "for each color among none"
			// adds nothing, and no card is promised mana it cannot get.
			return
		}
		produced = syms
	}
	if sel, ok := strings.CutPrefix(produced, "Special EachColorAmong_Valid "); ok {
		syms := eachColorAmongValid(h, c, strings.TrimSpace(sel))
		if syms == "" {
			return
		}
		produced = syms
	}
	produced = strings.TrimSpace(strings.TrimPrefix(produced, "Combo "))
	// Strip braces and spaces, then validate every remaining rune before any
	// of them reaches the pool: ComboChosen/ChosenColor/Special ... values
	// that do not name plain mana symbols fail closed instead of splitting
	// into garbage.
	runes := manaRuneNormalizer.Replace(produced)
	for _, r := range runes {
		if !strings.ContainsRune(ManaSymbols, r) {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unhandled Produced$ " + produced})
			return
		}
	}
	amt := mp.AmountNum(h, c, 1)
	if allocation {
		amt = 1
	}
	if amt < 0 {
		amt = 0
	}
	// RestrictValid$ (Master of Dark Rites' "Spend this mana only to cast
	// Vampire, Cleric, and/or Demon spells", Eldrazi Temple, Cavern of
	// Souls, Giada, Shrine of the Forsaken Gods): the produced mana carries
	// its spend restriction on the ManaAdd event itself, so the pool retains
	// the provenance per colour slot and the payment path
	// (manaAvailableFor / restrictValidMatches) can admit it only to
	// matching payments — the same event-level provenance the ManaReflected
	// family and the Tazri batch already ride. A class the payment path
	// cannot evaluate (anything but Spell./Activated.) is still retained --
	// it matches no payment, so the mana is never spendable, the
	// fail-closed direction.
	restriction := mp.RestrictValid
	// AddsNoCounter$ (Cavern of Souls' "that spell can't be countered",
	// Boseiju, Delighted Halfling — 3 corpus files): the produced mana carries
	// its can't-be-countered provenance on the same ManaAdd restriction batch
	// the payment path already reads, so a cast that spends one of these units
	// is marked can't-be-countered at payment time (rules/stack.go captures
	// the consumption, rules/cast.go folds state.FlagNoCounter into the
	// pay-time CastInfo). "True" is the plain flag; Forge's conditional
	// "!Permanent" (Boseiju's instant-or-sorcery mana) is recognised as the
	// NotPermanent condition, evaluated against the paying spell's face. Any
	// other value is a loud Note and NO protection — an unrecognised condition
	// must not silently promise something the engine cannot model.
	noCounter := ""
	switch mp.AddsNoCounter {
	case "":
	case "True":
		noCounter = "True"
	case "!Permanent":
		noCounter = "NotPermanent"
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unhandled AddsNoCounter$ " + mp.AddsNoCounter + "; the mana is ordinary"})
	}
	// PersistentMana$ True (Rousing Refrain, Savage Ventmaw, Klauth, Kessig
	// Naturalist: 23 corpus files / 24 raw lines, every occurrence the
	// literal True): the mana does not empty as steps and phases end (CR
	// 500.4 with the card's exception) until the turn ends. The marker rides
	// the ManaAdd event's Text suffix (events.ManaPersistentText) so
	// events.Apply can keep the units through ManaClear and expire them at
	// TurnChange; it composes with the restriction encoding (Klauth pairs it
	// with RestrictValid$). Any other value is a loud Note and ordinary
	// mana.
	persistent := false
	switch mp.PersistentMana {
	case "":
	case "True":
		persistent = true
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unhandled PersistentMana$ " + mp.PersistentMana + "; the mana is ordinary"})
	}
	// PersistentUntilEndOfCombat$ True is the CR 702.189a Firebending
	// exception ("Until end of combat, you don't lose this mana as steps and
	// phases end"): the units are persistent (they survive the step
	// boundaries within the combat phase) AND carry the combat tally that
	// events.Apply demotes as the end-of-combat step is left. It is implied
	// by, and composes with, the printed keyword expansion (cards/
	// kw_firebending.go); any other value is a loud Note and ordinary mana.
	combat := false
	switch mp.PersistentUntilEndOfCombat {
	case "":
	case "True":
		combat, persistent = true, true
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unhandled PersistentUntilEndOfCombat$ " + mp.PersistentUntilEndOfCombat + "; the mana is ordinary"})
	}
	// CR 107.4h: mana produced by a SNOW permanent is snow mana. A snow unit
	// is tagged in the pool event itself — Counter "S<colour>" — so the pool
	// slot and the parallel snow tally move through one event and a replay
	// derives both identically. The {S} pips a cost may carry are paid only
	// from that tally (rules/mana.go's resolveMana).
	//
	// Task castfilter2: mana produced by a Treasure/Cave/Desert permanent is
	// likewise tagged — Counter "<Tag><colour>" — into Player.TypedMana so
	// the filtered Count$CastTotalManaSpent Treasure/Cave/Desert heads can
	// read the per-unit producer provenance (Marut, Bat Colony, Cataclysmic
	// Prospecting). Artifact provenance composes with those tags in the
	// per-unit counter; it never replaces the Treasure/Cave/Desert type or
	// contributes a second pool unit. An untyped Artifact uses ArtifactC.
	tag, snow := ManaProducerTag(h, c.Source)
	// TriggersWhenSpent$ is retained alongside any spend restriction: the
	// ManaRestriction event encoding carries both the restriction and source
	// provenance, so spendability and the later trigger attribution compose.
	// The spend path dispatches the named rider after the payment completes.
	triggersWhenSpent := mp.TriggersWhenSpent
	// AddsCounters$ (Opal Palace's "If you spend this mana to cast your
	// commander, it enters with ... counters", Biophagus, Animal Attendant,
	// Guildmages' Forum: 4 corpus files) is retained the same way: rules'
	// entry-counter plan re-reads the rider from the producing source's face
	// at the cast spell's battlefield entry, and the source rides the
	// batch's provenance text -- so a rider with no RestrictValid$ still
	// needs a source-bearing batch, which the provenanceOnly gate emits.
	// The rider string itself is not decoded here (the plan owns the
	// grammar); an absent or empty value is not a rider and produces ordinary
	// mana, the fail-closed direction.
	addsCounters := mp.AddsCounters
	provenanceOnly := (triggersWhenSpent != "" || addsCounters != "") && restriction == "" && noCounter == ""
	for _, p := range ManaRecipients(h, c, sa) {
		var emitted [256]bool
		for _, r := range runes {
			// An allocation records one rune per selected unit. Coalesce equal
			// selections into the same ManaAdd batch (four selected W units are
			// one Amount:4 W batch), retaining provenance/restriction semantics
			// while a split U/R still emits one batch for each colour.
			if allocation && emitted[byte(r)] {
				continue
			}
			emitted[byte(r)] = true
			unitAmount := amt
			if allocation {
				unitAmount = 0
				for _, selected := range runes {
					if selected == r {
						unitAmount++
					}
				}
			}
			counter := string(r)
			switch {
			case tag != "":
				counter = tag + counter
			case snow:
				counter = "S" + counter
			}
			ev := events.Event{Kind: events.ManaAdd, Player: p,
				Counter: counter, Amount: unitAmount}
			if noCounter != "" {
				ev.Text = events.ManaRestrictionTextNC(restriction, c.Source, noCounter)
			} else if restriction != "" {
				ev.Text = events.ManaRestrictionText(restriction, c.Source)
			} else if provenanceOnly {
				ev.Text = events.ManaRestrictionText("", c.Source)
			}
			// AddsCounters$ rides the SAME provenance encoding as the restriction
			// (the " ac=" segment ManaAddsCountersText appends), so a replay
			// rebuilds the batch with the producing ABILITY's rider snapshot.
			// Composes with a restriction and with AddsNoCounter$; empty for
			// every non-rider ability, keeping those events byte-identical.
			ev.Text = events.ManaAddsCountersText(ev.Text, addsCounters)
			if combat {
				ev.Text = events.ManaCombatPersistentText(ev.Text)
			} else if persistent {
				ev.Text = events.ManaPersistentText(ev.Text)
			}
			h.Emit(ev)
		}
	}
}

// eachColorAmongValid resolves a Produced$ Special EachColorAmong_Valid <spec>
// selector: the union of the matching battlefield permanents' colours, in the
// controller-relative battlefield scan order (AliveFrom(0) x zone order, the
// same scan ManaReflectedCandidates uses), rendered in fixed WUBRG order via
// ColorMask's table. Colourless contributes nothing ("each color" never
// includes colourless). The spec is evaluated with the resolving source as
// Self and its controller as You, so a bare Permanent.YouCtrl always matches
// the resolving permanent itself while it is on the battlefield. No map
// iteration reaches the result: the union is a bitmask and the output order
// is the fixed WUBRG table.
func eachColorAmongValid(h Host, c *Ctx, spec string) string {
	g := h.Game()
	var mask ColorMask
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if c.MatchSpec(g, spec, id, c.Controller) {
				mask |= ColorMaskOf(g.Obj(id))
			}
		}
	}
	return mask.String()
}

// eachColorAmongExiledWith resolves a Produced$ Special
// EachColorAmong_ExiledWith selector (Sunbird Effigy's "for each color among
// the exiled cards used to craft it"): the union of the colours among the
// cards exiled WITH the resolving source (state.Object.ExiledWith naming it,
// the same exiledWithSet the Defined$ ExiledWith referent reads), rendered in
// fixed WUBRG order. An empty set returns "" -- a deterministic no-op, the
// eachColorAmongValid convention.
func eachColorAmongExiledWith(h Host, c *Ctx) string {
	g := h.Game()
	var mask ColorMask
	for _, t := range exiledWithSet(g, c) {
		mask |= ColorMaskOf(g.Obj(t.Obj))
	}
	return mask.String()
}

// ManaRecipients is the player or players a Mana SA adds its mana for. Forge's
// ManaEffect adds to getDefinedPlayersOrTargeted: with no Defined$ that is the
// activating player; with Defined$ it is each player the selector names, so
// Vernal Bloom's Defined$ TriggeredCardController gives the extra {G} to the
// tapped Forest's controller rather than to the enchantment's. An object
// selector names that object's controller (PlayerOf). A Defined$ that resolves
// to nobody adds nothing, as in Forge (SpellAbilityEffect.getDefinedPlayers has
// no activator fallback): Valleymaker's Defined$ ChosenPlayer must not hand the
// mana to its controller when no player was chosen.
func ManaRecipients(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	if !ManaOf(sa).HasDefined {
		return []state.PlayerID{c.Controller}
	}
	// definedPlayers applies Forge's getDefinedPlayers rule: a remembered CARD
	// contributes a seat only for the RememberedController/Owner spellings,
	// never for the plain Remembered family (a RepeatEach loop's subject).
	return definedPlayers(h, c, sa)
}
