package effects

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// lzAdd puts an already-parsed card into owner's zone, mirroring filter_test's
// board(t) fixture discipline: setup writes zones directly, the assertion path
// observes only emitted events.
func lzAdd(t *testing.T, h *fakeHost, owner state.PlayerID, zone state.Zone, card *cards.Card) state.ObjID {
	t.Helper()
	o := h.g.AddObject(card, owner)
	o.Zone = zone
	h.g.SetZone(zone, owner, append(h.g.Zone(zone, owner), o.ID))
	return o.ID
}

// lzCreature builds a synthetic creature card and drops it into owner's zone.
func lzCreature(t *testing.T, h *fakeHost, owner state.PlayerID, zone state.Zone, name string) state.ObjID {
	t.Helper()
	src := "Name:" + name + "\nManaCost:1 U\nTypes:Creature Wizard\nPT:1/1\nOracle:x\n"
	c, d := cards.ParseBytes("t.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return lzAdd(t, h, owner, zone, c)
}

// lzFiller builds a non-creature library filler card (a creature card would
// match the census SAs' ValidTgts$ Creature).
func lzFiller(t *testing.T, h *fakeHost, owner state.PlayerID, name string) state.ObjID {
	t.Helper()
	src := "Name:" + name + "\nManaCost:1 U\nTypes:Instant\nOracle:x\n"
	c, d := cards.ParseBytes("t.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return lzAdd(t, h, owner, state.ZLibrary, c)
}

// lzFills lays out n filler cards named F0..F<n-1> on seat 0's library and
// returns their ids in zone order.
func lzFills(t *testing.T, h *fakeHost, n int) []state.ObjID {
	t.Helper()
	var ids []state.ObjID
	for i := 0; i < n; i++ {
		ids = append(ids, lzFiller(t, h, 0, "F"+strconv.Itoa(i)))
	}
	return ids
}

// lzLibraryOrderIDs fails the test when any moved card left the game.
func lzLibraryOrderIDs(h *fakeHost, owner state.PlayerID) []state.ObjID {
	return h.g.Zone(state.ZLibrary, owner)
}

// TestGolgariThugDeathTriggerPutsTargetOnTopOfLibrary is the head test of
// golgari_thug1: the ACTUAL compiled corpus SA for Golgari Thug's death
// trigger ("put target creature card from your graveyard on top of your
// library", LibraryPosition$ 0) is driven through the object-target path of
// effChangeZone with the trigger's own target -- the Thug that just died --
// and the card must come to rest at the TOP of its owner's library. Before
// the fix the targeted path never read LibraryPosition$: the MoveZone bottom
// append stood and the card sat at the bottom of the library (found by the
// loop-design seat: with Rakdos, the Muscle + Ashnod's Altar the trigger
// resolved, Rakdos then exiled the top two cards, and Thug was not among
// them).
func TestGolgariThugDeathTriggerPutsTargetOnTopOfLibrary(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	thugCard, ok := reg.Lookup("Golgari Thug")
	if !ok {
		t.Fatal("corpus has no Golgari Thug")
	}
	f := thugCard.Faces[0]
	var trigSA *cards.SA
	for _, tr := range f.Triggers {
		if tr.Effect != nil && tr.Effect.API == "ChangeZone" {
			trigSA = tr.Effect
			break
		}
	}
	if trigSA == nil {
		t.Fatal("Golgari Thug has no ChangesZone trigger effect in the corpus")
	}
	if trigSA.Params["LibraryPosition"] != "0" || trigSA.Params["ValidTgts"] != "Creature" {
		t.Fatalf("compiled trigger params = %v, want LibraryPosition$ 0 / ValidTgts$ Creature", trigSA.Params)
	}

	h := newHost(t, 2)
	// Two cards in the library BEFORE the move, so "top" is a meaningful
	// assertion rather than a single-card zone that is both top and bottom.
	fills := lzFills(t, h, 2)
	thugID := lzAdd(t, h, 0, state.ZGraveyard, thugCard)
	if z := h.g.Obj(thugID).Zone; z != state.ZGraveyard {
		t.Fatalf("precondition: Thug zone = %v, want graveyard", z)
	}
	if lib := lzLibraryOrderIDs(h, 0); slices.Equal(lib, []state.ObjID{thugID}) {
		t.Fatalf("precondition: library = %v, want the two fillers only", lib)
	}

	Resolve(h, &Ctx{Controller: 0, Source: thugID,
		Targets: []state.Target{{Obj: thugID}}}, trigSA)

	if z := h.g.Obj(thugID).Zone; z != state.ZLibrary {
		t.Fatalf("after resolution Thug zone = %v, want library (the trigger moved nothing)", z)
	}
	lib := lzLibraryOrderIDs(h, 0)
	want := append([]state.ObjID{thugID}, fills...)
	if !slices.Equal(lib, want) {
		t.Fatalf("library = %v, want Thug on TOP: %v", lib, want)
	}
	var orders int
	for _, e := range h.log {
		if e.Kind == events.LibraryOrder {
			orders++
		}
	}
	if orders != 1 {
		t.Fatalf("LibraryOrder events = %d, want exactly 1 (the placement the fix adds)", orders)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("unexpected Note %+v (the placement must not be loud)", e)
		}
	}
}

// TestTargetedChangeZoneLibraryPositions pins the whole position vocabulary of
// the widened census on the object-target path, one case per distinct shape:
// top (0), second from top (1), the deeper offsets (2, 3 -- Riptide Gearhulk
// and Lost to Legend's shapes), bottom (-1 -- Condemn's shape), the bare X
// (Unexpectedly Absent's "just beneath the top X cards"), and the loud
// fail-closed shape (a value the Num grammar cannot resolve emits a Note and
// leaves the MoveZone bottom append standing rather than guessing).
func TestTargetedChangeZoneLibraryPositions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pos    string
		svars  map[string]string
		x      int32
		wantAt int // index the target must land at in the pre-move library
	}{
		{name: "top", pos: "0", wantAt: 0},
		{name: "second from top", pos: "1", wantAt: 1},
		{name: "third from top", pos: "2", wantAt: 2},
		{name: "fourth from top", pos: "3", wantAt: 3},
		{name: "bottom", pos: "-1", wantAt: -1},
		{name: "bare X", pos: "X", x: 2, wantAt: 2},
		{name: "svar X", pos: "X", svars: map[string]string{"X": "Count$xPaid"}, x: 3, wantAt: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, 2)
			fills := lzFills(t, h, 4)
			tgt := lzCreature(t, h, 0, state.ZBattlefield, "Vanished")
			if z := h.g.Obj(tgt).Zone; z != state.ZBattlefield {
				t.Fatalf("precondition: target zone = %v, want battlefield", z)
			}
			sa := sa(t, "DB$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Library | LibraryPosition$ "+tc.pos)
			Resolve(h, &Ctx{Controller: 0, Source: tgt, X: tc.x, SVars: tc.svars,
				Targets: []state.Target{{Obj: tgt}}}, sa)

			if z := h.g.Obj(tgt).Zone; z != state.ZLibrary {
				t.Fatalf("target zone = %v, want library", z)
			}
			lib := lzLibraryOrderIDs(h, 0)
			var want []state.ObjID
			if tc.wantAt < 0 {
				want = append(want, fills...)
				want = append(want, tgt)
			} else {
				want = append(want, fills[:tc.wantAt]...)
				want = append(want, tgt)
				want = append(want, fills[tc.wantAt:]...)
			}
			if !slices.Equal(lib, want) {
				t.Fatalf("library = %v, want %v (position %q)", lib, want, tc.pos)
			}
		})
	}
}

// TestTargetedChangeZoneLibraryPositionUnresolvableValueIsLoud pins the
// fail-closed direction: a LibraryPosition$ value the Num grammar cannot
// resolve emits ONE loud Note and the MoveZone bottom append stands -- the
// cards are never silently placed by a guessed position, and never silently
// left at the bottom either (the pre-fix behaviour for the whole class was
// the silent half of this).
func TestTargetedChangeZoneLibraryPositionUnresolvableValueIsLoud(t *testing.T) {
	h := newHost(t, 2)
	lzFills(t, h, 2)
	tgt := lzCreature(t, h, 0, state.ZBattlefield, "Vanished")
	sa := sa(t, "DB$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Library | LibraryPosition$ TriggeredLKI")
	Resolve(h, &Ctx{Controller: 0, Source: tgt,
		Targets: []state.Target{{Obj: tgt}}}, sa)
	if z := h.g.Obj(tgt).Zone; z != state.ZLibrary {
		t.Fatalf("target zone = %v, want library (the move itself must still land)", z)
	}
	var notes []string
	for _, e := range h.log {
		if e.Kind == events.Note && strings.Contains(e.Text, "LibraryPosition$") {
			notes = append(notes, e.Text)
		}
	}
	if len(notes) != 1 {
		t.Fatalf("LibraryPosition$ Notes = %v, want exactly one loud diagnostic", notes)
	}
}

// TestTargetedChangeZoneLibraryMultiTargetOrderIsKept pins the multi-target
// half of the fix: several targets moving to one library are placed as a
// block in TARGET order (the order the moves landed), never re-sorted.
func TestTargetedChangeZoneLibraryMultiTargetOrderIsKept(t *testing.T) {
	h := newHost(t, 2)
	fills := lzFills(t, h, 2)
	first := lzCreature(t, h, 0, state.ZBattlefield, "Alpha")
	second := lzCreature(t, h, 0, state.ZBattlefield, "Beta")
	sa := sa(t, "DB$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Library | LibraryPosition$ 0")
	// TargetsOffered marks the targets as an already-answered ask, so the
	// resolver consumes Ctx.Targets instead of posing a fresh ask (the same
	// contract rules' announcement ask rides).
	Resolve(h, &Ctx{Controller: 0, Source: first, TargetsOffered: true,
		Targets: []state.Target{{Obj: first}, {Obj: second}}}, sa)

	lib := lzLibraryOrderIDs(h, 0)
	want := append([]state.ObjID{first, second}, fills...)
	if !slices.Equal(lib, want) {
		t.Fatalf("library = %v, want %v (target order kept at the top)", lib, want)
	}
}

// TestTargetedChangeZoneLibraryPositionOwnerLibraryIsUsed pins the owner
// read: a battlefield creature CONTROLLED by the opponent still returns to
// its OWNER's library (the same rule effChangeZoneAll applies), so the
// placement order is emitted for the owner's library, not the controller's.
func TestTargetedChangeZoneLibraryPositionOwnerLibraryIsUsed(t *testing.T) {
	h := newHost(t, 2)
	fills := lzFills(t, h, 2)
	tgt := lzCreature(t, h, 1, state.ZBattlefield, "Stolen")
	if o := h.g.Obj(tgt); o.Controller != 1 {
		t.Fatalf("precondition: controller = %d, want 1", o.Controller)
	}
	sa := sa(t, "DB$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Library | LibraryPosition$ 0")
	Resolve(h, &Ctx{Controller: 1, Source: tgt,
		Targets: []state.Target{{Obj: tgt}}}, sa)

	if lib := lzLibraryOrderIDs(h, 1); !slices.Equal(lib, []state.ObjID{tgt}) {
		t.Fatalf("owner's library = %v, want %v (the moved card placed in its OWNER's library)", lib, []state.ObjID{tgt})
	}
	if lib := lzLibraryOrderIDs(h, 0); !slices.Equal(lib, fills) {
		t.Fatalf("controller's library = %v, want %v (untouched)", lib, fills)
	}
}

// targetedLibraryPositionCarriers is the measured class at the corpus pin:
// every corpus card carrying a targeted (ValidTgts$) ChangeZone whose
// Destination$ is Library with an explicit LibraryPosition$ (golgari_thug1).
// 140 cards: 70 top, 51 bottom, 12 second-from-top, 3+1 deeper, 3
// SVar-resolved X (Quarry Colossus, Unexpectedly Absent, Time Out). The pin
// is bidirectional: a corpus bump or a support change that alters membership
// fails here and forces a deliberate re-measure, so the class cannot silently
// grow (a new carrier with an unmodelled position value) or shrink silently.
var targetedLibraryPositionCarriers = []string{
	"Aethertow", "Agonizing Memories", "Anchor to the Aether", "Aura Extraction", "Azorius Charm",
	"Banishing Stroke", "Banishment Decree", "Bant Charm", "Barkform Harvester", "Blade of the Swarm",
	"Boseiju Reaches Skyward", "Bow of Nylea", "Brutalizer Exarch", "Bury in Books", "Canal Dredger",
	"Chimney Goyf", "Chimney Imp", "Chittering Rats", "Chrome Companion", "Chronostutter", "Civic Guildmage",
	"Cogwork Archivist", "Commit", "Condemn", "Conjurer's Bauble", "Cybernetic Specialist", "Deem Inferior",
	"Disempower", "Dovin's Dismissal", "Drake Stone", "Dutiful Knowledge Seeker", "Epitaph Golem",
	"Eternal Isolation", "Excommunicate", "Fallow Earth", "False Mourning", "Flitting Guerrilla",
	"Forced Landing", "Forced Retreat", "Golgari Thug", "Gone Missing", "Grasp of Phantoms", "Grazing Kelpie",
	"Griptide", "Hag Hedge-Mage", "Happy Hogan, Bodyguard", "Hide", "Hoarding Recluse", "Hoverstone Pilgrim",
	"Hunting Drake", "Ice Magic", "Isolation at Orthanc", "Jade-Cast Sentinel", "Jeskai Charm", "Junktroller",
	"Keeper of the Cadence", "King Crab", "Landscaper Colos", "Lashweed Lurker", "Lin Sivvi, Defiant Hero",
	"Lodestone Bauble", "Looming Hoverguard", "Lost Days", "Lost Hours", "Lost to Legend",
	"Malevolent Chandelier", "Meldweb Curator", "Mercenary Informer", "Metamorphose", "Misinformation",
	"Mistveil Plains", "Mystic Repeal", "Nantuko Tracer", "Natural Obsolescence", "Nevermaker",
	"Nightscape Apprentice", "Noxious Revival", "Ominous Cemetery", "Oust", "Painful Memories",
	"Paradox Shaper", "Phyrexian Archivist", "Phyrexian Chimney Imp", "Plow Under", "Primal Command",
	"Proteus Staff", "Quarry Colossus", "Reality Scramble", "Rebel Informer", "Reclaim", "Reinforcements",
	"Reito Lantern", "Reito Sentinel", "Repel", "Riptide Gearhulk", "Roil Spout", "Rootrunner", "Run Aground",
	"Salvage", "Scheming Symmetry", "Sea of Sand", "Sentinel of Lost Lore", "Set Adrift", "Shadow Guildmage",
	"Spin into Myth", "Stunted Growth", "Submerge", "Sundering Archaic", "Sunscape Apprentice",
	"Swiftgear Drake", "Swirling Torrent", "Synchronized Eviction", "Teferi, Hero of Dominaria",
	"Teferi, Timeless Voyager", "Tel-Jilad Stylus", "Temporal Cleansing", "Temporal Eddy", "Temporal Spring",
	"The Spot's Portal", "Time Ebb", "Time Out", "Time Reaper", "Tomb Trawler", "Totally Lost",
	"Transplant Theorist", "Trickster's Stratagem", "Unexpectedly Absent", "Uproot", "Vanishment",
	"Varragoth, Bloodsky Sire", "Vedalken Dismisser", "Vendilion Clique", "Vessel of Endless Rest",
	"Void Stalker", "Volrath's Dungeon", "Wan Shi Tong, All-Knowing", "Warrant", "Whisk Away",
}

// TestTargetedLibraryPositionCensus walks every SVar body and ability chain
// in the corpus and asserts the measured carrier set equals the pin. A
// carrier whose position value the object-target path cannot resolve (a
// literal, the bare X, or an SVar name the card's own table defines) is
// reported here by name.
func TestTargetedLibraryPositionCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	seen := map[*cards.SA]bool{}
	carriers := map[string]bool{}
	unsupported := map[string]string{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			var walk func(sa *cards.SA)
			walk = func(sa *cards.SA) {
				for ; sa != nil; sa = sa.Sub {
					if seen[sa] {
						continue
					}
					seen[sa] = true
					if sa.API != "ChangeZone" || !strings.Contains(sa.Params["Destination"], "Library") {
						continue
					}
					if strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
						continue
					}
					raw := strings.TrimSpace(sa.Params["LibraryPosition"])
					if raw == "" {
						continue
					}
					carriers[f.Name] = true
					if _, err := strconv.Atoi(raw); err != nil && raw != "X" {
						if _, ok := f.SVars[raw]; !ok {
							unsupported[f.Name] = raw
						}
					}
					break // one carrier per chain, not per sub
				}
			}
			for _, ab := range f.Abilities {
				walk(ab)
			}
			for _, tr := range f.Triggers {
				walk(tr.Effect)
			}
			for _, rp := range f.Repls {
				walk(rp.With)
			}
			for name := range f.SVars {
				walk(cards.ResolveSVar(f.SVars, name))
			}
		}
	}
	var measured []string
	for n := range carriers {
		measured = append(measured, n)
	}
	slices.Sort(measured)
	slices.Sort(targetedLibraryPositionCarriers)
	if !slices.Equal(measured, targetedLibraryPositionCarriers) {
		t.Errorf("targeted LibraryPosition$ carrier set drifted: measured %d, pinned %d", len(measured), len(targetedLibraryPositionCarriers))
		for _, n := range measured {
			if !slices.Contains(targetedLibraryPositionCarriers, n) {
				t.Errorf("  measured but not pinned: %s", n)
			}
		}
		for _, n := range targetedLibraryPositionCarriers {
			if !slices.Contains(measured, n) {
				t.Errorf("  pinned but not measured: %s", n)
			}
		}
	}
	for name, raw := range unsupported {
		t.Errorf("%s carries LibraryPosition$ %q, a value the object-target path cannot resolve (measure and classify it)", name, raw)
	}
}
