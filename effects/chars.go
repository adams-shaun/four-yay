package effects

import "github.com/adams-shaun/gorge/state"

// Chars is ONE object's characteristics as the game currently sees them: its
// printed values after every applicable continuous effect in CR 613 layer
// order, then layer-7d counters (rules-engine refactor spec W1d,
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md section
// 5). It is the record rules' layer walk builds -- rules.Derived is an alias
// of this type -- so an effect, a filter and the layer walk read one shape
// and cannot disagree about which characteristic a continuous effect moved.
//
// Read it through Host.Chars (HostRead) for the current, layer-derived
// answer. A printed value is read only through PrintedChars, so a reader of
// the printed face says so at the call site; reading o.Face() for a
// characteristic a continuous effect can change is the "printed vs derived"
// bug class this record exists to close.
//
// Keywords and Types are borrowed, read-only views: see Host.Chars for how
// long they stay valid. A caller that keeps either across another Chars
// query or an emit copies it.
type Chars struct {
	Power, Toughness int32
	// BasePower/BaseToughness are the object's BASE power and toughness: the
	// value through layer 7b (CR 613.4) -- the printed or characteristic-
	// defining value, after a 7b setting effect, and BEFORE any 7c modify or
	// any 7d counter. The base filter predicates (`basePowerEQ1`,
	// `powerGTbasePower`) read these; a 7c pump or a +1/+1 counter must move
	// Power/Toughness but never BasePower/BaseToughness. A face-down
	// battlefield permanent's base is its CR 708.5 face-down P/T (or a
	// FaceDownPower$ override), the same basis the walk starts from.
	BasePower, BaseToughness int32
	Keywords                 []string
	Types                    []string
	// AllCreatureTypes records the layer-4 semantic grant independently of
	// the materialized subtype vocabulary in Types.
	AllCreatureTypes bool
	// Name is the current layer-3 name. SetName$ overwrites the printed name.
	Name string
	// Text is the object's current CR 613.1d text: its printed Oracle text
	// ("" while a battlefield object is face down, CR 708.5) after every
	// applicable layer-3 effect in timestamp order -- a TextSet outright
	// replacement (api:ExchangeTextBox) then each TextFrom/TextTo
	// whole-word substitution (api:ChangeText). It is the one place the
	// engine renders an object's changed rules text.
	Text string
	// Colors is the object's current colour set as WUBRG letters (CR 613.1e):
	// its face's colours (ColorsOf, which already applies Devoid) then every
	// applicable layer-5 effect in timestamp order -- an OverwriteColors grant
	// replaces the set so far, a plain one extends it. "" is a colourless
	// object, not "no read": a battlefield land reads "".
	Colors string
	// Controller is the object's current controller (CR 108.4), the same
	// answer rules' controllerOf gives: the event-backed control (a control
	// change is an event, so layer 2 is already folded into the object) or,
	// inside an Effect observer's match, that registration's bound
	// controller.
	Controller state.PlayerID
}

// PrintedChars is the object's PRINTED characteristics: its current face as
// printed, with no continuous effect, no counter and no face-down basis
// applied. It is the one explicit printed read (Chars' doc): a caller that
// wants what the game currently sees reads Host.Chars instead. A faceless or
// nil object reads the zero Chars. Types and Keywords alias the face's own
// slices and must not be written.
func PrintedChars(o *state.Object) Chars {
	if o == nil || o.Face() == nil {
		return Chars{}
	}
	f := o.Face()
	p, t := int32(f.Power()), int32(f.Toughness())
	return Chars{Power: p, Toughness: t, BasePower: p, BaseToughness: t,
		Keywords: f.Keywords, Types: f.Types, Name: f.Name, Text: f.Oracle,
		Colors: ColorsOf(o), Controller: o.Controller}
}
