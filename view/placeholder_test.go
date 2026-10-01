package view

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ---------------------------------------------------------------------------
// Defect 1: Forge's CARDNAME/NICKNAME self-reference placeholders.
// ---------------------------------------------------------------------------

// forkedBoltSrc mirrors the user's screenshot card: a spell whose
// SpellDescription$ says "CARDNAME deals 2 damage ...". The placeholder must
// render as the card's own name.
const forkedBoltSrc = `Name:Forked Bolt
ManaCost:R
Types:Instant
A:SP$ DealDamage | ValidTgts$ Any | TgtPrompt$ Select any target | NumDmg$ 2 | SpellDescription$ CARDNAME deals 2 damage divided as you choose among one or two targets.
Oracle:x
`

// ambergrisSrc uses NICKNAME in a trigger's TriggerDescription$, with the
// legend-comma name shape Forge's nickname default is built for.
const ambergrisSrc = `Name:Ambergris, Agent of Destruction
ManaCost:1
Types:Creature
PT:1/1
T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigGain | TriggerDescription$ Whenever NICKNAME attacks, do a thing.
SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:x
`

// TestSubstitutePlaceholders locks the word-boundary, punctuation and
// nickname semantics of the placeholder substitution itself, independent of
// any stack projection.
func TestSubstitutePlaceholders(t *testing.T) {
	cases := []struct {
		name string
		text string
		nm   string
		want string
	}{
		{"cardname full name", "CARDNAME deals 2 damage to any target.", "Forked Bolt", "Forked Bolt deals 2 damage to any target."},
		{"nickname simple", "If NICKNAME is a Scout, do a thing.", "Forked Bolt", "If Forked is a Scout, do a thing."},
		{"nickname legend comma", "Whenever NICKNAME attacks, do a thing.", "Ambergris, Agent of Destruction", "Whenever Ambergris attacks, do a thing."},
		{"possessive", "CARDNAME's power", "Dreadmaw", "Dreadmaw's power"},
		{"punctuation", "Target: CARDNAME.", "Forked Bolt", "Target: Forked Bolt."},
		{"word boundary prefix", "XCARDNAME", "Forked Bolt", "XCARDNAME"},
		{"word boundary suffix", "CARDNAMES", "Forked Bolt", "CARDNAMES"},
		{"no placeholder", "Bolt deals 3 damage.", "Forked Bolt", "Bolt deals 3 damage."},
		{"both placeholders", "CARDNAME gets +1/+1 and NICKNAME gains menace.", "Forked Bolt", "Forked Bolt gets +1/+1 and Forked gains menace."},
		{"empty text", "", "Forked Bolt", ""},
		{"empty name", "CARDNAME cannot be targeted.", "", "CARDNAME cannot be targeted."},
	}
	for _, tc := range cases {
		if got := substitutePlaceholders(tc.text, tc.nm); got != tc.want {
			t.Errorf("%s: substitutePlaceholders(%q, %q) = %q, want %q", tc.name, tc.text, tc.nm, got, tc.want)
		}
	}
}

// TestStackViewSpellTextSubstitutesCardName is the defect-1 end-to-end path
// for a SPELL: the stack entry's Text must render "Forked Bolt deals ..."
// with the placeholder gone.
func TestStackViewSpellTextSubstitutesCardName(t *testing.T) {
	g, id := twoSeatWith(t, forkedBoltSrc)
	events.Apply(g, events.Event{Kind: events.PutOnStack, Obj: id, Player: 0, From: state.ZHand, To: state.ZStack})
	v := Project(g, flatChars{g}, 0, nil)
	if len(v.Stack) != 1 {
		t.Fatalf("stack %+v", v.Stack)
	}
	sv := v.Stack[0]
	const want = "Forked Bolt deals 2 damage divided as you choose among one or two targets."
	if sv.Text != want {
		t.Fatalf("Text = %q, want %q (CARDNAME substituted)", sv.Text, want)
	}
	if strings.Contains(sv.Text, "CARDNAME") || strings.Contains(sv.Text, "NICKNAME") {
		t.Fatalf("Text %q still carries a placeholder", sv.Text)
	}
	if sv.Name != "Forked Bolt" {
		t.Fatalf("Name = %q, want the card's face name", sv.Name)
	}
}

// TestStackViewAbilityTextSubstitutesNickname is the defect-1 path for an
// ABILITY: the substituted name is the SOURCE's face name (abilityName), and
// the trigger line's NICKNAME renders as the first word of that name.
func TestStackViewAbilityTextSubstitutesNickname(t *testing.T) {
	g, id := twoSeatWith(t, ambergrisSrc)
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	events.Apply(g, events.Event{Kind: events.TriggerPush, Player: 0, Obj: id, Amount: 0})
	v := Project(g, flatChars{g}, 1, nil)
	if len(v.Stack) != 1 || v.Stack[0].Kind != "trigger" {
		t.Fatalf("stack %+v", v.Stack)
	}
	sv := v.Stack[0]
	const want = "Whenever Ambergris attacks, do a thing."
	if sv.Text != want {
		t.Fatalf("Text = %q, want %q (NICKNAME substituted)", sv.Text, want)
	}
	if strings.Contains(sv.Text, "CARDNAME") || strings.Contains(sv.Text, "NICKNAME") {
		t.Fatalf("Text %q still carries a placeholder", sv.Text)
	}
	if sv.Name != "Ambergris, Agent of Destruction" {
		t.Fatalf("Name = %q, want the source's face name", sv.Name)
	}
}

// ---------------------------------------------------------------------------
// Defect 3: stack entries for abilities/triggers carry no artwork.
// ---------------------------------------------------------------------------

// TestStackViewAbilityCardFilledFromSource pins the defect-3 fix: an ability
// object on the stack gets its Card from the SOURCE permanent's cardView,
// without changing Kind.
func TestStackViewAbilityCardFilledFromSource(t *testing.T) {
	g, id := twoSeatWith(t, boltSrc)
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if got := g.Obj(id); got.Zone != state.ZBattlefield || len(g.Obj(id).Face().Abilities) == 0 {
		t.Fatalf("fixture: source not on the battlefield with an A: ability (zone %v)", got.Zone)
	}
	// An AbilityPush mint (events/apply.go), not a hand-built object: under
	// the StackKindOf classifier an unstamped fixture whose SA matches no
	// face list falls through the DelayedPush branch and misreads as a
	// trigger, while every engine mint is stamped StackKindActivated.
	events.Apply(g, events.Event{Kind: events.AbilityPush, Obj: id, Player: 0, Amount: 0})
	sids := g.Zone(state.ZStack, 0)
	if len(sids) != 1 || g.Obj(sids[0]).Ability == nil || g.Obj(sids[0]).Source != id {
		t.Fatalf("fixture: no minted ability object on the stack (%v)", sids)
	}
	if ab := g.Obj(sids[0]); !ab.StackKindKnown || ab.StackKind != state.StackKindActivated {
		t.Fatalf("fixture: minted object unstamped (%+v); the Kind assertion needs the stamp", ab)
	}
	v := Project(g, flatChars{g}, 0, nil)
	if len(v.Stack) != 1 {
		t.Fatalf("stack %+v", v.Stack)
	}
	sv := v.Stack[0]
	if sv.Kind != "ability" {
		t.Fatalf("Kind = %q, want \"ability\" (the Kind must not change)", sv.Kind)
	}
	if sv.Card == nil || sv.Card.Name != "Bolt" || sv.Card.ID != id {
		t.Fatalf("Card = %+v, want the source permanent's CardView", sv.Card)
	}
}

// TestStackViewAbilityCardNilWhenSourceHidden pins the redaction half of the
// fix: when the source has left the battlefield for a hidden zone (its
// owner's hand), the stack entry must not leak its card via CardView.
func TestStackViewAbilityCardNilWhenSourceHidden(t *testing.T) {
	g, id := twoSeatWith(t, watcherSrc)
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZHand})
	ab := g.AddObject(nil, 0)
	events.Move(g, ab.ID, state.ZLibrary, state.ZStack)
	ab.Ability = &cards.SA{Kind: "AB", API: "GainLife", Params: map[string]string{"SpellDescription": "Gain 1 life."}}
	ab.Source = id
	v := Project(g, flatChars{g}, 0, nil)
	if len(v.Stack) != 1 {
		t.Fatalf("stack %+v", v.Stack)
	}
	if v.Stack[0].Card != nil {
		t.Fatalf("Card = %+v, want nil for a source in a hidden zone (redaction)", v.Stack[0].Card)
	}
}

// TestStackViewAbilityCardNilWhenSourceAbsent pins the "GONE" half of the
// fix: when the source object cannot be resolved, no Card is fabricated.
func TestStackViewAbilityCardNilWhenSourceAbsent(t *testing.T) {
	g, _ := twoSeatWith(t, watcherSrc)
	ab := g.AddObject(nil, 0)
	events.Move(g, ab.ID, state.ZLibrary, state.ZStack)
	ab.Ability = &cards.SA{Kind: "AB", API: "GainLife", Params: map[string]string{"SpellDescription": "Gain 1 life."}}
	ab.Source = state.ObjID(9999)
	v := Project(g, flatChars{g}, 0, nil)
	if len(v.Stack) != 1 {
		t.Fatalf("stack %+v", v.Stack)
	}
	if v.Stack[0].Card != nil {
		t.Fatalf("Card = %+v, want nil for an unresolvable source", v.Stack[0].Card)
	}
}

// TestDecisionOptionLabelSubstitutesCardName pins fb-20260923T033148Z's
// label half: the engine builds an activated ability's offer label as
// "<face name>: <SpellDescription$>" with the raw description, so Mount
// Doom's damage ability reached the seat as "Mount Doom: CARDNAME deals 1
// damage to each opponent." The projected decision substitutes the
// placeholder with the label's own name prefix; a label without a
// placeholder or without the prefix is unchanged, and the engine's own
// pending decision is not written.
func TestDecisionOptionLabelSubstitutesCardName(t *testing.T) {
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Options: []decision.Option{
		{Index: 0, Kind: "ability", Obj: 7, Label: "Mount Doom: CARDNAME deals 1 damage to each opponent."},
		{Index: 1, Kind: "ability", Obj: 8, Label: "Ambergris, Agent of Destruction: NICKNAME fights."},
		{Index: 2, Kind: "activate", Obj: 7, Label: "Activate Mount Doom for mana"},
		{Index: 3, Kind: "pass", Label: "CARDNAME without a prefix"},
	}}
	cp := copyDecision(d)
	want := []string{
		"Mount Doom: Mount Doom deals 1 damage to each opponent.",
		"Ambergris, Agent of Destruction: Ambergris fights.",
		"Activate Mount Doom for mana",
		"CARDNAME without a prefix",
	}
	for i, w := range want {
		if cp.Options[i].Label != w {
			t.Errorf("projected option %d label = %q, want %q", i, cp.Options[i].Label, w)
		}
	}
	if !strings.Contains(d.Options[0].Label, "CARDNAME") {
		t.Fatalf("copyDecision wrote the engine's own option: %q", d.Options[0].Label)
	}
}
