<script lang="ts">
  import type { DvrState, DvrAction } from '../lib/dvr';
  import { dvrReducer } from '../lib/dvr';
  import type { EventBody } from '../protocol';
  import type { AutoPassLog } from '../lib/autolog';
  import Transcript from './Transcript.svelte';

  /**
   * Transcript.scroll.fixture.svelte: the live-tail follower and the paused
   * cursor follower of the transcript's scroll behaviour
   * (fb-20260928T043757Z, "the log should follow its end"). The hover
   * fixture's mount is static — two lines, no scrollable ancestor — so it
   * cannot drive a scroll assertion. This fixture mounts the production
   * Transcript inside a bounded `overflow-y:auto` wrapper that mimics
   * Table.svelte's `.log` (the wrapper is the scroll container; Transcript's
   * own `.transcript` does not scroll), starts with 40 lines so the wrapper
   * overflows, and drives change through the REAL dvr reducer, mirroring the
   * seated client's shape (match.svelte.ts): one `head` dispatch per event
   * frame, then a bulk `backfill` of the rendered lines — the backfill case
   * appends rows WITHOUT touching `dvr.cursor`, which is exactly the shape
   * the old cursor-keyed effect missed. The default page is unchanged from
   * the mount state so no other test depends on a hook having run.
   *
   * Window hooks (the PileModal.fixture.svelte `?case=`/window-hook
   * convention):
   *   __appendLog(n)   — seat-shaped append of n events (head dispatches +
   *                      one backfill batch) through dvrReducer.
   *   __headOnly(n, hiddenTail) — only the `head` chits for the next n seqs;
   *                      the rendered lines are held back for
   *                      __backfillPending. With hiddenTail the LAST
   *                      announced event is an engine-noise kind
   *                      ('decision_ask', filtered out by default) — the
   *                      shape the seated path lands on at every decision
   *                      boundary, where the old cursor-keyed effect found
   *                      no [data-seq] row for the cursor and gave up.
   *   __backfillPending() — one `backfill` dispatch of the lines announced
   *                      by the last __headOnly; the cursor does not move.
   *   __appendNote()   — one client-local auto-pass note under the lines.
   *   __pauseAt(seq)   — pause the DVR with the cursor parked on `seq`.
   */
  const body = (seq: number, kind = 'put_on_stack'): EventBody => ({
    event: { seq, kind, player: 0 },
    line: `Fixture line ${seq}: Ann casts a spell`,
  });

  let dvr = $state<DvrState>({
    match: 'm1', head: 40, cursor: 40, live: true, turnStarts: [], gap: false,
    events: Array.from({ length: 40 }, (_, i) => body(i + 1)),
  });
  let notes = $state<AutoPassLog[]>([]);
  let nextNoteId = 1;

  type Hooks = {
    __appendLog: (n: number, hiddenTail?: boolean) => void;
    __headOnly: (n: number, hiddenTail?: boolean) => void;
    __backfillPending: () => void;
    __appendNote: () => void;
    __pauseAt: (seq: number) => void;
  };
  const w = window as unknown as Hooks;
  let pending: EventBody[] = [];

  w.__appendLog = (n, hiddenTail = false) => {
    w.__headOnly(n, hiddenTail);
    w.__backfillPending();
  };
  w.__headOnly = (n, hiddenTail = false) => {
    const first = dvr.head;
    pending = Array.from({ length: n }, (_, i) =>
      body(first + 1 + i, hiddenTail && i === n - 1 ? 'decision_ask' : 'put_on_stack'));
    for (const b of pending) dvr = dvrReducer(dvr, { type: 'head', seq: b.event.seq } as DvrAction);
  };
  w.__backfillPending = () => {
    const bodies = pending;
    pending = [];
    dvr = dvrReducer(dvr, { type: 'backfill', events: bodies });
  };
  w.__appendNote = () => {
    const id = nextNoteId++;
    notes = [...notes, { id, turn: 1, text: `Auto-passed: fixture note ${id}` }];
  };
  w.__pauseAt = (seq) => {
    dvr = { ...dvr, live: false, cursor: seq };
  };
</script>

<div class="log">
  <Transcript {dvr} {notes} onSeek={() => {}} />
</div>

<style>
  /* Mimics Table.svelte's .log: a bounded box whose overflow-y:auto makes IT
     the scroll container. Transcript's own .transcript is height:100% with
     no overflow, so the scroll assertions read this wrapper. */
  .log {
    height: 300px;
    overflow-y: auto;
    min-height: 0;
  }
</style>
