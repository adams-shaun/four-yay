package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof on http.DefaultServeMux
	"os"
)

// pprofAddr serves the live runtime profiles (heap, allocs, goroutine, CPU)
// while a run is in flight: -memprofile only snapshots the heap after the last
// game, which misses the peak. Bind a task-agent port (8090-8099), never the
// demo's 8080-8081.
var pprofAddr = flag.String("pprof-addr", "", "serve net/http/pprof on this address during the run (e.g. 127.0.0.1:8093; empty = off)")

func startPprof() {
	if *pprofAddr == "" {
		return
	}
	go func() {
		if err := http.ListenAndServe(*pprofAddr, nil); err != nil {
			fmt.Fprintln(os.Stderr, "botbench: pprof:", err)
		}
	}()
}
