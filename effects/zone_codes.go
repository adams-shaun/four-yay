package effects

import (
	"math/bits"
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
func ZoneListOf(s string) ZoneList { return zoneSetOf(s, false) }

func zoneSetOf(s string, ignoreUnknown bool) ZoneList {
	var c ZoneList
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "Any" || part == "All" {
			c |= zoneListAll
			continue
		}
		z, known := parseZone(part)
		if !known {
			if !ignoreUnknown {
				c |= zoneListInvalid
			}
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

// Destination is a Destination$ value compiled once as a lenient zone set:
// unknown words are ignored rather than poisoning the list. Its bit layout
// uses ZoneList's zone and wildcard bits plus an empty-text marker.
type Destination uint16

const (
	destinationAny               = Destination(zoneListAll)
	destinationEmpty Destination = 1 << 15
)

// DestinationOf compiles s.
func DestinationOf(s string) Destination {
	if strings.TrimSpace(s) == "" {
		return destinationEmpty
	}
	return Destination(zoneSetOf(s, true))
}

// Admits reports that z is named or the list is a wildcard.
func (d Destination) Admits(z state.Zone) bool {
	return d&destinationAny != 0 || (z < 14 && d&(1<<z) != 0)
}

// Zones is the named zones as a bitmask over state.Zone values.
func (d Destination) Zones() uint32 {
	return uint32(d &^ (destinationAny | destinationEmpty))
}

// SoleZone reports the single named zone, if exactly one is named.
func (d Destination) SoleZone() (state.Zone, bool) {
	m := d.Zones()
	if m == 0 || m&(m-1) != 0 {
		return 0, false
	}
	return state.Zone(bits.TrailingZeros32(m)), true
}

// IsAny reports the list contains the wildcard.
func (d Destination) IsAny() bool { return d&destinationAny != 0 }

// IsEmpty reports the text is empty.
func (d Destination) IsEmpty() bool { return d&destinationEmpty != 0 }

func init() {
	cards.RegisterParamCoder(cards.PKOrigin, "effects.ZoneList", func(s string) uint16 { return uint16(ZoneListOf(s)) })
	cards.RegisterParamCoder(cards.PKExcludedOrigins, "effects.ZoneWords", func(s string) uint16 { return uint16(ZoneWordsOf(s)) })
	cards.RegisterParamCoder(cards.PKDestination, "effects.Destination", func(s string) uint16 { return uint16(DestinationOf(s)) })
}
