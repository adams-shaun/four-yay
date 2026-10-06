// Command keywordbench measures keyword presence separately from event-log use.
// It drives the same engine / view / seat.Bot path as mtgsim, with mirror matches
// so every named deck gets the same number of seat-games. No policy is changed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func main() {
	dir := flag.String("dir", ".cards", "compiled corpus and raw cardsfolder directory")
	names := flag.String("decks", "all", "comma-separated repo deck names, or all")
	games := flag.Int("games", 10, "mirror games PER DECK; 0 for presence/corpus only")
	seed := flag.Uint64("seed", 110000, "each deck uses seed+i, i in [0,games)")
	corpus := flag.Bool("corpus", false, "also rank raw K: lines and compiled keyword card reach")
	recompile := flag.Bool("recompile", false, "on any cache load failure, recompile cardsfolder IN MEMORY (never writes the cache; safe in a worktree sharing .cards)")
	flag.Parse()
	if err := run(*dir, *names, *games, *seed, *corpus, *recompile, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "keywordbench:", err)
		os.Exit(1)
	}
}

// loadRegistryForBench loads dir's fingerprint-keyed IR cache for measurement.
// The default path never recompiles: a benchmark must not silently substitute a
// freshly compiled corpus for the cache it claims to measure. When recompile is
// set and the cache load fails for ANY reason (missing, unreadable, version
// mismatch), it falls back to compiling cardsfolder in memory — exactly what
// cards.OpenCorpus does — and NEVER writes the cache: a benchmark must not
// mutate the corpus directory it measures.
func loadRegistryForBench(dir string, recompile bool) (*cards.Registry, bool, error) {
	path := cards.CachePath(dir)
	reg, err := cards.LoadRegistry(path)
	if err == nil {
		return reg, false, nil
	}
	if !recompile {
		var cve *cards.CacheVersionError
		if errors.As(err, &cve) {
			printSharedCacheAdvice(os.Stderr, err, path)
		}
		return nil, false, err
	}
	fmt.Fprintf(os.Stderr, "keywordbench: IR cache unusable (%v); recompiling cardsfolder in memory — nothing is written to %s\n", err, path)
	r, _, cerr := cards.CompileDir(cards.CorpusDir(dir))
	if cerr != nil {
		return nil, false, cerr
	}
	return r, true, nil
}

// printSharedCacheAdvice explains, for a *cards.CacheVersionError, that the
// cache file is keyed to this binary's compiler fingerprint (cards.CachePath),
// so `make compile-cards` run from a different build writes a DIFFERENT file
// and will not repair this one. It names the two safe remedies. Non-version
// errors print nothing: their plain failure is self-explanatory.
func printSharedCacheAdvice(w io.Writer, err error, path string) {
	var cve *cards.CacheVersionError
	if !errors.As(err, &cve) {
		return
	}
	fmt.Fprintf(w, "keywordbench: %v\n", err)
	fmt.Fprintf(w, "  the cache at %s is keyed to THIS build's compiler fingerprint — `make compile-cards` from a different build writes a different file and will not repair it.\n", path)
	fmt.Fprintln(w, "  run `make compile-cards` from this source tree, or rerun with -recompile to measure against an in-memory rebuild of cardsfolder (nothing is written).")
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// saMetrics deliberately attributes ONLY the actual ability, never all keywords
// on its source card. Regeneration is an API, not a Forge K: keyword.
func saMetrics(sa *cards.SA) []string {
	if sa == nil {
		return nil
	}
	var out []string
	if k := sa.Params["Keyword"]; k != "" {
		out = append(out, "kw:"+k)
	}
	for s := sa; s != nil; s = s.Sub {
		if s.API == "Regenerate" {
			out = append(out, "api:Regenerate")
			break
		}
	}
	return out
}

func cardMetrics(c *cards.Card) []string {
	m := map[string]bool{}
	for _, f := range c.Faces {
		for _, k := range f.Keywords {
			m["kw:"+cards.KeywordHead(k)] = true
		}
		for _, sa := range f.Abilities {
			for _, k := range saMetrics(sa) {
				m[k] = true
			}
		}
		for _, t := range f.Triggers {
			for _, k := range saMetrics(t.Effect) {
				m[k] = true
			}
		}
	}
	return keys(m)
}

// Raw = literal K: lines, including repeat lines and alternate faces, from
// GNU /usr/bin/grep, NOT the ignore-aware shell grep. Compiled = keyword
// occurrences across faces, and distinct registry cards bearing the keyword.
// Neither population includes dynamically granted keywords in SVar bodies.
func corpusReport(w io.Writer, dir string, reg *cards.Registry) error {
	rawBytes, err := exec.Command("/usr/bin/grep", "-rhI", "^K:", filepath.Join(dir, "cardsfolder")).Output()
	if err != nil {
		return fmt.Errorf("raw keyword scan: %w", err)
	}
	raw, occurrences, reach := map[string]int{}, map[string]int{}, map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(rawBytes)), "\n") {
		raw[cards.KeywordHead(strings.TrimSpace(strings.TrimPrefix(line, "K:")))]++
	}
	for _, c := range reg.Cards {
		seen := map[string]bool{}
		for _, f := range c.Faces {
			for _, line := range f.Keywords {
				k := cards.KeywordHead(line)
				occurrences[k]++
				seen[k] = true
			}
		}
		for k := range seen {
			reach[k]++
		}
	}
	all := map[string]bool{}
	for k := range raw {
		all[k] = true
	}
	for k := range reach {
		all[k] = true
	}
	order := keys(all)
	supported := effects.Supported()
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		// Reach times registered implementation (0/1); unimplemented follow,
		// themselves ordered by reach. Registration is NOT semantic completeness.
		as, bs := 0, 0
		if supported["kw:"+a] {
			as = reach[a]
		}
		if supported["kw:"+b] {
			bs = reach[b]
		}
		if as != bs {
			return as > bs
		}
		if reach[a] != reach[b] {
			return reach[a] > reach[b]
		}
		return a < b
	})
	fmt.Fprintf(w, "# corpus: %d compiled cards; raw binary=/usr/bin/grep -rhI '^K:' %s/cardsfolder; tokens excluded\n", len(reg.Cards), dir)
	fmt.Fprintln(w, "keyword\tregistered\traw_K_lines\tcompiled_K_occurrences\tcompiled_cards")
	for _, k := range order {
		fmt.Fprintf(w, "%s\t%t\t%d\t%d\t%d\n", k, supported["kw:"+k], raw[k], occurrences[k], reach[k])
	}
	return nil
}

type count struct {
	cards                                                      map[string]bool
	battlefield, offered, selected, activated, triggered, cast int
	// Observed contexts of UNSELECTED ability options; overlapping, not a
	// causal classification. No policy score is inferred from the log.
	outsideMain, attached, noPrintedCreature int
	alternatives                             map[string]int
}
type tally map[string]*count

func (t tally) at(k string) *count {
	if t[k] == nil {
		t[k] = &count{cards: map[string]bool{}, alternatives: map[string]int{}}
	}
	return t[k]
}
func (t tally) presence(deck []*cards.Card) {
	for _, c := range deck {
		for _, k := range cardMetrics(c) {
			t.at(k).cards[c.Faces[0].Name] = true
		}
	}
}

func abilityMetrics(g *state.Game, id state.ObjID, index int) []string {
	o := g.Obj(id)
	if o == nil || o.Face() == nil || index < 0 || index >= len(o.Face().Abilities) {
		return nil
	}
	return saMetrics(o.Face().Abilities[index])
}
func (t tally) choice(g *state.Game, d *decision.Decision, in decision.Intent) {
	for _, o := range d.Options {
		var metrics []string
		if o.Kind == "ability" {
			metrics = abilityMetrics(g, o.Obj, o.Ability)
		}
		if o.Kind == "cast" {
			if k := castMetric(o.Mode); k != "" {
				metrics = []string{k}
			}
		}
		picked := false
		for _, i := range in.Choices {
			if i == o.Index {
				picked = true
			}
		}
		for _, k := range metrics {
			c := t.at(k)
			c.offered++
			if picked {
				c.selected++
			} else if o.Kind == "ability" {
				hasCreature := false
				for _, id := range g.Zone(state.ZBattlefield, d.Player) {
					obj := g.Obj(id)
					if obj != nil && obj.Face() != nil && obj.Face().IsCreature() {
						hasCreature = true
						break
					}
				}
				if !hasCreature {
					c.noPrintedCreature++
				}
				for _, chosen := range d.Options {
					for _, i := range in.Choices {
						if chosen.Index == i {
							c.alternatives[chosen.Kind]++
						}
					}
				}
				if !g.Step.IsMain() {
					c.outsideMain++
				}
				if obj := g.Obj(o.Obj); obj != nil && obj.AttachedTo != 0 {
					c.attached++
				}
			}
		}
	}
}
func castMetric(mode string) string {
	switch mode {
	case "kicked":
		return "kw:Kicker"
	case "surged":
		return "kw:Surge"
	case "flashback":
		return "kw:Flashback"
	case "miracle":
		return "kw:Miracle"
	}
	return ""
}

// consume folds the actual log onto a separate state, so face/ability indices
// are resolved at event time, NOT against a card's possibly transformed final
// face. All mutation of the mirror is through events.Apply. Presence on the
// battlefield counts unique object+keyword pairs per game, not priority ticks.
func (t tally) consume(g *state.Game, log []events.Event, seen map[string]bool) {
	for _, ev := range log {
		switch ev.Kind {
		case events.AbilityPush:
			for _, k := range abilityMetrics(g, ev.Obj, int(ev.Amount)) {
				t.at(k).activated++
			}
		case events.TriggerPush:
			o := g.Obj(ev.Obj)
			if o != nil && o.Face() != nil && ev.Amount >= 0 && int(ev.Amount) < len(o.Face().Triggers) {
				tr := o.Face().Triggers[ev.Amount]
				metrics := map[string]bool{}
				if k := tr.Params["Keyword"]; k != "" {
					metrics["kw:"+k] = true
				}
				for _, k := range saMetrics(tr.Effect) {
					metrics[k] = true
				}
				for _, k := range keys(metrics) {
					t.at(k).triggered++
				}
			}
		case events.CastInfo:
			for _, mode := range strings.Split(ev.Counter, ",") {
				if k := castMetric(mode); k != "" {
					t.at(k).cast++
				}
			}
		}
		events.Apply(g, ev)
		// Includes transformed faces, but not continuously granted keywords:
		// these are printed/expanded IR facts, not a Derived() forecast.
		if ev.Kind == events.MoveZone || ev.Kind == events.FlipFace {
			o := g.Obj(ev.Obj)
			if o != nil && !o.IsToken && o.Zone == state.ZBattlefield && o.Face() != nil {
				c := &cards.Card{Faces: []*cards.Face{o.Face()}}
				for _, k := range cardMetrics(c) {
					key := fmt.Sprintf("%d/%s", ev.Obj, k)
					if !seen[key] {
						seen[key] = true
						t.at(k).battlefield++
					}
				}
			}
		}
	}
}

func play(cfg rules.Config, t tally) (string, error) {
	e := rules.New(cfg)
	mirror := e.G.Clone()
	offset := len(e.L.Events) // New has already applied genesis/deal to the clone.
	seen := map[string]bool{}
	b := seat.NewBot(cfg.Seed)
	e.Advance()
	intents := 0
	for {
		t.consume(mirror, e.L.Events[offset:], seen)
		offset = len(e.L.Events)
		if e.G.Over {
			break
		}
		if e.Pending() == nil || intents >= 40000 || e.G.Turn >= 200 {
			return "stalled", nil
		}
		d := e.Pending()
		v := view.Project(e.G, e, d.Player, d)
		v.Round = view.RoundOf(e.G, e.L.Events)
		in, err := b.Decide(context.Background(), v, *d)
		if err != nil {
			return "", err
		}
		t.choice(e.G, d, in)
		if err := e.Submit(in); err != nil {
			return "", err
		}
		intents++
	}
	if _, err := replay.Replay(e.L, cfg); err != nil {
		return "", err
	}
	return "replay_OK", nil
}

func printTally(w io.Writer, name string, t tally) {
	for _, k := range keys(t) {
		c := t[k]
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", name, k, len(c.cards), c.battlefield, c.offered, c.selected, c.activated, c.triggered, c.cast)
		if len(c.alternatives) > 0 {
			var alternatives []string
			for _, kind := range keys(c.alternatives) {
				alternatives = append(alternatives, fmt.Sprintf("%s:%d", kind, c.alternatives[kind]))
			}
			fmt.Fprintf(w, "# unselected deck=%s metric=%s outside_main=%d source_attached=%d no_printed_creature=%d chose=%s (overlapping contexts, not causes)\n", name, k, c.outsideMain, c.attached, c.noPrintedCreature, strings.Join(alternatives, ","))
		}
	}
}
func merge(dst, src tally) {
	for _, k := range keys(src) {
		a, b := dst.at(k), src[k]
		for name := range b.cards {
			a.cards[name] = true
		}
		a.battlefield += b.battlefield
		a.offered += b.offered
		a.selected += b.selected
		a.activated += b.activated
		a.triggered += b.triggered
		a.cast += b.cast
		a.outsideMain += b.outsideMain
		a.attached += b.attached
		a.noPrintedCreature += b.noPrintedCreature
		for _, kind := range keys(b.alternatives) {
			a.alternatives[kind] += b.alternatives[kind]
		}
	}
}
func run(dir, spec string, games int, seed uint64, corpus bool, recompile bool, w io.Writer) error {
	if games < 0 {
		return fmt.Errorf("negative -games")
	}
	// Load the named cache directly: measurement must not silently recompile it.
	// -recompile opts into an in-memory rebuild on any load failure.
	reg, _, err := loadRegistryForBench(dir, recompile)
	if err != nil {
		return err
	}
	if corpus {
		if err := corpusReport(w, dir, reg); err != nil {
			return err
		}
	}
	names := testutil.RepoDeckNames()
	if spec != "all" {
		names = strings.Split(spec, ",")
	}
	total := tally{}
	stalled := 0
	for _, k := range keys(effects.Supported()) {
		if strings.HasPrefix(k, "kw:") {
			total.at(k)
		}
	}
	total.at("api:Regenerate")
	fmt.Fprintf(w, "# games_per_deck=%d seeds=%d+i mirror_seats=2 bot=seat.NewBot mulligans=0 max_turns=200 max_intents=40000; commander files use commander/40 life; other files constructed/20 life\n", games, seed)
	fmt.Fprintln(w, "# activated=AbilityPush; triggered=TriggerPush; special_cast=CastInfo flags. Zeros for static/cost keywords without these encodings mean NOT MEASURED, not inert. Pushes do not prove resolution or benefit. Offered/selected are option occurrences, not unique opportunities. Battlefield excludes tokens and granted keywords.")
	fmt.Fprintln(w, "deck\tmetric\tdistinct_deck_cards\tbattlefield_objects\toffered\tselected\tactivated\ttriggered\tspecial_cast")
	for _, name := range names {
		name = strings.TrimSpace(name)
		deck, err := testutil.LoadRepoDeck(reg, name)
		if err != nil {
			return err
		}
		for _, c := range deck {
			if m := reg.Unsupported(c, effects.Supported()); len(m) > 0 {
				return fmt.Errorf("%s: %s unsupported %v", name, c.Faces[0].Name, m)
			}
		}
		f, err := testutil.LoadRepoDeckFile(name)
		if err != nil {
			return err
		}
		cfg := rules.Config{Names: []string{name, name}, Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens}
		if len(f.CommanderNames()) > 0 {
			if err := f.ValidateCommander(reg); err != nil {
				return err
			}
			idxs := f.CommanderIndices()
			cfg.Format = rules.FormatCommander
			cfg.StartingLife = 40
			cfg.Commanders = [][]int{idxs, append([]int(nil), idxs...)}
		}
		t := tally{}
		t.presence(deck)
		for i := 0; i < games; i++ {
			cfg.Seed = seed + uint64(i)
			status, err := play(cfg, t)
			if err != nil {
				return fmt.Errorf("%s seed %d: %w", name, cfg.Seed, err)
			}
			fmt.Fprintf(w, "# game deck=%s seed=%d %s\n", name, cfg.Seed, status)
			if status == "stalled" {
				stalled++
			}
		}
		printTally(w, name, t)
		merge(total, t)
	}
	printTally(w, "TOTAL", total)
	if stalled > 0 {
		return fmt.Errorf("%d games stalled; tallies include their PARTIAL logs", stalled)
	}
	return nil
}
