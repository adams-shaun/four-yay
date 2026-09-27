package cards

import "strings"

// SA is a spell ability, activated ability, static ability or drawback: the
// "SP$ / AB$ / DB$ / ST$ <API> | Param$ value | ..." construct that carries
// almost all card behaviour in the Forge corpus.
type SA struct {
	Kind   string // SP, AB, DB or ST
	API    string
	Params map[string]string
	Sub    *SA // resolved SubAbility$, filled in by link.go
	Line   string

	compiledCatalog *CompiledCatalog
	compiledID      AbilityID
}

// Trigger is a T: line. Execute$ names an SVar holding the effect.
type Trigger struct {
	Mode   string
	Params map[string]string
	Effect *SA
}

// Static is an S: line: a continuous effect or a play restriction.
type Static struct {
	Mode   string
	Params map[string]string
}

// Repl is an R: line: a replacement effect. ReplaceWith$ names an SVar.
type Repl struct {
	Event  string
	Params map[string]string
	With   *SA
}

// Face is one printed face. Most cards have exactly one; ALTERNATE starts
// another.
type Face struct {
	SpecializeColor string // color token from a SPECIALIZE:<COLOR> boundary, if any
	Name            string
	ManaCost        string
	Types           []string
	PT              string
	Loyalty         string
	Defense         string
	Colors          string
	Oracle          string
	Keywords        []string
	Aliases         []string // Universes-Within flavour names a decklist may use
	Abilities       []*SA
	Triggers        []Trigger
	Statics         []Static
	Repls           []Repl
	SVars           map[string]string

	// Derived values, computed once at load (Face.derive), never written
	// into the gob cache: a stale cache decodes these as zero and derive
	// repairs them immediately after decode, so Power(), Toughness() and
	// Cmc() read parsed values instead of re-parsing the printed text on
	// every call.
	power                  int32
	toughness              int32
	characteristicDefining bool
	allCreatureTypesCDA    bool
	cmc                    int32
	manaProduction         ManaProduction

	// colourIdentity is the face's colour identity, a bitmask over the five
	// colours (see ColourIdentity in face.go), derived once at load from the
	// printed text and never written into the gob cache: like the other
	// derived values below it stays unexported so a stale cache decodes it as
	// zero and derive() repairs it immediately after decode.
	colourIdentity uint8
	// compiledTriggerInterests is bound from the immutable catalog at load
	// time. Keeping this hot prefilter beside the legacy face avoids a catalog
	// slice lookup during every trigger scan; it is not serialized.
	compiledTriggerInterests TriggerInterest

	compiledCatalog *CompiledCatalog
	compiledID      FaceID

	// typeStatics* is the layer-4 static probe StaticsMayChangeTypes answers
	// from (see there). typeStaticsFirst/typeStaticsLen record the Statics
	// slice the probe was computed over, so a face whose Statics was appended
	// to after derive (a later keyword link, a struct copy that grew its
	// list) reads as "unknown" -- the conservative answer -- instead of a
	// stale "no". Not serialized, like every derived value above.
	typeStaticsFirst *Static
	typeStaticsLen   int
	// typeStaticsBound is set once the probe below has been computed over
	// the recorded slice; typeStaticsBF/typeStaticsOffBF say whether any
	// static could emit a type change from the battlefield / from any other
	// zone.
	typeStaticsBound bool
	typeStaticsBF    bool
	typeStaticsOffBF bool
	// contStaticsOffBF: some Continuous static could pass rules' source-zone
	// gate off the battlefield (see ContinuousStaticsMayFunctionOffBattlefield).
	contStaticsOffBF bool
	// anyStaticEZ: some static (any Mode$) carries an EffectZone$ key (see
	// StaticsMayNameEffectZone).
	anyStaticEZ bool
}

// typeStaticParams are the static parameter keys whose presence can make
// rules' staticEffects emit a layer-4 (LType) ContinuousEffect: the three
// AddType branch keys, plus AddStaticAbility$, whose granted inner static is
// read from an SVar body and so is conservatively treated as possibly one.
var typeStaticParams = [...]string{"AddType", "AddTypes", "AddAllCreatureTypes", "AddStaticAbility"}

// staticMayChangeTypes reports whether one static carries a typeStaticParams
// key, and whether it could function OFF the battlefield. The off-battlefield
// half over-approximates rules' zone gate (staticZoneAdmits ||
// stackSelfStaticOK): with no EffectZone$ and no ExcludeZone$ a static is
// battlefield-only, unless it is the stack-self shape (PresentZone$ naming
// Stack, or AffectedZone$ Stack). An AddStaticAbility$ grant's inner static
// is only reached once the OUTER static passed that same gate, so the outer
// static's zone keys decide for it too.
func staticMayChangeTypes(st *Static) (has, offBF bool) {
	if st.Mode != "Continuous" {
		// rules' staticEffects skips a non-Continuous static (and so any
		// AddStaticAbility$ grant it carries) before any emission.
		return false, false
	}
	for _, k := range typeStaticParams {
		if _, ok := st.Params[k]; ok {
			has = true
			break
		}
	}
	if !has {
		return false, false
	}
	return true, staticMayFunctionOffBattlefield(st)
}

// staticMayFunctionOffBattlefield over-approximates rules' Continuous source
// zone gate (staticZoneAdmits || stackSelfStaticOK) for a source NOT on the
// battlefield: with no EffectZone$ and no ExcludeZone$ key, staticZoneAdmits
// admits only the battlefield, and stackSelfStaticOK needs PresentZone$
// naming Stack or AffectedZone$ Stack.
func staticMayFunctionOffBattlefield(st *Static) bool {
	_, ez := st.Params["EffectZone"]
	_, xz := st.Params["ExcludeZone"]
	return ez || xz || strings.Contains(st.Params["PresentZone"], "Stack") || st.Params["AffectedZone"] == "Stack"
}

// deriveTypeStatics binds the layer-4 static probe to the face's current
// Statics slice.
func (f *Face) deriveTypeStatics() {
	f.typeStaticsFirst, f.typeStaticsLen = nil, 0
	f.typeStaticsBound, f.typeStaticsBF, f.typeStaticsOffBF, f.contStaticsOffBF = false, false, false, false
	f.anyStaticEZ = false
	if len(f.Statics) == 0 {
		return
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		has, off := staticMayChangeTypes(st)
		f.typeStaticsBF = f.typeStaticsBF || has
		f.typeStaticsOffBF = f.typeStaticsOffBF || off
		if st.Mode == "Continuous" && staticMayFunctionOffBattlefield(st) {
			f.contStaticsOffBF = true
		}
		if _, ok := st.Params["EffectZone"]; ok {
			f.anyStaticEZ = true
		}
	}
	f.typeStaticsFirst, f.typeStaticsLen, f.typeStaticsBound = &f.Statics[0], len(f.Statics), true
}

// StaticsMayChangeTypes reports whether any of the face's own statics could
// make rules' continuous-static scan emit a layer-4 type-changing effect
// (AddType$/AddTypes$/AddAllCreatureTypes$, or an AddStaticAbility$ grant
// whose inner static might) while the face's object sits on the battlefield
// (onBattlefield) or in some other zone. It is CONSERVATIVE: false is
// returned only for a face with no statics at all, or one whose probe was
// derived over exactly the Statics slice it still carries and found no such
// static for that zone class. A face never derived (a test literal, a
// runtime-built face) or whose Statics changed since derive answers true.
// rules' anyLayer4Active uses it to skip rebuilding the whole
// continuous-effect list after an event when no object can be carrying a
// live layer-4 static.
func (f *Face) StaticsMayChangeTypes(onBattlefield bool) bool {
	if f == nil || len(f.Statics) == 0 {
		return false
	}
	if !f.typeStaticsCurrent() {
		return true
	}
	if onBattlefield {
		return f.typeStaticsBF
	}
	return f.typeStaticsOffBF
}

// typeStaticsCurrent reports whether the derived static probe was computed
// over exactly the Statics slice the face carries now.
func (f *Face) typeStaticsCurrent() bool {
	return f.typeStaticsBound && f.typeStaticsLen == len(f.Statics) && f.typeStaticsFirst == &f.Statics[0]
}

// ContinuousStaticsMayFunctionOffBattlefield reports whether any of the face's
// Mode$ Continuous statics could pass rules' source-zone gate while its object
// is in a zone other than the battlefield (an EffectZone$ or ExcludeZone$
// key, or the stack-self PresentZone$/AffectedZone$ Stack shape). rules'
// staticEffects skips an off-battlefield object whose face answers false: no
// static on it could emit. CONSERVATIVE like StaticsMayChangeTypes -- an
// unbound or stale probe answers true.
func (f *Face) ContinuousStaticsMayFunctionOffBattlefield() bool {
	if f == nil || len(f.Statics) == 0 {
		return false
	}
	if !f.typeStaticsCurrent() {
		return true
	}
	return f.contStaticsOffBF
}

// StaticsMayNameEffectZone reports whether any of the face's statics, of
// any Mode$, carries an EffectZone$ key. rules' effectZoneOK admits a static
// whose source is off the battlefield only through a non-empty EffectZone$,
// so the whole-board static collectors skip an off-battlefield face that
// answers false. CONSERVATIVE like StaticsMayChangeTypes -- an unbound or
// stale probe answers true.
func (f *Face) StaticsMayNameEffectZone() bool {
	if f == nil || len(f.Statics) == 0 {
		return false
	}
	if !f.typeStaticsCurrent() {
		return true
	}
	return f.anyStaticEZ
}

func (f *Face) CompiledID() FaceID {
	if f == nil || f.compiledCatalog == nil {
		return 0
	}
	return f.compiledID
}

func (f *Face) CompiledTriggerInterests() (TriggerInterest, bool) {
	if f == nil || f.compiledCatalog == nil || f.compiledID == 0 {
		return 0, false
	}
	return f.compiledTriggerInterests, true
}

func (s *SA) CompiledKind() SAKind {
	if s == nil || s.compiledCatalog == nil {
		return SAKindUnknown
	}
	return s.compiledCatalog.Abilities[s.compiledID-1].Kind
}

func (s *SA) CompiledAPI() APICode {
	if s == nil || s.compiledCatalog == nil {
		return APIUnknown
	}
	return s.compiledCatalog.Abilities[s.compiledID-1].API
}

// ColourIdentity returns the card's colour identity, the bitwise union of
// every face's (so a double-faced card carries the colours of both sides). A
// commander's identity must be a superset of it for the card to be legal.
func (c *Card) ColourIdentity() uint8 {
	var m uint8
	for _, f := range c.Faces {
		m |= f.ColourIdentity()
	}
	return m
}

// SetsName reports whether any face carries a layer-3 rename (CR 613.1d): a
// `S:Mode$ Continuous | SetName$` static, or an api:Animate body with a
// `Name$` rider (the Curse of Fenric family), which effects registers as the
// same LText SetName effect. It is a card-data question the rules engine asks
// ONCE, at genesis, over the match's card pool: a match whose pool has no
// such carrier can never have a layer-3 rename, so the engine skips
// maintaining its rename table entirely (rules/setname.go). It lives here,
// with the IR it reads, rather than in rules -- it is a capability probe over
// printed script text, not a parameter read on a resolving primitive's path.
//
// Like ChangesTypes it is a SUPERSET probe: it scans static params, ability
// params (SP/AB/DB) and the raw SVar bodies, so an Animate reached only
// through a SubAbility chain or an SVar body is still recognised. A false
// positive costs only the rename refresh's own anySetNameActive
// short-circuit; a false negative would leave a resolving effect's name
// filter reading the printed name (the Curse of Fenric's own chapter III
// `Creature.namedFenric` target is exactly that case).
func (c *Card) SetsName() bool {
	if c == nil {
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, st := range f.Statics {
			if _, ok := st.Params["SetName"]; ok {
				return true
			}
		}
		for _, a := range f.Abilities {
			if saSetsName(a) {
				return true
			}
		}
		for _, t := range f.Triggers {
			if saSetsName(t.Effect) {
				return true
			}
		}
		for _, r := range f.Repls {
			if saSetsName(r.With) {
				return true
			}
		}
		for _, body := range f.SVars {
			if strings.Contains(body, "Animate") && strings.Contains(body, "Name$") {
				return true
			}
		}
	}
	return false
}

// saSetsName reports whether an ability (or any of its SubAbility chain) is an
// api:Animate body carrying a Name$ rider. AnimateAll deliberately does NOT
// count: its Name$ stays unread (effects clears the shared parser's field), so
// a pool whose only carrier is an AnimateAll with Name$ correctly needs no
// rename table.
func saSetsName(a *SA) bool {
	for ; a != nil; a = a.Sub {
		if a.API == "Animate" {
			if _, ok := a.Params["Name"]; ok {
				return true
			}
		}
	}
	return false
}

// layer4TypeParams are the Forge parameter names that make a continuous
// effect or an Animate-family effect change an object's TYPES (CR 613.1d /
// 613.1c). They are the keys rules/layers.go branches on when it builds an
// LType ContinuousEffect, plus the Animate API's own Types$ rider.
var layer4TypeParams = map[string]bool{
	"AddType":                true,
	"AddTypes":               true,
	"AddAllCreatureTypes":    true,
	"RemoveCardTypes":        true,
	"RemoveCreatureTypes":    true,
	"RemoveLegendary":        true,
	"RemoveAllAbilities":     true,
	"CharacteristicDefining": true,
	"Types":                  true,
}

// ChangesTypes reports whether any face prints a layer-4 type-changing effect:
// a `S:Mode$ Continuous` static carrying one of layer4TypeParams, an Animate /
// AnimateAll ability (whose `Types$`/`RemoveCardTypes$` riders the layers walk
// folds into an LType effect), or any SVar body that reaches one. It is the
// layer-4 counterpart of SetsName: a card-data question the rules engine asks
// ONCE, at genesis, over the match's card pool, so a match whose pool has no
// such carrier skips maintaining its derived-type table entirely
// (rules/layer4types.go). A card outside the pool can never register one of
// these effects.
//
// It is deliberately a SUPERSET probe: it scans static params, ability params
// (SP/AB/DB) and the raw SVar bodies, so an Effect/Trigger body that names its
// real work in an SVar (a `StaticAbilities$ X` whose X is a `S:...AddTypes$`
// line, an `Execute$ Y` trigger) is still recognised. A false positive costs
// only the refresh's own anyLayer4Active short-circuit; a false negative would
// silently miss the derived type at a target offer.
func (c *Card) ChangesTypes() bool {
	if c == nil {
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, st := range f.Statics {
			for k := range st.Params {
				if layer4TypeParams[k] {
					return true
				}
			}
		}
		for _, a := range f.Abilities {
			if saChangesTypes(a) {
				return true
			}
		}
		for _, t := range f.Triggers {
			if saChangesTypes(t.Effect) {
				return true
			}
		}
		for _, r := range f.Repls {
			if saChangesTypes(r.With) {
				return true
			}
		}
		for _, body := range f.SVars {
			if strings.Contains(body, "Animate") {
				return true
			}
			for k := range layer4TypeParams {
				if strings.Contains(body, k+"$") {
					return true
				}
			}
		}
	}
	return false
}

// saChangesTypes reports whether an ability (or any of its SubAbility chain)
// is an Animate-family effect or carries a layer-4 type parameter. The API
// spellings are the two the corpus uses: Animate (a single target) and
// AnimateAll (a ValidCards$ set).
func saChangesTypes(a *SA) bool {
	for ; a != nil; a = a.Sub {
		if a.API == "Animate" || a.API == "AnimateAll" {
			return true
		}
		for k := range a.Params {
			if layer4TypeParams[k] {
				return true
			}
		}
	}
	return false
}

// Card is one script file. AlternateMode describes how its faces relate;
// name-characteristic rules distinguish split cards from transforming DFCs.
type Card struct {
	Path          string
	AlternateMode string
	Faces         []*Face
}

// Diag is a non-fatal parse complaint. The whole corpus is expected to produce
// under ten of these; a jump means either a parser regression or an upstream
// data change worth looking at.
type Diag struct {
	Path string
	Msg  string
}

func newFace() *Face { return &Face{SVars: map[string]string{}} }
