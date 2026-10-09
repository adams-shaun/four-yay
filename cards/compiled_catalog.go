package cards

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
)

const CompiledCatalogSchema uint32 = 1

type CatalogIdentity struct {
	Schema     uint32
	CorpusHash [sha256.Size]byte
}

type Span struct {
	Start uint32
	Count uint32
}

type StringRef struct {
	Offset uint32
	Length uint32
}

type ParamRow struct {
	Key   StringID
	Value StringID
}

type KeywordRow struct {
	Head StringID
	Full StringID
}

type SVarRow struct {
	Name StringID
	Body StringID
}

type AbilityRow struct {
	Kind        SAKind
	API         APICode
	Params      Span
	Sub         AbilityID
	Line        StringID
	UnknownKind StringID
	UnknownAPI  StringID
}

type TriggerRow struct {
	Mode        TriggerMode
	Params      Span
	Effect      AbilityID
	UnknownMode StringID
}

type StaticRow struct {
	Mode        StaticMode
	Params      Span
	UnknownMode StringID
}

type ReplacementRow struct {
	Event        ReplEvent
	Params       Span
	With         AbilityID
	UnknownEvent StringID
}

type FaceRow struct {
	Types         Span
	Keywords      Span
	Abilities     Span
	ManaAbilities Span
	Triggers      Span
	Statics       Span
	Replacements  Span
	SVars         Span

	SpellAbility     AbilityID
	TypeMask         TypeMask
	KeywordMask      KeywordMask
	Colours          ColourMask
	ColourIdentity   ColourMask
	Flags            FaceFlags
	Power            int32
	Toughness        int32
	ManaValue        int32
	TriggerInterests TriggerInterest
}

// CompiledCatalog is immutable after publication by Registry.CompileMetadata.
// IDs are one-based; spans index the corresponding zero-based slices.
type CompiledCatalog struct {
	Identity CatalogIdentity

	Faces          []FaceRow
	Abilities      []AbilityRow
	Triggers       []TriggerRow
	Statics        []StaticRow
	Replacements   []ReplacementRow
	Params         []ParamRow
	Keywords       []KeywordRow
	SVars          []SVarRow
	TypeTokens     []StringID
	FaceAbilities  []AbilityID
	ManaAbilityIDs []AbilityID
	Strings        []StringRef
	StringBlob     []byte

	facePointers    []*Face
	abilityPointers []*SA
	// manaAbilityPointers[i] is abilityPointers[ManaAbilityIDs[i]-1] (nil for
	// an id out of range), built once at CompileMetadata so Face.ManaAbilities
	// can serve a face's span without allocating.
	manaAbilityPointers []*SA
}

func (c *CompiledCatalog) CanonicalBytes() []byte {
	if c == nil {
		return nil
	}
	return canonicalCatalogBytes(c)
}

func (c *CompiledCatalog) String(id StringID) (string, bool) {
	if c == nil || id == 0 || int(id) > len(c.Strings) {
		return "", false
	}
	r := c.Strings[id-1]
	end := uint64(r.Offset) + uint64(r.Length)
	if end > uint64(len(c.StringBlob)) {
		return "", false
	}
	return string(c.StringBlob[r.Offset:uint32(end)]), true
}

type catalogBuilder struct {
	catalog    CompiledCatalog
	stringIDs  map[string]StringID
	abilityIDs map[*SA]AbilityID
}

type faceBinding struct {
	face *Face
	id   FaceID
}

type abilityBinding struct {
	sa *SA
	id AbilityID
}

func (r *Registry) Catalog() *CompiledCatalog {
	if r == nil {
		return nil
	}
	if r.lazy != nil {
		return r.lazy.catalog()
	}
	return r.catalog
}

func (r *Registry) invalidateCatalog() {
	if r == nil || r.catalog == nil {
		return
	}
	old := r.catalog
	for _, face := range old.facePointers {
		if face != nil && face.compiledCatalog == old {
			face.compiledCatalog = nil
			face.compiledID = 0
			face.compiledTriggerInterests = 0
			face.compiledTypeMask = 0
		}
	}
	for _, sa := range old.abilityPointers {
		if sa != nil && sa.compiledCatalog == old {
			sa.compiledCatalog = nil
			sa.compiledID = 0
		}
	}
	r.catalog = nil
}

// CompileMetadata constructs all rows before publishing any runtime binding.
// Unknown vocabulary is represented by a zero code plus its original string.
func (r *Registry) CompileMetadata() error {
	if r == nil {
		return fmt.Errorf("compile metadata: nil registry")
	}
	b := catalogBuilder{
		stringIDs:  make(map[string]StringID),
		abilityIDs: make(map[*SA]AbilityID),
	}
	var faces []faceBinding
	var abilities []abilityBinding
	b.catalog.facePointers = nil
	b.catalog.abilityPointers = nil

	compileCard := func(c *Card) error {
		if c == nil {
			return nil
		}
		for _, face := range c.Faces {
			if face == nil {
				continue
			}
			id, err := b.compileFace(face, &abilities)
			if err != nil {
				return err
			}
			faces = append(faces, faceBinding{face: face, id: id})
		}
		return nil
	}
	for _, card := range r.cards {
		if err := compileCard(card); err != nil {
			return err
		}
	}
	tokenKeys := make([]string, 0, len(r.Tokens))
	for key := range r.Tokens {
		tokenKeys = append(tokenKeys, key)
	}
	sort.Strings(tokenKeys)
	for _, key := range tokenKeys {
		if err := compileCard(r.Tokens[key]); err != nil {
			return err
		}
	}

	canonical := canonicalCatalogBytes(&b.catalog)
	b.catalog.Identity = CatalogIdentity{Schema: CompiledCatalogSchema, CorpusHash: sha256.Sum256(canonical)}
	b.catalog.manaAbilityPointers = make([]*SA, len(b.catalog.ManaAbilityIDs))
	for i, id := range b.catalog.ManaAbilityIDs {
		if id != 0 && int(id) <= len(b.catalog.abilityPointers) {
			b.catalog.manaAbilityPointers[i] = b.catalog.abilityPointers[id-1]
		}
	}
	for _, binding := range faces {
		binding.face.compiledCatalog = &b.catalog
		binding.face.compiledID = binding.id
		binding.face.compiledTriggerInterests = b.catalog.Faces[binding.id-1].TriggerInterests
		binding.face.compiledTypeMask = b.catalog.Faces[binding.id-1].TypeMask
	}
	for _, binding := range abilities {
		binding.sa.compiledCatalog = &b.catalog
		binding.sa.compiledID = binding.id
	}
	r.catalog = &b.catalog
	return nil
}

func (b *catalogBuilder) compileFace(face *Face, bindings *[]abilityBinding) (FaceID, error) {
	faceID, err := oneBasedID(len(b.catalog.Faces), "faces")
	if err != nil {
		return 0, err
	}
	row := FaceRow{
		Power:          face.power,
		Toughness:      face.toughness,
		ManaValue:      face.cmc,
		Colours:        printedColourMask(face),
		ColourIdentity: ColourMask(face.colourIdentity),
	}
	for _, typ := range face.Types {
		id, err := b.stringID(typ)
		if err != nil {
			return 0, err
		}
		b.catalog.TypeTokens = append(b.catalog.TypeTokens, id)
		row.TypeMask |= typeMaskFor(typ)
	}
	if row.Types, err = checkedSpan(len(b.catalog.TypeTokens)-len(face.Types), len(face.Types), "face types"); err != nil {
		return 0, err
	}
	keywordStart := len(b.catalog.Keywords)
	for _, keyword := range face.Keywords {
		head := KeywordHead(keyword)
		headID, err := b.stringID(head)
		if err != nil {
			return 0, err
		}
		fullID, err := b.stringID(keyword)
		if err != nil {
			return 0, err
		}
		b.catalog.Keywords = append(b.catalog.Keywords, KeywordRow{Head: headID, Full: fullID})
		row.KeywordMask |= keywordMaskFor(head)
	}
	if row.Keywords, err = checkedSpan(keywordStart, len(face.Keywords), "face keywords"); err != nil {
		return 0, err
	}

	abilityStart := len(b.catalog.FaceAbilities)
	for _, sa := range face.Abilities {
		id, err := b.compileAbility(sa, bindings)
		if err != nil {
			return 0, err
		}
		b.catalog.FaceAbilities = append(b.catalog.FaceAbilities, id)
		if row.SpellAbility == 0 && sa != nil && sa.Kind == "SP" {
			row.SpellAbility = id
		}
		if manaAbilityListEntry(sa) {
			b.catalog.ManaAbilityIDs = append(b.catalog.ManaAbilityIDs, id)
		}
	}
	if row.Abilities, err = checkedSpan(abilityStart, len(face.Abilities), "face abilities"); err != nil {
		return 0, err
	}
	manaCount := 0
	for _, sa := range face.Abilities {
		if manaAbilityListEntry(sa) {
			manaCount++
		}
	}
	if row.ManaAbilities, err = checkedSpan(len(b.catalog.ManaAbilityIDs)-manaCount, manaCount, "face mana abilities"); err != nil {
		return 0, err
	}

	triggerStart := len(b.catalog.Triggers)
	for _, trigger := range face.Triggers {
		params, err := b.compileParams(trigger.Params)
		if err != nil {
			return 0, err
		}
		effect, err := b.compileAbility(trigger.Effect, bindings)
		if err != nil {
			return 0, err
		}
		mode := trigger.ModeKind()
		var unknown StringID
		if mode == 0 {
			unknown, err = b.stringID(trigger.Mode)
			if err != nil {
				return 0, err
			}
		}
		b.catalog.Triggers = append(b.catalog.Triggers, TriggerRow{Mode: mode, Params: params, Effect: effect, UnknownMode: unknown})
		if strings.TrimSpace(trigger.ParamStr(PKPhase)) != "" {
			row.TriggerInterests |= TriggerInterestAny
		} else {
			row.TriggerInterests |= triggerInterestForMode(trigger.ModeKind())
		}
	}
	if row.Triggers, err = checkedSpan(triggerStart, len(face.Triggers), "face triggers"); err != nil {
		return 0, err
	}

	staticStart := len(b.catalog.Statics)
	for _, static := range face.Statics {
		params, err := b.compileParams(static.Params)
		if err != nil {
			return 0, err
		}
		mode := static.ModeKind()
		var unknown StringID
		if mode == 0 {
			unknown, err = b.stringID(static.Mode)
			if err != nil {
				return 0, err
			}
		}
		b.catalog.Statics = append(b.catalog.Statics, StaticRow{Mode: mode, Params: params, UnknownMode: unknown})
	}
	if row.Statics, err = checkedSpan(staticStart, len(face.Statics), "face statics"); err != nil {
		return 0, err
	}

	replacementStart := len(b.catalog.Replacements)
	for _, replacement := range face.Repls {
		params, err := b.compileParams(replacement.Params)
		if err != nil {
			return 0, err
		}
		with, err := b.compileAbility(replacement.With, bindings)
		if err != nil {
			return 0, err
		}
		event := replacement.EventKind()
		var unknown StringID
		if event == 0 {
			unknown, err = b.stringID(replacement.Event)
			if err != nil {
				return 0, err
			}
		}
		b.catalog.Replacements = append(b.catalog.Replacements, ReplacementRow{Event: event, Params: params, With: with, UnknownEvent: unknown})
	}
	if row.Replacements, err = checkedSpan(replacementStart, len(face.Repls), "face replacements"); err != nil {
		return 0, err
	}

	svarStart := len(b.catalog.SVars)
	keys := sortedKeys(face.SVars)
	for _, name := range keys {
		nameID, err := b.stringID(name)
		if err != nil {
			return 0, err
		}
		bodyID, err := b.stringID(face.SVars[name])
		if err != nil {
			return 0, err
		}
		b.catalog.SVars = append(b.catalog.SVars, SVarRow{Name: nameID, Body: bodyID})
	}
	if row.SVars, err = checkedSpan(svarStart, len(keys), "face SVars"); err != nil {
		return 0, err
	}
	if row.TypeMask&(TypeInstant|TypeSorcery) == 0 {
		row.Flags |= FaceFlagPermanent
	}
	if face.characteristicDefining {
		row.Flags |= FaceFlagCharacteristicDefining
	}
	b.catalog.Faces = append(b.catalog.Faces, row)
	b.catalog.facePointers = append(b.catalog.facePointers, face)
	return FaceID(faceID), nil
}

func (b *catalogBuilder) compileAbility(sa *SA, bindings *[]abilityBinding) (AbilityID, error) {
	if sa == nil {
		return 0, nil
	}
	if id := b.abilityIDs[sa]; id != 0 {
		return id, nil
	}
	id, err := oneBasedID(len(b.catalog.Abilities), "abilities")
	if err != nil {
		return 0, err
	}
	abilityID := AbilityID(id)
	b.abilityIDs[sa] = abilityID
	b.catalog.Abilities = append(b.catalog.Abilities, AbilityRow{})
	b.catalog.abilityPointers = append(b.catalog.abilityPointers, sa)
	*bindings = append(*bindings, abilityBinding{sa: sa, id: abilityID})

	params, err := b.compileParams(sa.Params)
	if err != nil {
		return 0, err
	}
	sub, err := b.compileAbility(sa.Sub, bindings)
	if err != nil {
		return 0, err
	}
	kind := saKindCode(sa.Kind)
	api := APICodeForName(sa.API)
	row := AbilityRow{Kind: kind, API: api, Params: params, Sub: sub}
	if row.Line, err = b.stringID(sa.Line); err != nil {
		return 0, err
	}
	if kind == SAKindUnknown {
		if row.UnknownKind, err = b.stringID(sa.Kind); err != nil {
			return 0, err
		}
	}
	if api == APIUnknown {
		if row.UnknownAPI, err = b.stringID(sa.API); err != nil {
			return 0, err
		}
	}
	b.catalog.Abilities[abilityID-1] = row
	return abilityID, nil
}

func (b *catalogBuilder) compileParams(params map[string]string) (Span, error) {
	start := len(b.catalog.Params)
	keys := sortedKeys(params)
	for _, key := range keys {
		keyID, err := b.stringID(key)
		if err != nil {
			return Span{}, err
		}
		valueID, err := b.stringID(params[key])
		if err != nil {
			return Span{}, err
		}
		b.catalog.Params = append(b.catalog.Params, ParamRow{Key: keyID, Value: valueID})
	}
	return checkedSpan(start, len(keys), "parameters")
}

func (b *catalogBuilder) stringID(value string) (StringID, error) {
	if value == "" {
		return 0, nil
	}
	if id := b.stringIDs[value]; id != 0 {
		return id, nil
	}
	if uint64(len(b.catalog.StringBlob))+uint64(len(value)) > math.MaxUint32 {
		return 0, fmt.Errorf("compiled string blob exceeds uint32")
	}
	id, err := oneBasedID(len(b.catalog.Strings), "strings")
	if err != nil {
		return 0, err
	}
	ref := StringRef{Offset: uint32(len(b.catalog.StringBlob)), Length: uint32(len(value))}
	b.catalog.StringBlob = append(b.catalog.StringBlob, value...)
	b.catalog.Strings = append(b.catalog.Strings, ref)
	stringID := StringID(id)
	b.stringIDs[value] = stringID
	return stringID, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func oneBasedID(length int, what string) (uint32, error) {
	if uint64(length) >= math.MaxUint32 {
		return 0, fmt.Errorf("compiled %s exceed uint32", what)
	}
	return uint32(length + 1), nil
}

func checkedSpan(start, count int, what string) (Span, error) {
	if start < 0 || count < 0 || uint64(start) > math.MaxUint32 || uint64(count) > math.MaxUint32 || uint64(start)+uint64(count) > math.MaxUint32 {
		return Span{}, fmt.Errorf("compiled %s span exceeds uint32", what)
	}
	return Span{Start: uint32(start), Count: uint32(count)}, nil
}

func printedColourMask(face *Face) ColourMask {
	if face == nil || face.HasKeyword("Devoid") {
		return 0
	}
	var mask ColourMask
	for i := 0; i < len(face.ManaCost); i++ {
		mask |= colourMaskFor(face.ManaCost[i : i+1])
	}
	if mask != 0 {
		return mask
	}
	for colour := range strings.SplitSeq(face.Colors, ",") {
		switch strings.ToLower(strings.TrimSpace(colour)) {
		case "white":
			mask |= ColourMaskWhite
		case "blue":
			mask |= ColourMaskBlue
		case "black":
			mask |= ColourMaskBlack
		case "red":
			mask |= ColourMaskRed
		case "green":
			mask |= ColourMaskGreen
		}
	}
	return mask
}

func canonicalCatalogBytes(c *CompiledCatalog) []byte {
	var out []byte
	put8 := func(v uint8) { out = append(out, v) }
	put16 := func(v uint16) { out = binary.LittleEndian.AppendUint16(out, v) }
	put32 := func(v uint32) { out = binary.LittleEndian.AppendUint32(out, v) }
	putSpan := func(s Span) { put32(s.Start); put32(s.Count) }
	putLen := func(n int) { put32(uint32(n)) }
	put32(CompiledCatalogSchema)
	putLen(len(c.Strings))
	for _, row := range c.Strings {
		put32(row.Offset)
		put32(row.Length)
	}
	putLen(len(c.StringBlob))
	out = append(out, c.StringBlob...)
	putLen(len(c.Params))
	for _, row := range c.Params {
		put32(uint32(row.Key))
		put32(uint32(row.Value))
	}
	putLen(len(c.Keywords))
	for _, row := range c.Keywords {
		put32(uint32(row.Head))
		put32(uint32(row.Full))
	}
	putLen(len(c.SVars))
	for _, row := range c.SVars {
		put32(uint32(row.Name))
		put32(uint32(row.Body))
	}
	putLen(len(c.TypeTokens))
	for _, id := range c.TypeTokens {
		put32(uint32(id))
	}
	putLen(len(c.FaceAbilities))
	for _, id := range c.FaceAbilities {
		put32(uint32(id))
	}
	putLen(len(c.ManaAbilityIDs))
	for _, id := range c.ManaAbilityIDs {
		put32(uint32(id))
	}
	putLen(len(c.Abilities))
	for _, row := range c.Abilities {
		put8(uint8(row.Kind))
		put16(uint16(row.API))
		putSpan(row.Params)
		put32(uint32(row.Sub))
		put32(uint32(row.Line))
		put32(uint32(row.UnknownKind))
		put32(uint32(row.UnknownAPI))
	}
	putLen(len(c.Triggers))
	for _, row := range c.Triggers {
		put16(uint16(row.Mode))
		putSpan(row.Params)
		put32(uint32(row.Effect))
		put32(uint32(row.UnknownMode))
	}
	putLen(len(c.Statics))
	for _, row := range c.Statics {
		put16(uint16(row.Mode))
		putSpan(row.Params)
		put32(uint32(row.UnknownMode))
	}
	putLen(len(c.Replacements))
	for _, row := range c.Replacements {
		put8(uint8(row.Event))
		putSpan(row.Params)
		put32(uint32(row.With))
		put32(uint32(row.UnknownEvent))
	}
	putLen(len(c.Faces))
	for _, row := range c.Faces {
		putSpan(row.Types)
		putSpan(row.Keywords)
		putSpan(row.Abilities)
		putSpan(row.ManaAbilities)
		putSpan(row.Triggers)
		putSpan(row.Statics)
		putSpan(row.Replacements)
		putSpan(row.SVars)
		put32(uint32(row.SpellAbility))
		put32(uint32(row.TypeMask))
		put32(uint32(row.KeywordMask))
		put8(uint8(row.Colours))
		put8(uint8(row.ColourIdentity))
		put8(uint8(row.Flags))
		put32(uint32(row.Power))
		put32(uint32(row.Toughness))
		put32(uint32(row.ManaValue))
		put16(uint16(row.TriggerInterests))
	}
	return out
}
