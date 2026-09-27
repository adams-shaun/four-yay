package host

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// sampleLoader serves testutil.SampleDecks' four decks under the names
// "a".."d", so every host test is corpus-free and deterministic.
func sampleLoader(t *testing.T) func(string) (Deck, error) {
	t.Helper()
	names, decks := testutil.SampleDecks(t, 4)
	byName := map[string][]*cards.Card{}
	for i, n := range names {
		byName[n] = decks[i]
	}
	return func(name string) (Deck, error) {
		cs, ok := byName[name]
		if !ok {
			return Deck{}, ErrNotFound
		}
		return Deck{Name: name, Cards: cs}, nil
	}
}

func testOptions(t *testing.T) Options {
	t.Helper()
	takeMatchSlot(t)
	return Options{LoadDeck: sampleLoader(t), Sleep: func(time.Duration, <-chan struct{}) {}}
}

func fourSeatTable(id TableID, perpetual bool) TableConfig {
	return TableConfig{ID: id, Name: "Table " + string(id), Seats: 4, Decks: []string{"a", "b", "c", "d"},
		Seed: 99, Pace: 0, Spectator: view.Omniscient, Perpetual: perpetual}
}

func TestATablePlaysOneMatchToCompletionAndGoesIdle(t *testing.T) {
	t.Parallel()
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
		t.Fatal(err)
	}
	if got := r.Tables(); len(got) != 1 || got[0].State != protocol.TableIdle || got[0].Match != 0 {
		t.Fatalf("before start: %+v", got)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")
	ti := r.Tables()[0]
	if ti.State != protocol.TableIdle || ti.Match != 1 || ti.Seats != 4 || ti.Spectator != "omniscient" {
		t.Fatalf("after one match: %+v", ti)
	}
	ms, err := r.Matches("t1")
	if err != nil || len(ms) != 1 {
		t.Fatalf("matches %v, %v", ms, err)
	}
	m := ms[0]
	if m.State != protocol.MatchFinished || m.Match != 1 || m.Seed != MatchSeed(99, 1) || m.Head == "" || m.Events < 100 || m.Turns < 2 {
		t.Fatalf("match info %+v", m)
	}
	if m.Result != "win" && m.Result != "draw" {
		t.Fatalf("result %q", m.Result)
	}
	if (m.Result == "win") != (m.Winner != nil) {
		t.Fatalf("winner/result disagree: %+v", m)
	}
	if len(m.Seats) != 4 || m.Seats[0].Colour != protocol.SeatColours[0] || m.Seats[1].Deck != "c" {
		// k=1: seat i plays Decks[(i+1)%4] → a,b,c,d rotate to b,c,d,a.
		t.Fatalf("seats %+v", m.Seats)
	}
}

func TestHostedPoliciesReplayDeterministically(t *testing.T) {
	t.Parallel()
	type runResult struct {
		info protocol.MatchInfo
		log  *events.Log
	}
	run := func(policy string, autoPay bool) runResult {
		r, _ := New(testOptions(t))
		defer r.Close()
		tableCfg := fourSeatTable("t1", false)
		tableCfg.BotPolicy = policy
		tableCfg.AutoMana = autoPay
		tableCfg.BotAutoPayMana = autoPay
		if err := r.AddTable(tableCfg); err != nil {
			t.Fatal(err)
		}
		if err := r.Start("t1"); err != nil {
			t.Fatal(err)
		}
		r.Wait("t1")
		r.mu.RLock()
		tab := r.tables["t1"]
		r.mu.RUnlock()
		tab.mu.RLock()
		m := tab.history[0]
		tab.mu.RUnlock()
		m.mu.RLock()
		info, log, rulesCfg := m.info(), m.e.L.Clone(), m.cfg
		m.mu.RUnlock()
		replayed, err := replay.Replay(log, rulesCfg)
		if err != nil {
			t.Fatalf("%q replay: %v", policy, err)
		}
		if got, want := replayed.L.Head(), info.Head; got != want {
			t.Fatalf("%q replay head %s, want %s", policy, got, want)
		}
		return runResult{info: info, log: log}
	}
	type policyCase struct {
		name    string
		policy  string
		autoPay bool
	}
	results := make(map[string]runResult, 8)
	for _, tc := range []policyCase{
		{name: "default", policy: ""},
		{name: BotPolicy, policy: BotPolicy},
		{name: LethalPressurePolicy, policy: LethalPressurePolicy},
		{name: CastProfilePolicy, policy: CastProfilePolicy},
		{name: "default-auto-pay", policy: "", autoPay: true},
		{name: BotPolicy + "-auto-pay", policy: BotPolicy, autoPay: true},
		{name: LethalPressurePolicy + "-auto-pay", policy: LethalPressurePolicy, autoPay: true},
		{name: CastProfilePolicy + "-auto-pay", policy: CastProfilePolicy, autoPay: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := run(tc.policy, tc.autoPay), run(tc.policy, tc.autoPay)
			for _, got := range []runResult{a, b} {
				if got.info.State != protocol.MatchFinished || (got.info.Result != "win" && got.info.Result != "draw") || (got.info.Result == "win") != (got.info.Winner != nil) {
					t.Fatalf("%q did not finish with a valid outcome: %+v", tc.name, got.info)
				}
			}
			if !reflect.DeepEqual(a.log.Events, b.log.Events) || !reflect.DeepEqual(a.log.Intents, b.log.Intents) || a.info.Head != b.info.Head || a.info.Result != b.info.Result || !reflect.DeepEqual(a.info.Winner, b.info.Winner) {
				t.Fatalf("two %q runs differ: %+v vs %+v", tc.name, a.info, b.info)
			}
			if tc.autoPay {
				planned := false
				for _, ev := range a.log.Events {
					if ev.Kind == events.DecisionMade && strings.Contains(ev.Text, ";payment:") {
						planned = true
						break
					}
				}
				if !planned {
					t.Fatalf("%q auto-pay run has no planned DecisionMade event", tc.name)
				}
			}
			results[tc.name] = a
		})
	}
	defaultRun, explicitBot := results["default"], results[BotPolicy]
	if !reflect.DeepEqual(defaultRun.log.Events, explicitBot.log.Events) || !reflect.DeepEqual(defaultRun.log.Intents, explicitBot.log.Intents) || defaultRun.info.Head != explicitBot.info.Head || defaultRun.info.Result != explicitBot.info.Result || !reflect.DeepEqual(defaultRun.info.Winner, explicitBot.info.Winner) {
		t.Fatalf("omitted policy and explicit %q differ: %+v vs %+v", BotPolicy, defaultRun.info, explicitBot.info)
	}
	// The cast-profile policy on the embedded default profile is
	// intent-identical to the production bot over a whole hosted match (the
	// same weights, pinned equal by botpolicy's profile tests) -- so its
	// event log, intents, head, result and winner must all match the
	// explicit-bot run exactly.
	castProfileRun := results[CastProfilePolicy]
	if !reflect.DeepEqual(castProfileRun.log.Events, explicitBot.log.Events) || !reflect.DeepEqual(castProfileRun.log.Intents, explicitBot.log.Intents) || castProfileRun.info.Head != explicitBot.info.Head || castProfileRun.info.Result != explicitBot.info.Result || !reflect.DeepEqual(castProfileRun.info.Winner, explicitBot.info.Winner) {
		t.Fatalf("%q on the default profile and explicit %q differ: %+v vs %+v", CastProfilePolicy, BotPolicy, castProfileRun.info, explicitBot.info)
	}
}

func TestAPerpetualTableStartsTheNextMatchWithTheDerivedSeed(t *testing.T) {
	t.Parallel()
	cooled := 0
	o := testOptions(t)
	o.Cooldown = time.Second
	var r *Registry
	o.Sleep = func(d time.Duration, stop <-chan struct{}) {
		if d == time.Second {
			cooled++
			if cooled == 2 {
				// Stop after two matches. Close is asynchronous (it waits for
				// this very goroutine), so wait for its stop signal before
				// returning, or the loop could start a third match first.
				go r.Close()
				<-r.Done()
			}
		}
	}
	r, _ = New(o)
	if err := r.AddTable(fourSeatTable("t1", true)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")
	ms, _ := r.Matches("t1")
	if len(ms) < 2 {
		t.Fatalf("perpetual table played %d matches", len(ms))
	}
	if ms[0].Seed != MatchSeed(99, 1) || ms[1].Seed != MatchSeed(99, 2) || ms[0].Head == ms[1].Head {
		t.Fatalf("seeds/heads %+v %+v", ms[0], ms[1])
	}
	if ms[1].Seats[0].Deck != "c" { // k=2: seat 0 plays Decks[(0+2)%4]
		t.Fatalf("decks did not rotate: %+v", ms[1].Seats)
	}
}

func TestCloseAbortsALiveMatch(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	var r *Registry
	n := 0
	o.Sleep = func(time.Duration, <-chan struct{}) {
		n++
		if n == 50 {
			go r.Close()
			<-r.Done()
		}
	}
	r, _ = New(o)
	if err := r.AddTable(fourSeatTable("t1", true)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")
	ms, _ := r.Matches("t1")
	if len(ms) != 1 || ms[0].State != protocol.MatchAborted || ms[0].Result != "" {
		t.Fatalf("after Close: %+v", ms)
	}
	if r.Tables()[0].State != protocol.TableIdle {
		t.Fatalf("table state %s after Close", r.Tables()[0].State)
	}
}

// blockingSeat never answers on its own: Decide blocks until ctx is
// cancelled, the shape a disconnected human seat (Task 25) will eventually
// have. entered is closed the moment Decide is called, so a test can wait
// for the table to be genuinely stuck inside a decision before it acts —
// otherwise Close could win the race and abort the match at play's
// top-of-loop stop check without ever exercising ctx cancellation at all.
type blockingSeat struct{ entered chan struct{} }

func (s blockingSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	close(s.entered)
	<-ctx.Done()
	return decision.Intent{}, ctx.Err()
}

// TestCloseCancelsASeatBlockedInDecide is Ruling FL-17: t.stop is polled
// only between decisions, so once the loop is inside Decide, cancelling
// the table's own context is the only way to unblock it. Without that
// wiring this test hangs — Close would wait on r.wg forever.
func TestCloseCancelsASeatBlockedInDecide(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	o := testOptions(t)
	o.Seats = func(names []string, seed uint64) []seat.Seat {
		out := make([]seat.Seat, len(names))
		for i := range out {
			out[i] = blockingSeat{entered: entered}
		}
		return out
	}
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("seat's Decide was never called")
	}
	closed := make(chan struct{})
	go func() {
		r.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return: a seat blocked in Decide was never cancelled")
	}
	r.Wait("t1")
	ms, _ := r.Matches("t1")
	if len(ms) != 1 || ms[0].State != protocol.MatchCrashed {
		t.Fatalf("a cancelled seat should crash the match: %+v", ms)
	}
}

// TestCloseInterruptsALongCooldown is Ruling FL-18: Options.Sleep now takes
// the table's stop channel, and the default implementation (registry.go's
// defaultSleep) races a real timer against it. This test supplies its own
// Sleep to prove the *contract* — a cooldown sleep must return as soon as
// stop closes, not after the full duration — independent of that default.
func TestCloseInterruptsALongCooldown(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	o.Cooldown = time.Hour
	var r *Registry
	stopped := make(chan struct{})
	o.Sleep = func(d time.Duration, stop <-chan struct{}) {
		if d != time.Hour {
			return // the per-decision Pace sleeps (d==0); ignore them.
		}
		// Trigger Close from inside the cooldown sleep itself, then prove
		// this very call returns because stop fired, not because an hour
		// actually elapsed.
		go r.Close()
		select {
		case <-stop:
			close(stopped)
		case <-time.After(5 * time.Second):
			t.Error("cooldown sleep was not asked to stop")
		}
	}
	r, _ = New(o)
	if err := r.AddTable(fourSeatTable("t1", true)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		r.Wait("t1")
		close(done)
	}()
	// The match itself takes seconds (more under -race); the promptness
	// clock starts only once the cooldown sleep has been reached and Close
	// has been triggered from inside it.
	select {
	case <-stopped:
	case <-time.After(2 * time.Minute):
		t.Fatal("the match never reached its cooldown sleep")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return promptly from a long cooldown")
	}
}

// TestConcurrentReadersDuringALiveMatch is Important #4: every other
// test's Tables()/Matches() call happens strictly before Start or after
// Wait, so -race never actually exercised the RWMutex design this task
// exists for. This one reads continuously while two full matches play out
// on a perpetual table, run under -race by the package's own test command.
func TestConcurrentReadersDuringALiveMatch(t *testing.T) {
	t.Parallel()
	const cooldown = time.Millisecond
	o := testOptions(t)
	o.Cooldown = cooldown
	cooled := 0
	var r *Registry
	o.Sleep = func(d time.Duration, stop <-chan struct{}) {
		if d != cooldown {
			return
		}
		cooled++
		if cooled == 2 {
			go r.Close()
			<-r.Done()
		}
	}
	r, _ = New(o)
	if err := r.AddTable(fourSeatTable("t1", true)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	readers := make(chan struct{})
	go func() {
		defer close(readers)
		for {
			select {
			case <-r.Done():
				return
			default:
			}
			r.Tables()
			if _, err := r.Matches("t1"); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	r.Wait("t1")
	<-readers
}

func TestConfigurationIsValidated(t *testing.T) {
	t.Parallel()
	r, _ := New(testOptions(t))
	defer r.Close()
	bad := []TableConfig{
		{},
		{ID: "x", Seats: 0, Decks: []string{"a"}},
		{ID: "x", Seats: 9, Decks: []string{"a"}},
		{ID: "x", Seats: 2},
		{ID: "x", Seats: 2, Decks: []string{"a"}, Spectator: view.Seat},
		{ID: "x", Seats: 2, Decks: []string{"nope"}},
	}
	for i, c := range bad {
		if err := r.AddTable(c); err == nil {
			t.Errorf("config %d accepted: %+v", i, c)
		}
	}
	good := TableConfig{ID: "x", Seats: 2, Decks: []string{"a"}, Spectator: view.Public}
	if err := r.AddTable(good); err != nil {
		t.Fatal(err)
	}
	if err := r.AddTable(good); err == nil {
		t.Fatal("duplicate table id accepted")
	}
	if err := r.Start("missing"); err == nil {
		t.Fatal("Start of an unknown table succeeded")
	}
	if _, err := r.Matches("missing"); err == nil {
		t.Fatal("Matches of an unknown table succeeded")
	}
}

func TestTableBotPolicyDefaultsAndRejectsUnknown(t *testing.T) {
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	good := TableConfig{ID: "good", Seats: 2, Decks: []string{"a"}, Spectator: view.Public}
	if err := r.AddTable(good); err != nil {
		t.Fatal(err)
	}
	if got := r.Tables()[0].BotPolicy; got != BotPolicy {
		t.Fatalf("default policy = %q, want %q", got, BotPolicy)
	}
	bad := good
	bad.ID, bad.BotPolicy = "bad", "legacy"
	if err := r.AddTable(bad); err == nil {
		t.Fatal("legacy policy was accepted for a hosted table")
	}
}

func TestBotPolicyMetadataIsCarriedToMatch(t *testing.T) {
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cfg := TableConfig{ID: "t1", Seats: 2, Decks: []string{"a", "b"}, Spectator: view.Public, BotPolicy: LethalPressurePolicy}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	tab := r.tables["t1"]
	r.mu.RUnlock()
	m, err := r.newMatch(tab, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.info().BotPolicy; got != LethalPressurePolicy {
		t.Fatalf("match policy = %q, want %q", got, LethalPressurePolicy)
	}
}

func TestDefaultSeatsConstructTheConfiguredPolicy(t *testing.T) {
	board := botpolicy.NewBoard(2)
	board.Life[1] = 10
	board.Creatures[1] = botpolicy.Creature{Power: 10, Toughness: 1, Controller: 0}
	board.Creatures[2] = botpolicy.Creature{Power: 1, Toughness: 1, Controller: 1}
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Max: 1,
		Options: []decision.Option{{Index: 0, Obj: state.ObjID(1), Player: 1}}}

	for _, tc := range []struct {
		policy string
		want   []int
	}{
		// AR7 lethal pressure was promoted into the default bot (held-out
		// 2,066-1,934 vs the old default), so both names now swing the
		// lethal-if-unblocked 10/1 into the 1/1 blocker.
		{policy: BotPolicy, want: []int{0}},
		{policy: LethalPressurePolicy, want: []int{0}},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			seats := defaultSeats(tc.policy, []string{"a", "b"}, 7)
			bot, ok := seats[0].(seat.BoardSeat)
			if !ok {
				t.Fatalf("configured seat %T does not accept public board decisions", seats[0])
			}
			in, err := bot.DecideBoard(context.Background(), board, d)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(in.Choices, tc.want) {
				t.Fatalf("%s chose %v, want %v", tc.policy, in.Choices, tc.want)
			}
		})
	}
}

func TestNewRequiresLoadDeckAndSleep(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{}); err == nil {
		t.Fatal("New accepted empty Options")
	}
}
