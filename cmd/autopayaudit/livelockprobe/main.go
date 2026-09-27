//go:build autopayaudit

// Command livelockprobe replays one bench game and, if it aborts, prints the
// looping object's identity and the event shape around the loop (audit aid).
package main

import (
	"flag"
	"fmt"
	"os"

	"runtime/pprof"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

func main() {
	a := flag.String("a", "pro-shaper", "seat 0 deck")
	b := flag.String("b", "ulalek-eldrazi", "seat 1 deck")
	seed := flag.Uint64("seed", 5109280, "game seed")
	auto0 := flag.Bool("auto0", true, "seat 0 auto-pay")
	auto1 := flag.Bool("auto1", false, "seat 1 auto-pay")
	obj := flag.Int("obj", 50585, "object to describe")
	variant1 := flag.String("variant1", "", "seat 1 plays seat.NewAutoPayVariantBot(seed, variant) (overrides -auto1)")
	constructed := flag.Bool("constructed", false, "constructed format (default commander)")
	every := flag.Int("every", 0, "print turn/objects/events every N decisions (0 = off)")
	cpuprof := flag.String("cpuprofile", "", "write a CPU profile of the game")
	maxDec := flag.Int("max-decisions", 0, "stop the game (as a guard stall) after this many decisions; 0 = off")
	from := flag.Int("from", 83370, "first event index to print")
	n := flag.Int("n", 40, "events to print")
	flag.Parse()
	reg, err := testutil.OpenCorpusRegistry(".cards")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	var decks [][]*cards.Card
	var cmdrs [][]int
	for _, name := range []string{*a, *b} {
		d, err := testutil.LoadRepoDeck(reg, name)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		f, _ := testutil.LoadRepoDeckFile(name)
		decks = append(decks, d)
		cmdrs = append(cmdrs, f.CommanderIndices())
	}
	cfg := rules.Config{Seed: *seed, Names: []string{*a, *b}, Decks: decks, Format: rules.FormatCommander, StartingLife: 40, Commanders: cmdrs}
	if *constructed {
		cfg = rules.Config{Seed: *seed, Names: []string{*a, *b}, Decks: decks}
	}
	cfg.Tokens = reg.Tokens
	cfg.NameUniverse = reg.Cards
	mk := func(auto bool, s uint64) seat.Seat {
		if auto {
			return seat.NewBot(s).EnableAutoPayMana()
		}
		return seat.NewBot(s)
	}
	seats := []seat.Seat{mk(*auto0, *seed^1), mk(*auto1, *seed^2)}
	if *variant1 != "" {
		seats[1] = seat.NewAutoPayVariantBot(*seed^2, *variant1)
	}
	if *cpuprof != "" {
		f, err := os.Create(*cpuprof)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
	}
	dn := 0
	hooks := bench.Hooks{}
	if *every > 0 || *maxDec > 0 {
		hooks.Guard = func(e *rules.Engine) (string, string) {
			dn++
			if *maxDec > 0 && dn >= *maxDec {
				return "max-decisions", ""
			}
			if *every <= 0 {
				return "", ""
			}
			if dn%*every == 0 {
				fmt.Printf("decision %d: turn %d step %v active %d objs %d events %d life %d/%d hand %d/%d bf %d/%d\n", dn, e.G.Turn, e.G.Step, e.G.Active, len(e.G.Objs), len(e.L.Events),
					e.G.Players[0].Life, e.G.Players[1].Life, len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZHand, 1)), len(e.G.Zone(state.ZBattlefield, 0)), len(e.G.Zone(state.ZBattlefield, 1)))
			}
			return "", ""
		}
	}
	o, e, err := bench.PlayGame(cfg, seats, 200, 20000, hooks)
	fmt.Printf("outcome %+v err %v events %d\n", o.StallOn, err, len(e.L.Events))
	if o.Livelock != "" {
		fmt.Println(o.Livelock[:min(len(o.Livelock), 400)])
	}
	if ob := e.G.Obj(stateID(*obj)); ob != nil {
		name := "?"
		if ob.Face() != nil {
			name = ob.Face().Name
		}
		fmt.Printf("object %d: %s owner %d zone %v card-token=%v\n", *obj, name, ob.Owner, ob.Zone, ob.Card == nil)
	}
	for i := *from; i < *from+*n && i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		nm := ""
		if ob := e.G.Obj(ev.Obj); ob != nil && ob.Face() != nil {
			nm = ob.Face().Name
		}
		fmt.Printf("%6d %-14s p%d obj %d (%s) from %v to %v amt %d text %q\n", i, ev.Kind, ev.Player, ev.Obj, nm, ev.From, ev.To, ev.Amount, ev.Text)
	}
}

func stateID(i int) state.ObjID { return state.ObjID(i) }
