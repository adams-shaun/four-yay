package rules

// probe runs f under the resolution kernel (resolve.Kernel.Probe): the test
// helper for fixtures that drive an engine's internals (resolveTop, an entry)
// instead of Submitting an intent. A tape ask inside f poses its decision; the
// answering Submit re-executes f from the checkpoint with the answers served.
func (e *Engine) probe(f func()) { e.tape.Probe(asResolve(e), f) }

func init() { testProbeResolveTop = true }
