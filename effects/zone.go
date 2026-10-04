package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("ChangeZone", effChangeZone)
	Register("ChangeZoneAll", effChangeZoneAll)
	Register("Destroy", effDestroy)
	Register("DestroyAll", effDestroyAll)
	Register("Sacrifice", effSacrifice)
	Register("Manifest", effManifest)
	Register("ManifestDread", effManifestDread)
	Register("Cloak", effCloak)
	Register("Seek", effSeek)
}

// effSeek implements Alchemy's random library-to-hand seek. Unlike a hidden
// library search, seek neither reveals nor shuffles: it samples the eligible
// pool without replacement using the host's seeded RNG.
func effSeek(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// RememberFound$ makes the found card(s) the resolution's Remembered set
	// (Forge's SeekEffect rememberFound), REPLACING whatever the resolution
	// started with -- the same rule the DigUntil fix (c1d996d4) landed for
	// DB$ DigUntil. A triggered resolution's ctx Remembered already carries
	// the trigger's captured referent (Goblin Trapfinder's own dying card),
	// so appending would make a chained Defined$ Remembered act on it too.
	// The trigger referents survive in Ctx.Captured, the separate channel.
	// Accumulate across the multi-player walk and assign once, so a second
	// player's found cards do not clobber the first's.
	rememberFound := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberFound)), "True")
	var seekRemembered []state.Target
	defined := DefinedRefOf(sa)
	players := DefinedRef(h, c, defined, sa)
	if defined.Raw == "" {
		players = []state.Target{{Player: c.Controller, IsPlayer: true}}
	}
	for _, target := range players {
		if !target.IsPlayer || int(target.Player) >= len(g.Players) {
			continue
		}
		owner := target.Player
		pool := zoneOf(g, state.ZLibrary, owner)
		if raw := DefinedOf(sa).Cards.Text; raw != "" {
			if raw != "Top_10_OfLibrary" {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "Seek withholds DefinedCards$ " + raw + "; no cards moved"})
				continue
			}
			if len(pool) > 10 {
				pool = pool[:10]
			}
		}

		spec := strings.TrimSpace(sa.ParamStr(cards.PKType))
		if spec == "" {
			spec = "Card"
		}
		types := strings.Split(strings.TrimSpace(sa.ParamStr(cards.PKTypes)), ",")
		if strings.TrimSpace(sa.ParamStr(cards.PKTypes)) == "" {
			types = []string{spec}
		}
		selected := make([]state.ObjID, 0)
		used := make(map[state.ObjID]bool)
		for _, typeSpec := range types {
			typeSpec = permanentCardSpec(strings.TrimSpace(typeSpec))
			eligible := make([]state.ObjID, 0, len(pool))
			for _, id := range pool {
				if used[id] || !MatchesSpecCtx(g, typeSpec, id, c.SpecContext(c.Controller)) {
					continue
				}
				eligible = append(eligible, id)
			}
			n := int32(1)
			if len(types) == 1 {
				n = Num(h, c, sa, "Num", 1)
			}
			if n < 0 {
				n = 0
			}
			if n > int32(len(eligible)) {
				n = int32(len(eligible))
			}
			for i := int32(0); i < n; i++ {
				j := h.Rand(len(eligible))
				id := eligible[j]
				selected = append(selected, id)
				used[id] = true
				eligible = append(eligible[:j], eligible[j+1:]...)
			}
		}
		if len(selected) == 0 {
			continue
		}
		for _, id := range selected {
			h.Emit(moveZoneEvent(c, id, state.ZLibrary, state.ZHand))
			if rememberFound {
				seekRemembered = append(seekRemembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
		if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKImprintFound)), "True") && c.Source != 0 {
			// ImprintFound$ is Forge's seek imprint (SeekEffect writes
			// imprintedCards). It rides the separate SeekFound list -- not the
			// ordinary Imprinted one -- because the found cards sit in a hand
			// at continuation time, where `Defined$ Imprinted`'s CR 607.2a
			// exiled-only reader would hide them; a chained Origin$ Hand body
			// (Spawning Pod, Gitrog, Kardum, Puppet Raiser) reads them here.
			h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: append([]state.ObjID(nil), selected...), Text: "seek-found"})
		}
		h.Emit(events.Event{Kind: events.Seek, Player: owner, Obj: c.Source})
	}
	if rememberFound {
		c.Remembered = seekRemembered
	}
}

// ParseZone maps a Forge zone name to a state.Zone. Unknown names resolve to
// the graveyard, which is where the overwhelming majority of movement goes
// and is a safe default for an unmodelled destination.
func ParseZone(s string) state.Zone {
	z, ok := parseZone(s)
	if !ok {
		return state.ZGraveyard
	}
	return z
}

// ZoneWord maps a state.Zone back to the canonical Forge zone word ParseZone
// reads (ParseZone's exact-match vocabulary). It is the reverse direction the
// move-driven lifetimes need when the zone is taken from the OBJECT at grant
// time rather than named by the script: a Duration$ Permanent Animate/Pump
// grant records its object's current zone in ExileOnMoved so the grant ends
// when that object leaves the zone it was granted in (CR 400.7 -- a zone
// change makes it a new object). An out-of-range or unnamed zone yields "",
// which ParseZone can never match, so the sweep simply never fires -- the
// honest no-op for a zone this vocabulary does not carry.
func ZoneWord(z state.Zone) string {
	switch z {
	case state.ZHand:
		return "Hand"
	case state.ZBattlefield:
		return "Battlefield"
	case state.ZLibrary:
		return "Library"
	case state.ZGraveyard:
		return "Graveyard"
	case state.ZExile:
		return "Exile"
	case state.ZStack:
		return "Stack"
	case state.ZCommand:
		return "Command"
	case state.ZSideboard:
		return "Sideboard"
	case state.ZCeased:
		return "Ceased"
	}
	return ""
}

// ParseZoneWord is parseZone's exported form for callers that must react to
// an UNKNOWN zone word (fail closed) rather than silently degrading to a
// graveyard the way ParseZone does: the trigger-side PresentZone$ clause's
// recognised-vocabulary check shares this one classification with every
// other zone word so the two cannot disagree.
func ParseZoneWord(s string) (state.Zone, bool) {
	return parseZone(s)
}

func parseZone(s string) (state.Zone, bool) {
	switch parseZoneCodes.Code(string(strings.TrimSpace(s))) {
	case parseZoneHand:
		return state.ZHand, true
	case parseZoneBattlefield:
		return state.ZBattlefield, true
	case parseZoneLibrary:
		return state.ZLibrary, true
	case parseZoneGraveyard:
		return state.ZGraveyard, true
	case parseZoneExile:
		return state.ZExile, true
	case parseZoneStack:
		return state.ZStack, true
	case parseZoneCommand:
		return state.ZCommand, true
	case parseZoneSideboard:
		return state.ZSideboard, true
	case parseZoneCeased:
		return state.ZCeased, true
	}
	return 0, false
}

// ParseZones parses an Origin$ zone set. Any and All are wildcards. A false
// result means at least one token was unknown; callers must not treat it as a
// graveyard origin.
func ParseZones(s string) (zones []state.Zone, all, ok bool) {
	ok = true
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "Any" || part == "All" {
			all = true
			continue
		}
		z, known := parseZone(part)
		if !known {
			ok = false
			continue
		}
		if !zoneIn(zones, z) {
			zones = append(zones, z)
		}
	}
	return zones, all, ok
}

func zoneIn(zones []state.Zone, want state.Zone) bool {
	for _, z := range zones {
		if z == want {
			return true
		}
	}
	return false
}

// mixedOriginIncludesHand identifies every explicit multi-zone Origin$ that
// includes Hand. Such an effect needs one origin-aware hidden-zone chooser;
// the exact-Hand and exact-Library walkers cannot safely stand in for it.
func mixedOriginIncludesHand(zones []state.Zone, all bool) bool {
	return !all && len(zones) > 1 && zoneIn(zones, state.ZHand)
}

type parseZoneCode uint16

const (
	parseZoneHand parseZoneCode = iota + 1
	parseZoneBattlefield
	parseZoneLibrary
	parseZoneGraveyard
	parseZoneExile
	parseZoneStack
	parseZoneCommand
	parseZoneSideboard
	parseZoneCeased
)

var parseZoneCodes = state.NewStrCodes(
	state.StrEntry[parseZoneCode]{Key: "Hand", Val: parseZoneHand},
	state.StrEntry[parseZoneCode]{Key: "Battlefield", Val: parseZoneBattlefield},
	state.StrEntry[parseZoneCode]{Key: "Library", Val: parseZoneLibrary},
	state.StrEntry[parseZoneCode]{Key: "Graveyard", Val: parseZoneGraveyard},
	state.StrEntry[parseZoneCode]{Key: "Exile", Val: parseZoneExile},
	state.StrEntry[parseZoneCode]{Key: "Stack", Val: parseZoneStack},
	state.StrEntry[parseZoneCode]{Key: "Command", Val: parseZoneCommand},
	state.StrEntry[parseZoneCode]{Key: "Sideboard", Val: parseZoneSideboard},
	state.StrEntry[parseZoneCode]{Key: "Ceased", Val: parseZoneCeased},
)
