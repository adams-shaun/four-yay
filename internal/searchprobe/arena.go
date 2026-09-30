package searchprobe

// slab hands out exactly-sized, capacity-limited subslices of large chunks,
// so many small owned slices cost one allocation per chunk instead of one
// each. A chunk is never grown or moved -- a full one is left to the slices
// already cut from it and a new one is started -- so every slice (and every
// pointer into one) stays valid for as long as it is referenced.
//
// A recording slab is append-only: what it hands out is owned by the frames
// that hold it. A scratch slab is reset between uses and hands out storage
// valid only until then.
type slab[T any] struct {
	buf []T
}

// slabMin and slabMax bound a new chunk's length (in elements): chunks
// double from slabMin, and a request larger than slabMax gets a chunk of
// exactly its own size.
const slabMin, slabMax = 64, 4096

// take returns n zeroed elements (nil for n == 0, like an append from nil).
func (s *slab[T]) take(n int) []T {
	if n == 0 {
		return nil
	}
	if cap(s.buf)-len(s.buf) < n {
		size := min(max(2*cap(s.buf), slabMin), slabMax)
		s.buf = make([]T, 0, max(size, n))
	}
	lo := len(s.buf)
	s.buf = s.buf[:lo+n]
	return s.buf[lo : lo+n : lo+n]
}

// reset makes a scratch slab's current chunk reusable from its start. The
// chunk is cleared, so it holds no references past its use.
func (s *slab[T]) reset() {
	clear(s.buf)
	s.buf = s.buf[:0]
}

// frameArena is the storage one kind of capture writes a frame into.
type frameArena struct {
	identities slab[Identity]
	events     slab[ObservedEvent]
	refs       slab[uint32]
	pairs      slab[[2]uint32]
	decisions  slab[ObservedDecision]
	options    slab[ObservedOption]
	players    slab[BoardPlayer]
	cards      slab[BoardCard]
	stack      slab[BoardStack]
	links      slab[historyLink]
}

func (a *frameArena) reset() {
	a.identities.reset()
	a.events.reset()
	a.refs.reset()
	a.pairs.reset()
	a.decisions.reset()
	a.options.reset()
	a.players.reset()
	a.cards.reset()
	a.stack.reset()
	a.links.reset()
}
