package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The zone-word parameters a trigger, replacement or static line tests on
// every zone-change event (Origin$, ExcludedOrigins$, Destination$) are
// classified once at load: each key has a cards.ParamCoder below, so the
// ParamSet stores the compiled code and the matchers read it through
// ParamCode instead of re-parsing the word per event (spec W4, "strings
// compile to masks").

// ZoneList is a zone-set parameter (Origin$'s grammar: one zone word or a
// comma list, Any/All a wildcard) compiled exactly as ParseZones reads it: a
// bit per named zone, plus the wildcard and the "an unknown word" bits.
type ZoneList uint16

const (
	zoneListAll     ZoneList = 1 << 14
	zoneListInvalid ZoneList = 1 << 15
)

// ZoneListOf compiles s with ParseZones' grammar.
func ZoneListOf(s string) ZoneList {
	var c ZoneList
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "Any" || part == "All" {
			c |= zoneListAll
			continue
		}
		z, known := parseZone(part)
		if !known {
			c |= zoneListInvalid
			continue
		}
		c |= 1 << z
	}
	return c
}

// Admits is ParseZones' common reading: every word known, and z named or the
// list a wildcard.
func (c ZoneList) Admits(z state.Zone) bool {
	return c&zoneListInvalid == 0 && (c&zoneListAll != 0 || c.Has(z))
}

// OK reports that every word was a known zone (ParseZones' ok).
func (c ZoneList) OK() bool { return c&zoneListInvalid == 0 }

// All reports an Any/All wildcard (ParseZones' all).
func (c ZoneList) All() bool { return c&zoneListAll != 0 }

// Has reports that z is named.
func (c ZoneList) Has(z state.Zone) bool { return z < 14 && c&(1<<z) != 0 }

// Zones is the named zones as a bitmask over state.Zone values.
func (c ZoneList) Zones() uint32 { return uint32(c &^ (zoneListAll | zoneListInvalid)) }

// ZoneWords is a lenient zone list (ExcludedOrigins$' grammar): each
// non-blank comma word through ParseZone, so an unknown word degrades to the
// graveyard exactly as the word-by-word read did.
type ZoneWords uint16

// ZoneWordsOf compiles s with that grammar.
func ZoneWordsOf(s string) ZoneWords {
	var c ZoneWords
	for part := range strings.SplitSeq(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			c |= 1 << ParseZone(p)
		}
	}
	return c
}

// Has reports that z is named.
func (c ZoneWords) Has(z state.Zone) bool { return z < 16 && c&(1<<z) != 0 }

// Destination is a Destination$ value compiled once: ParseZone's zone (an
// unknown word degrades to the graveyard), plus whether the text is exactly
// "Any" (the matchers' wildcard spelling) or empty.
type Destination uint16

const (
	destinationAny   Destination = 1 << 8
	destinationEmpty Destination = 1 << 9
)

// DestinationOf compiles s.
func DestinationOf(s string) Destination {
	d := Destination(ParseZone(s))
	if s == "Any" {
		d |= destinationAny
	}
	if s == "" {
		d |= destinationEmpty
	}
	return d
}

// Zone is ParseZone(s).
func (d Destination) Zone() state.Zone { return state.Zone(d & 0xff) }

// IsAny reports the text is exactly "Any".
func (d Destination) IsAny() bool { return d&destinationAny != 0 }

// IsEmpty reports the text is empty.
func (d Destination) IsEmpty() bool { return d&destinationEmpty != 0 }

func init() {
	cards.RegisterParamCoder(cards.PKOrigin, "effects.ZoneList", func(s string) uint16 { return uint16(ZoneListOf(s)) })
	cards.RegisterParamCoder(cards.PKExcludedOrigins, "effects.ZoneWords", func(s string) uint16 { return uint16(ZoneWordsOf(s)) })
	cards.RegisterParamCoder(cards.PKDestination, "effects.Destination", func(s string) uint16 { return uint16(DestinationOf(s)) })
}
