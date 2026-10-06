package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

// profiler is the -cpuprofile / -memprofile support: the two pprof capture
// points a bench run needs, wired so a profiled run produces byte-identical
// results to an unprofiled one (profiling changes scheduling, never engine
// outputs -- both are pure runtime observations and the bench's results are
// a pure function of its seed inputs).
//
// The CPU profile covers the whole run (corpus open through report write) at
// the runtime's 100 Hz per-thread default. The heap profile is written once,
// after every game has finished: a forced GC first, so the inuse_space sample
// is live heap only, not heap that died mid-run; pprof reads BOTH alloc_space
// (cumulative allocation over the whole run, the garbage-creation signal) and
// inuse_space (live heap at end) from this one file, so a single -memprofile flag serves both views.
// Sampling is the runtime's default 512 KiB rate, the same rate every
// -test.memprofile in this repo samples at, so magnitudes are comparable to
// those runs.
//
// Nothing here is on the default path: both paths empty means the calls are
// no-ops and the run is exactly the unprofiled bench.
type profiler struct {
	cpuPath string
	memPath string

	cpuFile *os.File
}

// start begins CPU profiling when -cpuprofile was given. It must be called
// before any game work; finish stops it again.
func (p *profiler) start() error {
	if p.cpuPath == "" {
		return nil
	}
	f, err := os.Create(p.cpuPath)
	if err != nil {
		return fmt.Errorf("cpuprofile: %w", err)
	}
	p.cpuFile = f
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		p.cpuFile = nil
		return fmt.Errorf("cpuprofile: %w", err)
	}
	return nil
}

// finish stops CPU profiling (if started) and writes the heap profile (if
// requested). It is idempotent and safe to defer over every exit path,
// including a run that failed mid-matrix: a partial profile of a failed run
// is still readable evidence. The error is returned so main can surface a
// write failure instead of silently dropping a profile the operator asked
// for.
func (p *profiler) finish() error {
	var first error
	if p.cpuFile != nil {
		pprof.StopCPUProfile()
		if err := p.cpuFile.Close(); err != nil && first == nil {
			first = fmt.Errorf("cpuprofile: %w", err)
		}
		p.cpuFile = nil
	}
	if p.memPath == "" {
		return first
	}
	f, err := os.Create(p.memPath)
	if err != nil {
		if first == nil {
			first = fmt.Errorf("memprofile: %w", err)
		}
		return first
	}
	// Force a full GC so the heap snapshot is live-set only: without it the
	// profile carries garbage from the last games still awaiting collection,
	// and inuse_space overstates what the run holds at rest.
	runtime.GC()
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil && first == nil {
		first = fmt.Errorf("memprofile: %w", err)
	}
	if err := f.Close(); err != nil && first == nil {
		first = fmt.Errorf("memprofile: %w", err)
	}
	return first
}
