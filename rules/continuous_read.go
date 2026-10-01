package rules

// RegisteredContinuous is the engine's registered continuous-effect list --
// the registry AddContinuous writes (resolved pumps, Effect-API grants,
// control and copy effects); printed statics are not in it -- for a
// read-only scan. The slice and everything it references belong to the
// engine: a caller must not modify it, nor keep it past its next call into
// the engine.
func (e *Engine) RegisteredContinuous() []ContinuousEffect { return e.continuous }
