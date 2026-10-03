package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Every rules test runs with recycled snapshot arenas poisoned: a reader
// that kept a pointer into a window's snapshot past the window's close reads
// garbage, so TestHeads, the replay tests and every trigger test double as
// the recycling's aliasing check. Poisoning happens only at recycle time and
// every reused slot is overwritten before it is read, so correct code is
// unaffected by it.
func init() { triggerSnapshotPoison = true }

// TestTriggerBeforeReadsAreClassified holds every read of e.triggerBefore in
// the package to the shapes the snapshot recycling is argued safe for
// (trigger_snapshot_pool.go): a record takes the snapshot only through
// retainTriggerBefore, which marks it retained; the window-local saves
// (before/saved) only restore it; checkTriggers reads the board through it.
// A new read shape fails here until it is argued and added.
func TestTriggerBeforeReadsAreClassified(t *testing.T) {
	allowed := []*regexp.Regexp{
		// The field declaration, with its clone-policy tag (clone_policy_test.go).
		regexp.MustCompile("^\\s*triggerBefore \\*triggerSnapshot( +`clone:\"[a-z]+\"`)?$"),
		regexp.MustCompile(`^\s*e\.triggerBefore = [A-Za-z.]+$`),
		regexp.MustCompile(`^\s*defer func\(\) \{ e\.triggerBefore = (before|saved) \}\(\)$`),
		regexp.MustCompile(`^\s*(before|saved) := e\.triggerBefore$`),
		regexp.MustCompile(`^\s*saved, before := e\.applyingReplacement, e\.triggerBefore$`),
		regexp.MustCompile(`e\.triggerBefore (==|!=) nil`),
		regexp.MustCompile(`^\s*e\.triggerBefore, [a-zA-Z.]+ = [a-zA-Z.]+, [a-zA-Z.]+$`),
		regexp.MustCompile(`^\s*e\.applyingReplacement, e\.triggerBefore = [a-zA-Z.]+, [a-zA-Z.]+$`),
	}
	boardRead := regexp.MustCompile(`e\.triggerBefore\.(game|continuous)\b`)
	// A record field set from a window-local save would take the snapshot
	// without marking it.
	forbidden := regexp.MustCompile(`before:\s+(before|saved)\b`)
	word := regexp.MustCompile(`\btriggerBefore\b`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "trigger_snapshot_pool.go" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			code := line
			if j := strings.Index(code, "//"); j >= 0 {
				code = code[:j]
			}
			if forbidden.MatchString(code) {
				t.Errorf("%s:%d: a record takes a window-local snapshot save: %s", f, i+1, strings.TrimSpace(line))
			}
			if !word.MatchString(code) {
				continue
			}
			ok := f == "trigger_match.go" && boardRead.MatchString(code)
			for _, re := range allowed {
				ok = ok || re.MatchString(strings.TrimRight(code, " \t"))
			}
			if !ok {
				t.Errorf("%s:%d: unclassified e.triggerBefore read (a record must take it through retainTriggerBefore): %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// TestTriggerSnapshotRecycling pins the pool's lifecycle: an unretained
// window's arena is recycled into the next window's snapshot, a retained one
// is never recycled (its board stays readable), a by-value engine copy never
// touches the owner's pool, and Release hands the pool to the next clone.
func TestTriggerSnapshotRecycling(t *testing.T) {
	c, diags := cards.ParseBytes("snap.txt", []byte("Name:Snapshot Bear\nTypes:Creature\nPT:2/2\n"))
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	c.Link()
	deck := make([]*cards.Card, 20)
	for i := range deck {
		deck[i] = c
	}
	e := New(Config{Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}})

	// An unretained window recycles its arena, and the next snapshot reuses it.
	s1 := e.snapshotTriggerBoard()
	arena := &s1.game.Objs[:cap(s1.game.Objs)][0]
	e.triggerBefore = s1
	e.closeTriggerWindow(s1, nil)
	if e.triggerBefore != nil || s1.game != nil {
		t.Fatal("closing a window did not restore the outer snapshot and drop its own board")
	}
	s2 := e.snapshotTriggerBoard()
	if &s2.game.Objs[:cap(s2.game.Objs)][0] != arena {
		t.Fatal("the next window's snapshot did not reuse the recycled arena")
	}
	for i := range e.G.Objs {
		if s2.game.Objs[i].ID != e.G.Objs[i].ID || s2.game.Objs[i].Zone != e.G.Objs[i].Zone || s2.game.Objs[i].Controller != e.G.Objs[i].Controller {
			t.Fatalf("recycled snapshot slot %d = %+v, want the live object %+v", i, s2.game.Objs[i], e.G.Objs[i])
		}
	}

	// A retained snapshot survives its window with its board intact.
	e.triggerBefore = s2
	if got := e.retainTriggerBefore(); got != s2 || !s2.retained {
		t.Fatal("retainTriggerBefore did not mark and return the window's snapshot")
	}
	want := append([]state.Object(nil), s2.game.Objs...)
	e.closeTriggerWindow(s2, nil)
	if s2.game == nil {
		t.Fatal("a retained snapshot's board was recycled")
	}
	s3 := e.snapshotTriggerBoard()
	if &s3.game.Objs[0] == &s2.game.Objs[0] {
		t.Fatal("a retained snapshot's arena was handed to the next window")
	}
	for i := range want {
		if s2.game.Objs[i].ID != want[i].ID || s2.game.Objs[i].Zone != want[i].Zone {
			t.Fatalf("a retained snapshot's slot %d changed: %+v", i, s2.game.Objs[i])
		}
	}

	// A by-value copy (entryPreview's preview) neither takes from nor feeds
	// the owner's pool.
	e.triggerBefore = s3
	e.closeTriggerWindow(s3, nil)
	pooled := len(e.snapPool.objs)
	cp := *e
	s4 := cp.snapshotTriggerBoard()
	cp.closeTriggerWindow(s4, nil)
	if len(e.snapPool.objs) != pooled {
		t.Fatalf("a by-value copy changed the owner's pool: %d arenas, want %d", len(e.snapPool.objs), pooled)
	}

	// A released clone's arenas seed the next clone's pool.
	cl := e.Clone()
	s5 := cl.snapshotTriggerBoard()
	cl.triggerBefore = s5
	cl.closeTriggerWindow(s5, nil)
	sp := cl.Release()
	if len(sp.snapObjs) == 0 {
		t.Fatal("Release dropped the clone's snapshot arenas")
	}
	next := e.CloneInto(&sp)
	if next.snapPool == nil || next.snapPool.owner != next || len(next.snapPool.objs) == 0 {
		t.Fatal("CloneInto did not adopt the spare's snapshot arenas")
	}
	if e.snapPool == next.snapPool {
		t.Fatal("a clone shares its original's snapshot pool")
	}
}

// TestLookBackLKIOutlivesItsWindow pins the one pointer a matched look-back
// trigger keeps: a dies trigger matched through the window's snapshot holds
// the departing object's look-back state after the window closed and its
// arena was recycled (poisoned, under test), so its LKI is a copy, not a
// pointer into the arena.
func TestLookBackLKIOutlivesItsWindow(t *testing.T) {
	c, diags := cards.ParseBytes("snap.txt", []byte("Name:Snapshot Mourner\nTypes:Creature\nPT:2/2\n"+
		"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card.Self | Execute$ TrigGain | TriggerDescription$ x\n"+
		"SVar:TrigGain:DB$ GainLife | LifeAmount$ 1\n"))
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	c.Link()
	deck := make([]*cards.Card, 20)
	for i := range deck {
		deck[i] = c
	}
	e := New(Config{Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}})
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) == 0 {
		t.Fatal("precondition: no card in hand")
	}
	id := hand[0]
	placeOnBattlefield(t, e, id)
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.triggerBefore != nil || e.snapPool == nil || len(e.snapPool.objs) == 0 {
		t.Fatal("precondition: the departure window did not close and recycle its snapshot")
	}
	found := false
	for _, pt := range e.pendingTriggers {
		if pt.Ctx.LKI == nil {
			continue
		}
		found = true
		if pt.Ctx.LKI.ID != id || pt.Ctx.LKI.Zone != state.ZBattlefield || pt.Ctx.LKI.Controller != 0 {
			t.Fatalf("the dies trigger's LKI changed after its window closed: %+v", *pt.Ctx.LKI)
		}
	}
	if !found {
		t.Fatalf("precondition: the dies trigger did not queue with an LKI (%d pending)", len(e.pendingTriggers))
	}
}
