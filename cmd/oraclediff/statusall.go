package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/adopt"
)

// runStatusAll runs the gate over every committed set (section 11.3 C8)
// and prints the format roll-ups ("Standard:A" once every set is
// declared) with their reason buckets. -out writes the generated dashboard
// (never committed); -write-ratchet records compliance/ratchet.json as the
// sets stand; -json prints one SetStatus per line.
//
// The gate runs in child processes of at most adopt.ChildMaxCards cards,
// procs at a time, because a gate run's heap cannot shrink in-process
// (adopt.ChildMaxCards says why). -sets limits the run to those sets;
// -child runs them in this process, which is how a child is invoked.
func runStatusAll(dir, level, out string, writeRatchet, asJSON bool, only []string, memprof string, procs int, child bool) error {
	if os.Getenv("GOMEMLIMIT") == "" {
		// A gate batch is allocation-heavy (an engine per scenario); a
		// soft limit keeps the collector ahead of it.
		debug.SetMemoryLimit(768 << 20)
	}
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	cs, err := adopt.Build(reg, ".")
	if err != nil {
		return err
	}
	codes := cs.SetCodes()
	if len(only) > 0 {
		codes = only
	}
	var sets []adopt.SetStatus
	if child {
		sets, err = cs.Status(reg, ".", level, codes...)
	} else {
		sets, err = cs.StatusChunked(codes, procs, func(batch []string) ([]adopt.SetStatus, error) {
			return statusChild(dir, level, batch)
		})
	}
	if err != nil {
		return err
	}
	if memprof != "" {
		f, err := os.Create(memprof)
		if err != nil {
			return err
		}
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			return err
		}
		f.Close()
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		for _, s := range sets {
			if err := enc.Encode(s); err != nil {
				return err
			}
		}
		if child {
			return nil
		}
	}
	for _, r := range cs.Rollups(sets) {
		if asJSON {
			break
		}
		var bs []string
		for _, b := range adopt.Buckets {
			if n := r.Buckets[b]; n > 0 {
				bs = append(bs, fmt.Sprintf("%s %d", b, n))
			}
		}
		fmt.Printf("%s\n    %s\n", r.Label, strings.Join(bs, ", "))
	}
	if out != "" {
		var b bytes.Buffer
		cs.WriteDashboard(&b, sets, gitHead())
		if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", out)
	}
	if child {
		return nil
	}
	switch level {
	case "A":
		return finishRatchetA(sets, only, writeRatchet, cs)
	case "B":
		return finishRatchetB(sets, writeRatchet, cs)
	}
	return nil
}

// finishRatchetA writes or checks the level-A ratchet. A write builds fresh
// level-A entries for the measured sets, carries every entry's existing
// level-B floor forward, and keeps unmeasured sets' entries when only is a
// slice.
func finishRatchetA(sets []adopt.SetStatus, only []string, writeRatchet bool, cs *adopt.Census) error {
	if writeRatchet {
		old, err := loadRatchetMaybe()
		if err != nil {
			return err
		}
		rec := adopt.RatchetOf(sets, old)
		if len(only) > 0 {
			// A slice updates its own sets' entries and keeps the rest.
			for k, e := range rec {
				if old == nil {
					old = map[string]adopt.RatchetEntry{}
				}
				old[k] = e
			}
			rec = old
		}
		return writeRatchetFile(rec)
	}
	r, err := adopt.LoadRatchet(".")
	if err != nil {
		return err
	}
	d, err := compliance.LoadDeclared("compliance/declared.json")
	if err != nil {
		return err
	}
	fails, slack := adopt.CheckRatchet(cs.SetCodes(), sets, d, r)
	if len(slack) > 0 {
		fmt.Printf("ratchet: %d sets improved past %s (-write-ratchet records them): %s\n", len(slack), adopt.RatchetFile, strings.Join(slack, "; "))
	}
	for _, f := range fails {
		fmt.Println("RATCHET FAIL:", f)
	}
	if len(fails) > 0 {
		return fmt.Errorf("%d certification ratchet failures", len(fails))
	}
	return nil
}

// finishRatchetB writes or checks the level-B ratchet. A write updates only
// the measured sets' outstanding_b floors and keeps every other field; a
// check holds the measured sets to those floors, and a set with no floor is
// outside the B claim and passes.
func finishRatchetB(sets []adopt.SetStatus, writeRatchet bool, cs *adopt.Census) error {
	if writeRatchet {
		old, err := loadRatchetMaybe()
		if err != nil {
			return err
		}
		return writeRatchetFile(adopt.RatchetOfB(sets, old))
	}
	r, err := adopt.LoadRatchet(".")
	if err != nil {
		return err
	}
	fails, slack := adopt.CheckRatchetB(sets, r)
	if len(slack) > 0 {
		fmt.Printf("ratchet: %d sets improved past the level-B floor (-write-ratchet records them): %s\n", len(slack), strings.Join(slack, "; "))
	}
	for _, f := range fails {
		fmt.Println("RATCHET FAIL:", f)
	}
	if len(fails) > 0 {
		return fmt.Errorf("%d level-B ratchet failures", len(fails))
	}
	return nil
}

// loadRatchetMaybe reads the committed ratchet, treating a missing file as
// an empty one (the first write creates it).
func loadRatchetMaybe() (map[string]adopt.RatchetEntry, error) {
	old, err := adopt.LoadRatchet(".")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return old, nil
}

// writeRatchetFile marshals and writes compliance/ratchet.json.
func writeRatchetFile(rec map[string]adopt.RatchetEntry) error {
	if err := os.WriteFile(adopt.RatchetFile, adopt.MarshalRatchet(rec), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", adopt.RatchetFile)
	return nil
}

// statusChild runs one batch of sets in a fresh oraclediff process.
func statusChild(dir, level string, batch []string) ([]adopt.SetStatus, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, "status", "-all", "-child", "-json", "-cards", dir, "-level", level, "-sets", strings.Join(batch, ","))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outb, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var sets []adopt.SetStatus
	sc := bufio.NewScanner(bytes.NewReader(outb))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var s adopt.SetStatus
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			return nil, err
		}
		sets = append(sets, s)
	}
	return sets, sc.Err()
}

func gitHead() string {
	b, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "an unknown head"
	}
	return strings.TrimSpace(string(b))
}
