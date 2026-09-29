package host

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
)

// sidecar is <k>.json: everything about a match except its events and
// intents. Rewritten whole at start and at end; never carries a timestamp
// (PL-11), so two runs of one configuration write identical files.
type sidecar struct {
	Table string              `json:"table"`
	Match int                 `json:"match"`
	Seed  uint64              `json:"seed"`
	Seats []protocol.SeatInfo `json:"seats"`
	Names []string            `json:"names"`
	// PlayerNames is the display player name per seat (rules.Config.
	// PlayerNames), independent of the deck. Persisted so a restarted or
	// archived match rebuilds a view whose player box names the seat the
	// same way the live one did (R-8.4). Omitted/older sidecars rebuild
	// without it, which replays the display names back to the deck stem — the
	// pre-B3 behaviour — so nothing migrates (R-E5-2).
	PlayerNames []string `json:"player_names,omitempty"`
	Decks       []string `json:"decks"`
	Spectator   string   `json:"spectator"`
	State       string   `json:"state"`
	Result      string   `json:"result,omitempty"`
	Winner      *uint8   `json:"winner"`
	Head        string   `json:"head,omitempty"`
	Events      int      `json:"events"`
	Turns       int32    `json:"turns"`
	Reason      string   `json:"reason,omitempty"`
	// Mulligans is the London-mulligan allowance the match's rules.Config ran
	// with, persisted so a restart rebuilds a replay Config that reproduces
	// the match (R-8.4; host/viewat.go rebuilds its Config from this sidecar
	// alone, not from the live TableConfig). omitempty keeps both an old
	// sidecar that predates the field and a no-mulligan match loading as
	// exactly 0 — the value those matches played with — so nothing is
	// migrated or versioned (R-E5-2).
	Mulligans int `json:"mulligans,omitempty"`
	// Format, StartingLife and Commanders are the rest of the match's
	// rules.Config, for a Commander match: the construction format, the
	// opening life it actually played with (40, not the 0 TableConfig
	// carried) and each seat's commander indices, parallel to Decks.
	// Persisted so a
	// restart rebuilds a replay Config that reproduces a Commander match
	// exactly (R-8.4, viewat.go). omitempty keeps an old sidecar that
	// predates the fields loading as Format zero (constructed), life 0 (the
	// engine's 20) and no commanders — exactly the values those matches
	// played with (R-E5-2).
	Format       Format  `json:"format,omitempty"`
	StartingLife int32   `json:"starting_life,omitempty"`
	Commanders   [][]int `json:"commanders,omitempty"`
	// NameUniverse records whether the live match was played with a card-name
	// universe. A missing field is a pre-feature no-universe match, whose ETB
	// and mid-resolution fallbacks replay unchanged.
	NameUniverse bool `json:"name_universe,omitempty"`
	// NameUniverseNames is the immutable, sorted name list actually offered by
	// a universe-backed match. Keeping it beside the log prevents a later
	// corpus addition, removal or rename from changing a recorded numeric name
	// choice's label on replay.
	NameUniverseNames []string `json:"name_universe_names,omitempty"`
	BotPolicy         string   `json:"bot_policy"`
	// Refusals and Fallbacks are BP-06 ladder diagnostics: how many Submits
	// the engine refused and how many times the fallback rungs ran. They are
	// counters only and never reach an event, so a sidecar written without
	// them (omitempty) loads byte-identically and replays unchanged.
	Refusals  int `json:"refusals,omitempty"`
	Fallbacks int `json:"fallbacks,omitempty"`
	// RootRefusals is BP-08's honest-root diagnostic (spec §5.1): how many
	// bots.EnvSeat decisions were answered from a REFUSED root (nil engine).
	// Same counter-only contract as Refusals above.
	RootRefusals int `json:"root_refusals,omitempty"`
}

func (sc sidecar) info() protocol.MatchInfo {
	return protocol.MatchInfo{Table: sc.Table, Match: sc.Match, Seed: sc.Seed, Seats: sc.Seats, State: sc.State,
		Result: sc.Result, Winner: sc.Winner, Head: sc.Head, Events: sc.Events, Turns: sc.Turns, BotPolicy: sc.BotPolicy}
}

// matchFiles are a live match's append-only logs. The two offset slices
// below record the byte size of each file after every appended burst,
// parallel to the match's own bounds (evOff[j] is the events file's size
// once burst j — genesis for j=0, intent j-1 otherwise — is durable), and
// are what an in-place rewind truncates against (truncateTo). They are
// touched only by append and truncateTo, always on the match goroutine
// under the match lock.
type matchFiles struct {
	events, intents *os.File
	sync            bool
	evBytes         int64   // current events file size
	inBytes         int64   // current intents file size
	evOff           []int64 // events file size after each appended burst
	inOff           []int64 // intents file size after each appended burst
}

func tableDir(dir string, t TableID) string { return filepath.Join(dir, string(t)) }

func matchPath(dir string, t TableID, k int, ext string) string {
	return filepath.Join(tableDir(dir, t), strconv.Itoa(k)+ext)
}

// openMatchFiles creates (truncating) a match's two logs.
func openMatchFiles(dir string, t TableID, k int, sync bool) (*matchFiles, error) {
	if err := os.MkdirAll(tableDir(dir, t), 0o755); err != nil {
		return nil, err
	}
	ev, err := os.Create(matchPath(dir, t, k, ".events"))
	if err != nil {
		return nil, err
	}
	in, err := os.Create(matchPath(dir, t, k, ".intents"))
	if err != nil {
		ev.Close()
		return nil, err
	}
	return &matchFiles{events: ev, intents: in, sync: sync}, nil
}

// append writes one burst: the new events and the intent that produced
// them (nil for genesis), one JSON object per line, then fsyncs when
// configured (PL-13, opts.Sync). The per-burst byte offsets are recorded
// here on success, so a later rewind (truncateTo) can cut both files back
// to any recorded burst boundary in place.
//
// Burst atomicity (fix round 1): the intent that owns a burst is written
// BEFORE the burst's events. The two live in separate files and can never
// be committed atomically, and this ordering is the safe side: it guarantees
// the events stream is never longer than the intents that account for it.
// The one leftover a crash or kill can leave is an orphan intent at the
// tail whose events never made it, which read-time reconcile (reconcileLog)
// trims back so a restart always serves a consistent prefix.
func (f *matchFiles) append(evs []events.Event, in *decision.Intent) error {
	var inAdd int64
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		if _, err := f.intents.Write(append(b, '\n')); err != nil {
			return err
		}
		inAdd += int64(len(b) + 1)
		if f.sync {
			if err := f.intents.Sync(); err != nil {
				return err
			}
		}
	}
	var evAdd int64
	w := bufio.NewWriter(f.events)
	for _, e := range evs {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		w.Write(b)
		w.WriteByte('\n')
		evAdd += int64(len(b) + 1)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if f.sync {
		if err := f.events.Sync(); err != nil {
			return err
		}
	}
	// The burst is fully appended: record its byte offsets. evOff is indexed
	// by burst (genesis = 0), inOff likewise; genesis's intents offset stays
	// 0 because genesis carries no intent line.
	f.evBytes += evAdd
	f.evOff = append(f.evOff, f.evBytes)
	f.inBytes += inAdd
	f.inOff = append(f.inOff, f.inBytes)
	return nil
}

// truncateTo cuts both files back to the state they had after burst j
// (genesis for j == 0, intent j-1 otherwise) and repositions the write
// offsets there, so the next append continues the truncated log exactly
// where it left off — the in-place half of a rewind (host/undo.go). Each
// Truncate is one syscall; the Sync makes the shorter length durable
// before the second file is touched, and readLog's reconcileLog re-derives
// this same prefix from ANY interrupted intermediate state (one file cut,
// the other not, or a cut lost to a crash before its metadata reached
// disk), so the pair needs no cross-file atomicity. Bursts past j drop out
// of the offset slices with the bytes.
func (f *matchFiles) truncateTo(j int) error {
	if j < 0 || j >= len(f.evOff) || j >= len(f.inOff) {
		return fmt.Errorf("host: truncation point %d outside the persisted bursts (%d events-file bursts)", j, len(f.evOff))
	}
	if err := f.events.Truncate(f.evOff[j]); err != nil {
		return err
	}
	if f.sync {
		if err := f.events.Sync(); err != nil {
			return err
		}
	}
	if err := f.intents.Truncate(f.inOff[j]); err != nil {
		return err
	}
	if f.sync {
		if err := f.intents.Sync(); err != nil {
			return err
		}
	}
	// Rewind the write offsets: append resumes from the truncated end, not
	// from the old EOF, or the next burst would reopen the hole Truncate
	// closed.
	if _, err := f.events.Seek(f.evOff[j], 0); err != nil {
		return err
	}
	if _, err := f.intents.Seek(f.inOff[j], 0); err != nil {
		return err
	}
	f.evOff = f.evOff[:j+1]
	f.inOff = f.inOff[:j+1]
	f.evBytes = f.evOff[j]
	f.inBytes = f.inOff[j]
	return nil
}

func (f *matchFiles) close() {
	if f == nil {
		return
	}
	f.events.Close()
	f.intents.Close()
}

// writeSidecar writes <k>.json atomically (temp file + rename). When sync
// is set the temp is fsynced before the rename and the parent directory
// after it (fix round 1, fsync coverage) so the rename, not just the file's
// contents, is durable. The temp file is removed on every error path and
// consumed by the rename, so no *.tmp survives (writeFileAtomic).
func writeSidecar(dir string, sc sidecar, sync bool) error {
	if err := os.MkdirAll(tableDir(dir, TableID(sc.Table)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(matchPath(dir, TableID(sc.Table), sc.Match, ".json"), append(b, '\n'), sync)
}

// writeFileAtomic writes b to path via a temp file in the same directory:
// write + fsync the temp (contents durable before the rename exposes
// them), rename it over path, then fsync the parent directory (the rename
// itself durable). The temp is removed on every error path and consumed by
// the rename on success, so no *.tmp is ever left behind.
//
// The directory fsync is the one step some filesystems reject (e.g. EINVAL
// on Windows); it is best-effort, because the temp already sync'd and
// renamed is the durable part and failing the dir sync would otherwise
// break writes to otherwise-supported paths.
func writeFileAtomic(path string, b []byte, sync bool) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	if werr == nil && sync {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if sync {
		if d, err := os.Open(filepath.Dir(path)); err == nil {
			d.Sync()
			d.Close()
		}
	}
	return nil
}

func readSidecar(dir string, t TableID, k int) (sidecar, error) {
	var sc sidecar
	raw, err := os.ReadFile(matchPath(dir, t, k, ".json"))
	if err != nil {
		return sc, err
	}
	if err := json.Unmarshal(raw, &sc); err != nil {
		return sc, fmt.Errorf("host: %s/%d.json: %w", t, k, err)
	}
	return sc, nil
}

// readSidecars lists a table's matches from its sidecars, ascending by k.
func readSidecars(dir string, t TableID) ([]sidecar, error) {
	entries, err := os.ReadDir(tableDir(dir, t))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []sidecar
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		k, err := strconv.Atoi(name[:len(name)-len(".json")])
		if err != nil {
			continue
		}
		sc, err := readSidecar(dir, t, k)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Match < out[j].Match })
	return out, nil
}

// readLog rebuilds a match's events.Log from its files. A trailing partial
// line (a crash mid-write) is ignored; anything malformed before it is an
// error. The Seed comes from the sidecar. The two streams are reconciled to
// a consistent prefix (fix round 1) before returning, so Events/ViewAt of a
// match whose write was cut off never serve a log that cannot replay.
func readLog(dir string, t TableID, k int) (*events.Log, error) {
	sc, err := readSidecar(dir, t, k)
	if err != nil {
		return nil, err
	}
	l := events.NewLog(sc.Seed)
	if err := readLines(matchPath(dir, t, k, ".events"), func(b []byte) error {
		var e events.Event
		if err := json.Unmarshal(b, &e); err != nil {
			return err
		}
		l.Append(e)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := readLines(matchPath(dir, t, k, ".intents"), func(b []byte) error {
		var in decision.Intent
		if err := json.Unmarshal(b, &in); err != nil {
			return err
		}
		l.Intents = append(l.Intents, in)
		return nil
	}); err != nil {
		return nil, err
	}
	reconcileLog(l)
	return l, nil
}

// reconcileLog trims a log read from disk to the consistent prefix a replay
// can rebuild (fix round 1, burst atomicity). A burst's events and its
// owning intent live in two separate files and can never be committed
// atomically, so a crash or kill mid-burst can leave either stream ahead of
// the other: the events file can be cut inside its last burst (the
// DecisionAsk or GameOver that would complete it never written, so the tail
// is not a complete burst), and — because append writes the intent before
// its burst — the intents file can hold an orphan intent whose events never
// made it. boundsOf marks every complete burst; each non-genesis burst needs
// exactly one intent, so a log with C complete bursts is trustworthy only up
// to C-1 of its intents and the C bursts those account for, and neither
// stream may run past the other. Everything past that is trimmed, so
// Events and ViewAt always serve a prefix that replays cleanly.
//
// DECIDED, fb-20260915T094418Z: the trim stays on the file path, cutting a
// parked/finished burst's overshoot tail along with a crash-cut one — the
// two are indistinguishable here (reconcileLog has no Config to replay
// with, and a crash mid-APPEND leaves exactly the shape a legitimate tail
// has: complete-looking lines past the last ask). What the trim costs is
// re-admitted one consumer up, in matchForLog: the replay of the trimmed
// recording legitimately runs past its end on this shape, and the sidecar's
// Head — the chain hash over the full stream the match held at its terminal
// transition — proves the reconstructed tail byte for byte. The in-memory
// callers do NOT trim: SnapshotForFeedback skips the reconcile for any
// non-crashed match (under its read lock every event belongs to a
// completed burst, so the tail is real and verifiable), and the live
// match's own bookkeeping (m.bounds) has always recorded true burst ends.
func reconcileLog(l *events.Log) {
	bounds := boundsOf(l.Events)
	if len(bounds) == 0 {
		// No complete burst — not even the genesis burst (boundary 0) a
		// wiped match would still have. Nothing here can replay, so drop
		// both streams rather than serve events' untrustworthy tail.
		l.Events, l.Intents = nil, nil
		return
	}
	// C complete bursts want C-1 intents (genesis needs none). Clamp to
	// whatever the intents file actually has, so the events stream never
	// runs past what the intents can drive.
	need := len(bounds) - 1
	if n := len(l.Intents); n < need {
		need = n
	}
	// bounds[need] is one past burst `need`, the last burst still owned by
	// an intent we hold (for need > 0) or the genesis burst (for need == 0).
	// Everything before that boundary replays; everything after is the tail
	// a crash cut off and must not be served.
	l.Events = l.Events[:bounds[need]]
	if len(l.Intents) > need {
		l.Intents = l.Intents[:need]
	}
}

// readLines calls fn per complete, newline-terminated record. A file that
// ends with '\n' splits into a trailing empty element; one cut off
// mid-write ends with a partial record. Either way the final element is
// not a complete record and is dropped.
func readLines(path string, fn func([]byte) error) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := bytes.Split(raw, []byte{'\n'})
	for _, line := range lines[:len(lines)-1] {
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return fmt.Errorf("host: %s: %w", path, err)
		}
	}
	return nil
}

// tablesFile is tables.json.
type tablesFile struct {
	Tables []tableRecord `json:"tables"`
}

type tableRecord struct {
	Config TableConfig `json:"config"`
	Match  int         `json:"match"`
}

// save writes tables.json (sorted by id) when persistence is on. It takes
// the registry lock itself; callers that already hold r.mu use saveLocked.
// fsync coverage (fix round 1): the temp file is synced before the rename
// and the parent directory after it (writeFileAtomic), the same guarantees
// as the sidecar and the events files.
func (r *Registry) save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked()
}

// saveLocked is the body of save with r.mu already held (AddTable, run and
// archive call it that way); save is saveLocked plus the lock it takes
// itself. Persisting is no-op in memory mode.
func (r *Registry) saveLocked() error {
	if r.opts.Dir == "" {
		return nil
	}
	var tf tablesFile
	ids := make([]string, 0, len(r.tables))
	for id := range r.tables {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	for _, id := range ids {
		t := r.tables[TableID(id)]
		t.mu.RLock()
		tf.Tables = append(tf.Tables, tableRecord{Config: t.cfg, Match: t.k})
		t.mu.RUnlock()
	}
	if err := os.MkdirAll(r.opts.Dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(r.opts.Dir, "tables.json"), append(b, '\n'), r.opts.Sync)
}
