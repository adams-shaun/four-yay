# gorge engine speed: docs/015 comparison (2026-10-07)

The docs/015 row set against the docs/015 pin, old main, and today's main,
plus the subset-corpus run and the ratio of today's main to old main.

| docs/015 row | a4af596 (docs/015 pin) | old main (e0496f062) | today's main | today, subset corpus | today vs old main |
|---|---|---|---|---|---|
| Random play FDN A, turns/s | 933 | 807 | 3,956 | 3,922 | 4.9x |
| Random play FDN B, turns/s | 1,722 | 1,266 | 3,653 | 3,867 | 2.9x |
| Random play Burn, turns/s | 3,093 | 2,389 | 6,224 | 6,754 | 2.6x |
| Built-in bot FDN A / B, games/s | 39.6 / 68.8 | 38.4 / 50.5 | 148.6 / 150.1 | 148.8 / 163.3 | 3.9x / 3.0x |
| Auto-pay bot FDN A / B, games/s | 31.5 / 45.0 | 30.9 / 32.7 | 83.0 / 92.2 | 104.8 / 101.3 | 2.7x / 2.8x |
| Copy a mid-game state, GC excluded | 14.3 µs | 12.0 µs | 10.7 µs | 7.8 µs | 1.1x |
| Same copies with GC paid | 50.3 µs | 51.3 µs | 53.9 µs | 24.4 µs | 0.95x |
| One search step (copy + submit) | 59.9 µs | 61.4 µs | 31.7 µs | 30.0 µs | 1.9x |
| Pass-priority step only | 39.8 µs | 42.1 µs | 25.9 µs | 26.9 µs | 1.6x |
| Old rollout sampler, rollouts/s | 40.0 | 28.3 | 94.3 | 103.3 | 3.3x |
| azmcts + honest redeal, 100 sims, sims/s | n/a | 336 | 1,099 | 1,294 | 3.3x |
| azmcts + honest redeal, 1,000 sims, sims/s | n/a | 203 | 954 | 1,012 | 4.7x |
