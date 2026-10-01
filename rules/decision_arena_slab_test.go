package rules

import "testing"

// reset clears exactly the slots take handed out -- every earlier chunk and
// the current chunk's used prefix -- so every slot is zero when handed out
// again, across chunk boundaries and a partial last chunk.
func TestSlabResetClearsEveryHandedSlot(t *testing.T) {
	var s slab[int]
	const chunk = 8
	for round := 0; round < 3; round++ {
		var got [][]int
		for _, n := range []int{3, 4, 2, 8, 1, 5} {
			b := s.take(n, chunk)
			for i := range b {
				if b[i] != 0 {
					t.Fatalf("round %d: take(%d) handed out a dirty slot", round, n)
				}
				b[i] = round + 1
			}
			got = append(got, b)
		}
		s.reset()
		for _, c := range s.chunks {
			for i, v := range c {
				if v != 0 {
					t.Fatalf("round %d: slot %d not cleared by reset", round, i)
				}
			}
		}
	}
}
