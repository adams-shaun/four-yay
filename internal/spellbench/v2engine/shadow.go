package v2engine

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

// The shadow-state check: join the engine's truth side channel (truth.go)
// with the agent's belief log (v2agent.BeliefRecord) on (game_id, seat,
// seat_step) and count, field by field, how often the agent's
// reconstruction disagrees with engine truth.

// FieldStat is one field's comparison count.
type FieldStat struct {
	Field      string   `json:"field"`
	Compared   int      `json:"compared"`
	Mismatches int      `json:"mismatches"`
	Examples   []string `json:"examples,omitempty"`
}

// Rate is the mismatch fraction.
func (f FieldStat) Rate() float64 {
	if f.Compared == 0 {
		return 0
	}
	return float64(f.Mismatches) / float64(f.Compared)
}

// ShadowReport is the whole comparison.
type ShadowReport struct {
	TruthRecords  int         `json:"truth_records"`
	BeliefRecords int         `json:"belief_records"`
	Joined        int         `json:"joined"`
	Fields        []FieldStat `json:"fields"`
}

type shadowKey struct {
	game, seat string
	step       int64
}

// ReadTruth reads a truth side-channel file.
func ReadTruth(path string) ([]TruthRecord, error) {
	var out []TruthRecord
	err := readLines(path, func(line []byte) error {
		var r TruthRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

// ReadBelief reads an agent belief log.
func ReadBelief(path string) ([]v2agent.BeliefRecord, error) {
	var out []v2agent.BeliefRecord
	err := readLines(path, func(line []byte) error {
		var r v2agent.BeliefRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

// readLines calls f per nonblank line; a ".gz" file is read as
// concatenated gzip members, and a member truncated by a killed writer
// ends the read quietly after its last complete line.
func readLines(path string, f func([]byte) error) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	var r io.Reader = fh
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(fh)
		if err != nil {
			return err
		}
		defer zr.Close()
		r = zr
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		if err := f(sc.Bytes()); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			var syn *json.SyntaxError
			if errors.As(err, &syn) {
				return nil // a torn last line
			}
			return err
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	return nil
}

type fieldTable struct {
	order []string
	stats map[string]*FieldStat
}

func (t *fieldTable) check(field string, equal bool, example func() string) {
	s := t.stats[field]
	if s == nil {
		s = &FieldStat{Field: field}
		t.stats[field] = s
		t.order = append(t.order, field)
	}
	s.Compared++
	if !equal {
		s.Mismatches++
		if len(s.Examples) < 3 {
			s.Examples = append(s.Examples, example())
		}
	}
}

// Shadow accumulates the comparison game by game, so a run's side channel
// (hundreds of MB) never has to be held in memory at once.
type Shadow struct {
	rep ShadowReport
	t   *fieldTable
}

// NewShadow starts an empty comparison.
func NewShadow() *Shadow {
	return &Shadow{t: &fieldTable{stats: map[string]*FieldStat{}}}
}

// AddGame compares one game's truth and belief records (any records of
// other games are joined just the same).
func (s *Shadow) AddGame(truth []TruthRecord, belief []v2agent.BeliefRecord) {
	s.rep.TruthRecords += len(truth)
	s.rep.BeliefRecords += len(belief)
	byKey := make(map[shadowKey]*TruthRecord, len(truth))
	for i := range truth {
		r := &truth[i]
		byKey[shadowKey{r.GameID, r.Seat, r.SeatStep}] = r
	}
	for i := range belief {
		b := &belief[i]
		tr := byKey[shadowKey{b.GameID, b.Seat, b.SeatStep}]
		if tr == nil {
			s.t.check("join:truth_record_present", false, func() string { return fmt.Sprintf("%s %s step %d", b.GameID, b.Seat, b.SeatStep) })
			continue
		}
		s.t.check("join:truth_record_present", true, nil)
		s.rep.Joined++
		compareRecord(s.t, tr, b)
	}
}

// Report is the comparison so far.
func (s *Shadow) Report() ShadowReport {
	rep := s.rep
	rep.Fields = nil
	for _, f := range s.t.order {
		rep.Fields = append(rep.Fields, *s.t.stats[f])
	}
	return rep
}

// CompareShadow joins truth and belief records and counts mismatches.
func CompareShadow(truth []TruthRecord, belief []v2agent.BeliefRecord) ShadowReport {
	s := NewShadow()
	s.AddGame(truth, belief)
	return s.Report()
}

// StreamTruth calls f with each game's truth records, in file order; a
// worker's side channel holds its games one after another.
func StreamTruth(path string, f func(gameID string, recs []TruthRecord)) error {
	var cur []TruthRecord
	flush := func() {
		if len(cur) > 0 {
			f(cur[0].GameID, cur)
		}
		cur = nil
	}
	err := readLines(path, func(line []byte) error {
		var r TruthRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		if len(cur) > 0 && cur[0].GameID != r.GameID {
			flush()
		}
		cur = append(cur, r)
		return nil
	})
	flush()
	return err
}

// BeliefGameID is the game id of a belief log's first record ("" when it
// has none).
func BeliefGameID(path string) (string, error) {
	id := ""
	err := readLines(path, func(line []byte) error {
		if id != "" {
			return errStop
		}
		var r struct {
			GameID string `json:"game_id"`
		}
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		id = r.GameID
		return errStop
	})
	if errors.Is(err, errStop) {
		err = nil
	}
	return id, err
}

var errStop = errors.New("stop")

func compareRecord(t *fieldTable, tr *TruthRecord, b *v2agent.BeliefRecord) {
	at := fmt.Sprintf("%s %s step %d (%s turn %d %s)", tr.GameID, tr.Seat, tr.SeatStep, tr.GorgeKind, tr.Turn, tr.PhaseStep)
	ex := func(what string, want, got any) func() string {
		return func() string { return fmt.Sprintf("%s: %s truth=%v belief=%v", at, what, want, got) }
	}
	t.check("game.turn", int64(tr.Turn) == b.Turn, ex("turn", tr.Turn, b.Turn))
	t.check("game.phase_step", tr.PhaseStep == b.PhaseStep, ex("phase", tr.PhaseStep, b.PhaseStep))
	t.check("game.active_seat", tr.Active == b.Active, ex("active", tr.Active, b.Active))
	for _, tp := range tr.Players {
		var bp *v2agent.BeliefPlayer
		for j := range b.Players {
			if b.Players[j].Seat == tp.Seat {
				bp = &b.Players[j]
			}
		}
		if bp == nil {
			t.check("player.present", false, ex("player "+tp.Seat, "present", "absent"))
			continue
		}
		who := "own."
		if tp.Seat != tr.Seat {
			who = "opp."
		}
		t.check("player."+who+"life", tp.Life == bp.Life, ex(tp.Seat+" life", tp.Life, bp.Life))
		t.check("player."+who+"hand_count", tp.HandSize == bp.HandCount, ex(tp.Seat+" hand", tp.HandSize, bp.HandCount))
		t.check("player."+who+"library_count", tp.LibrarySize == bp.LibraryCount, ex(tp.Seat+" library", tp.LibrarySize, bp.LibraryCount))
		t.check("player."+who+"graveyard_count", tp.GraveyardSize == bp.GraveyardSize, ex(tp.Seat+" graveyard", tp.GraveyardSize, bp.GraveyardSize))
		t.check("player."+who+"mana_pool", mapsEqual(tp.Pool, bp.Pool), ex(tp.Seat+" pool", tp.Pool, bp.Pool))
	}
	bobj := map[string]*v2agent.BeliefObject{} // lookup only
	for j := range b.Objects {
		bobj[b.Objects[j].ObjectID] = &b.Objects[j]
	}
	tseen := map[string]bool{} // lookup only
	for j := range tr.Objects {
		to := &tr.Objects[j]
		tseen[to.ObjectID] = true
		bo := bobj[to.ObjectID]
		z := to.Zone + "."
		t.check("object."+z+"present", bo != nil, ex(to.Zone+" "+to.Name, "present", "absent"))
		if bo == nil {
			continue
		}
		t.check("object."+z+"zone", bo.Zone == to.Zone, ex(to.Name+" zone", to.Zone, bo.Zone))
		if !to.FaceDown || to.Name != "" {
			t.check("object."+z+"name", bo.Name == to.Name, ex(to.ObjectID+" name", to.Name, bo.Name))
		}
		t.check("object."+z+"controller", bo.Controller == to.Controller, ex(to.Name+" controller", to.Controller, bo.Controller))
		t.check("object."+z+"owner", bo.Owner == to.Owner, ex(to.Name+" owner", to.Owner, bo.Owner))
		if to.Zone != "battlefield" {
			continue
		}
		t.check("object.battlefield.tapped", bo.Tapped == to.Tapped, ex(to.Name+" tapped", to.Tapped, bo.Tapped))
		t.check("object.battlefield.power", eqPtr(to.Power, bo.Power), ex(to.Name+" power", deref(to.Power), deref(bo.Power)))
		t.check("object.battlefield.toughness", eqPtr(to.Toughness, bo.Toughness), ex(to.Name+" toughness", deref(to.Toughness), deref(bo.Toughness)))
		t.check("object.battlefield.damage", bo.Damage == to.Damage, ex(to.Name+" damage", to.Damage, bo.Damage))
		t.check("object.battlefield.counters", counters32Equal(to.Counters, bo.Counters), ex(to.Name+" counters", to.Counters, bo.Counters))
		t.check("object.battlefield.summoning_sick", bo.SummonSick == to.SummonSick, ex(to.Name+" sick", to.SummonSick, bo.SummonSick))
		t.check("object.battlefield.attacking", bo.Attacking == to.Attacking, ex(to.Name+" attacking", to.Attacking, bo.Attacking))
		t.check("object.battlefield.token", bo.Token == to.Token, ex(to.Name+" token", to.Token, bo.Token))
		t.check("object.battlefield.keywords", strings.Join(dedupe(to.Keywords), ",") == strings.Join(dedupe(bo.Keywords), ","),
			ex(to.Name+" keywords", to.Keywords, bo.Keywords))
	}
	for j := range b.Objects {
		bo := &b.Objects[j]
		if !tseen[bo.ObjectID] {
			t.check("object."+bo.Zone+".extra_in_belief", false, ex(bo.Zone+" "+bo.Name, "absent", "present"))
		}
	}
	// Hidden information: the belief's inferred multisets against truth.
	t.check("hidden.own_library.exact", intMapsEqual(tr.OwnLibrary, b.OwnLibrary),
		ex("own library diff", "", diffCounts(tr.OwnLibrary, b.OwnLibrary)))
	total := 0
	for _, n := range tr.OwnLibrary {
		total += n
	}
	t.check("hidden.own_library.size_consistent", total == b.OwnLibraryCount, ex("own library size", total, b.OwnLibraryCount))
	oppTruth := map[string]int{}
	for k, n := range tr.OppHand {
		oppTruth[k] += n
	}
	for k, n := range tr.OppLibrary {
		oppTruth[k] += n
	}
	if b.OppHidden != nil {
		t.check("hidden.opp_hand_plus_library.exact", intMapsEqual(oppTruth, b.OppHidden),
			ex("opp hidden diff", "", diffCounts(oppTruth, b.OppHidden)))
	}
}

func deref(p *int32) any {
	if p == nil {
		return "null"
	}
	return *p
}

func eqPtr(a, b *int32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func mapsEqual(a, b map[string]int) bool { return intMapsEqual(a, b) }

func intMapsEqual(a, b map[string]int) bool {
	for k, v := range a {
		if v != 0 && b[k] != v {
			return false
		}
	}
	for k, v := range b {
		if v != 0 && a[k] != v {
			return false
		}
	}
	return true
}

func counters32Equal(a, b map[string]int32) bool {
	for k, v := range a {
		if v != 0 && b[k] != v {
			return false
		}
	}
	for k, v := range b {
		if v != 0 && a[k] != v {
			return false
		}
	}
	return true
}

func dedupe(xs []string) []string {
	s := append([]string(nil), xs...)
	sort.Strings(s)
	out := s[:0]
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// diffCounts renders belief - truth, name by name, in name order.
func diffCounts(truth, belief map[string]int) string {
	names := map[string]bool{} // lookup only
	for k := range truth {
		names[k] = true
	}
	for k := range belief {
		names[k] = true
	}
	var keys []string
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if d := belief[k] - truth[k]; d != 0 {
			parts = append(parts, fmt.Sprintf("%s%+d", k, d))
		}
	}
	return strings.Join(parts, " ")
}

// WriteShadowTable prints the report as an aligned table.
func WriteShadowTable(w io.Writer, rep ShadowReport) {
	fmt.Fprintf(w, "truth records %d, belief records %d, joined %d\n", rep.TruthRecords, rep.BeliefRecords, rep.Joined)
	fmt.Fprintf(w, "%-44s %9s %9s %8s\n", "field", "compared", "mismatch", "rate")
	for _, f := range rep.Fields {
		fmt.Fprintf(w, "%-44s %9d %9d %7.3f%%\n", f.Field, f.Compared, f.Mismatches, 100*f.Rate())
	}
	for _, f := range rep.Fields {
		for _, e := range f.Examples {
			fmt.Fprintf(w, "  %s: %s\n", f.Field, e)
		}
	}
}
