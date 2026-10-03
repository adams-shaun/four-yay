package effects

// The ChangeZone ChooseFromDefined$ referent spellings beyond the original
// dotted AttachedTo form: each measured non-AttachedTo selector either
// resolves the intended object pool (offered as the pick's options and
// enforced on the answered picks) or stays loud-fail-closed (one Note, an
// empty pool, nothing offered). Every card here is the REAL compiled corpus
// card, so a corpus pin that moves the scripts fails the lookup.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// chooseFromDefinedSA locates the real corpus card's ChangeZone sub-ability
// carrying the named ChooseFromDefined$ spelling: printed face abilities and
// their sub-ability chains first, then every face's SVar bodies compiled
// through cards.ResolveSVar (the same compile the resolving chain uses).
func chooseFromDefinedSA(t *testing.T, cardName, spelling string) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup(cardName)
	if !ok {
		t.Fatalf("corpus missing %q", cardName)
	}
	for _, face := range card.Faces {
		for _, ab := range face.Abilities {
			if ab.Params["ChooseFromDefined"] == spelling {
				return ab
			}
			for sub := ab.Sub; sub != nil; sub = sub.Sub {
				if sub.Params["ChooseFromDefined"] == spelling {
					return sub
				}
			}
		}
		for name := range face.SVars {
			if sa := cards.ResolveSVar(face.SVars, name); sa != nil &&
				sa.Params["ChooseFromDefined"] == spelling {
				return sa
			}
		}
	}
	t.Fatalf("corpus pin moved: %q no longer carries ChooseFromDefined$ %s", cardName, spelling)
	return nil
}

// isolate detaches a copy's SubAbility chain so a selector test exercises
// the one ChangeZone leg it asserts (the SA body is shared card data; only
// the copy is touched).
func isolate(sa *cards.SA) *cards.SA {
	cp := *sa
	cp.Sub = nil
	return &cp
}

// cfdFixture is one placed object: its inline card text, owner and zone.
type cfdFixture struct {
	text  string
	owner state.PlayerID
	zone  state.Zone
}

// cfdBoard builds a 2-seat ask host and places every fixture in its zone.
// Zones are rebuilt through SetZone so the game's zone slices agree with the
// object fields, and the returned ids follow the fixtures' order.
func cfdBoard(t *testing.T, specs ...cfdFixture) (*askHost, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	ids := make([]state.ObjID, 0, len(specs))
	byOwner := map[state.Zone]map[state.PlayerID][]state.ObjID{}
	for _, s := range specs {
		o := h.g.AddObject(mkCard(t, s.text), s.owner)
		o.Zone = s.zone
		ids = append(ids, o.ID)
		if byOwner[s.zone] == nil {
			byOwner[s.zone] = map[state.PlayerID][]state.ObjID{}
		}
		byOwner[s.zone][s.owner] = append(byOwner[s.zone][s.owner], o.ID)
	}
	for z, owners := range byOwner {
		for p, zs := range owners {
			h.g.SetZone(z, p, zs)
		}
	}
	return h, ids
}

// assertOfferSet pins the decision's offered object set exactly (order
// preserved on the first mismatch for the failure message).
func assertOfferSet(t *testing.T, d *decision.Decision, want ...state.ObjID) {
	t.Helper()
	got := make([]state.ObjID, 0, len(d.Options))
	for _, o := range d.Options {
		got = append(got, o.Obj)
	}
	if len(got) != len(want) {
		t.Fatalf("options = %v, want exactly %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("options = %v, want %v (slot %d differs)", got, want, i)
		}
	}
}

// assertNote pins that one Note naming the selector was emitted.
func assertNote(t *testing.T, h *askHost, raw string) {
	t.Helper()
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "ChooseFromDefined$ "+raw) &&
			strings.Contains(ev.Text, "not resolvable") {
			return
		}
	}
	t.Fatalf("no fail-closed Note for ChooseFromDefined$ %s; log: %+v", raw, h.log)
}

const (
	whiteCreature = "Name:White Runt\nManaCost:W\nTypes:Creature\nPT:1/1\nOracle:x\n"
	blueCreature  = "Name:Blue Drake\nManaCost:1 U\nTypes:Creature\nPT:2/2\nOracle:x\n"
	plainLand     = "Name:Test Plains\nTypes:Land\nOracle:x\n"
)

// assertOptionsExclude reports whether the decision's options exclude every
// named id (the boolean is a formality for the caller above).
func assertOptionsExclude(t *testing.T, d *decision.Decision, exclude ...state.ObjID) bool {
	t.Helper()
	seen := map[state.ObjID]bool{}
	for _, o := range d.Options {
		seen[o.Obj] = true
	}
	for _, id := range exclude {
		if seen[id] {
			t.Fatalf("option list must not offer %d; options: %+v", id, d.Options)
			return false
		}
	}
	return true
}

// TestDefinedTopThirdOfLibraryRounding pins the rounding rule directly: top
// third rounded up over 1, 3, 4 and 7-card libraries.
func TestDefinedTopThirdOfLibraryRounding(t *testing.T) {
	for n, want := range map[int]int{1: 1, 3: 1, 4: 2, 7: 3} {
		h, ids := cfdBoard(t, nCards(n)...)
		c := &Ctx{Source: ids[0], Controller: 0}
		ts, ok := knownDefinedTargets(h, c, "TopThirdOfLibrary")
		if !ok {
			t.Fatalf("TopThirdOfLibrary over %d cards: not resolvable", n)
		}
		if len(ts) != want {
			t.Fatalf("TopThirdOfLibrary over %d cards = %d targets, want %d", n, len(ts), want)
		}
		for i, tgt := range ts {
			if tgt.Obj != ids[i] {
				t.Fatalf("TopThirdOfLibrary slot %d = %d, want the zone-order card %d", i, tgt.Obj, ids[i])
			}
		}
	}
}

// nCards builds n distinct library cards.
func nCards(n int) []cfdFixture {
	out := make([]cfdFixture, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, cfdFixture{cfdCreatureText(i+1, i+1), 0, state.ZLibrary})
	}
	return out
}

func cfdCreatureText(cost, power int) string {
	return "Name:Round Card " + string(rune('A'+cost)) + "\nManaCost:" + itoa(cost) +
		"\nTypes:Creature\nPT:" + itoa(power) + "/" + itoa(power) + "\nOracle:x\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
