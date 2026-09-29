import type { DecisionBody, Frame, MatchStart, SeatInfo, Snapshot, View, EventBody, TableHaltedBody } from '../protocol';
import { dvrReducer, initialDvr, type DvrAction, type DvrState } from './dvr';
import { ApiError, fetchEvents, fetchMatches, fetchView } from './api';
import { withBase } from './basepath';
import { ViewCache } from './viewcache';
import { turnStartsFrom } from './turns';
import type { SeatCtx } from './seat';
import { clientBreadcrumbs } from './breadcrumbs';
import { ModelBus, stepTransitions } from './clientmodel/gorge';
import type { ClientModelSource, ModelListener, PendingDecision } from './clientmodel/types';

/** frameSeq reads the seq an event/decision frame was addressed at, for the seated path's seq chit. */
function frameSeq(f: Frame): number | null {
  const b = f.body as EventBody;
  return b?.event?.seq ?? null;
}

/** MatchState is everything the focused view renders for one table. A seat context (M2e-4) is additive: when present, views and events are fetched seat-scoped (ViewAtSeat/EventsSeat) so the rendered board and log are the seat's own redacted truth, and the spectator-frame bodies are never rendered; when absent, every fetch and render is byte-identical to the spectator path. */
export class MatchState implements ClientModelSource {
  match = $state<number | null>(null);
  view = $state<View | null>(null);
  /** Sequence of the view actually assigned to the rendered board. */
  renderedSeq = $state<number | null>(null);
  seats = $state<SeatInfo[]>([]);
  dvr = $state<DvrState>(initialDvr);
  decision = $state<DecisionBody | null>(null);
  halted = $state<string | null>(null);
  loadError = $state<string | null>(null);
  private inflight = false;
  private again = false;
  // Invalidates live view/transcript requests started before a match change
  // or rewind; those responses describe a seq space the client discarded.
  private liveEpoch = 0;
  private cache = new ViewCache((seq) => this.fetchViewAt(seq));
  private seeking = 0;
  // seatSince is the last seq the seated path backfilled redacted transcript
  // lines up to; the next decision boundary fetches from here, never 0.
  private seatSince = 0;

  // The client-model bus (sub-project 0): MatchState is the gorge adapter,
  // and every view assignment tells listeners what changed, BEFORE the new
  // view is assigned, so a motion listener can still measure the old DOM.
  private model = new ModelBus();

  constructor(readonly table: string, readonly seat?: SeatCtx) {}

  /** ClientModelSource: the viewer's pending decision as the rendered view carries it. */
  get clientDecision(): PendingDecision | null {
    return this.view?.decision ?? null;
  }

  /** ClientModelSource: subscribe to steps/resets; returns the unsubscribe. */
  onModel(fn: ModelListener): () => void {
    return this.model.on(fn);
  }

  /**
   * apply consumes one stream frame. It reports whether the frame moved this
   * same match backwards, so the route can also reset the seat's local
   * decision/post state before it adopts the restored view.
   *
   * A reconnect normally begins with a `snapshot`, not a `rewind` frame. A
   * successful Undo whose rewind frame was lost therefore arrives as a
   * shorter same-match snapshot. Treating it as an ordinary snapshot left
   * SeatPanelState in the discarded sequence space, where it rejected the
   * restored lower-sequence decision forever.
   */
  apply(f: Frame): boolean {
    // An overflow frame is the server saying it DROPPED frames for this
    // session: Session.push never blocks, so a burst that outruns the SSE
    // writer discards frames, marks the session overflowed and closes it
    // (host/session.go, host/httpapi/sse.go). The dropped frames can include
    // the decision frame that is this client's ONLY repaint edge during play,
    // so ignoring this — as the client did — leaves the board frozen on the
    // last painted view until the player reloads. It carries no table (it
    // describes the session, not a match), so it is handled before the table
    // guard below and refetches rather than trusting what we hold.
    if (f.t === 'overflow') {
      this.resync();
      return false;
    }
    if (f.table !== this.table) return false;
    switch (f.t) {
      case 'match_start':
        this.liveEpoch++;
        this.seeking++;
        this.match = f.match ?? null;
        this.seats = (f.body as MatchStart).seats;
        this.setRenderedView(null, null); // the previous match's board; wait for this one's snapshot before showing anything
        this.decision = null;
        this.halted = null;
        this.seatSince = 0;
        break;
      case 'snapshot': {
        const s = f.body as Snapshot;
        const rewound = this.match === (f.match ?? this.match) && s.head < this.dvr.head;
        if (rewound) {
          // This is the reconnect spelling of TRewind. Keep this block in
          // lockstep with the explicit case below: both discard stale reads,
          // decisions and transcript tails before fetching the restored seat
          // projection.
          this.liveEpoch++;
          this.seeking++;
          this.match = f.match ?? this.match;
          this.decision = null;
          this.halted = null;
          this.seats = s.seats;
          this.seatSince = 0;
          this.dispatch({ type: 'rewind', match: `${this.table}/${this.match}`, head: s.head, turnStarts: s.turn_starts });
          if (this.seat) {
            this.withdrawPaintedDecision();
            void this.refreshLive();
            void this.backfillEvents(0);
          } else {
            this.setRenderedView(s.view, s.head);
          }
          return true;
        }
        this.match = f.match ?? this.match;
        // ui16: the snapshot carries the match's seat list, so a subscriber
        // gets it however it joined — cold load, refresh, or a mid-game focus
        // subscribe that never saw the match_start frame. Re-seeding here
        // (not once at mount from tables.list, which is async) is what makes
        // the table route's seats correct for the whole match.
        this.seats = s.seats;
        this.dispatch({ type: 'snapshot', match: `${this.table}/${this.match}`, head: s.head, turnStarts: s.turn_starts });
        if (this.dvr.live) {
          if (this.seat) {
            // The pushed snapshot is the table's spectator view (redacted
            // for the spectator visibility, which for a fixture table is
            // omniscient — a god view). A seat must not render it: fetch
            // the seat's own projection at head, and start the redacted
            // transcript from the top.
            this.seatSince = 0;
            void this.refreshLive();
            void this.backfillEvents(0);
          } else {
            this.setRenderedView(s.view, s.head);
          }
        }
        break;
      }
      case 'rewind': {
        const s = f.body as Snapshot;
        // A rewind is a same-match snapshot with a NON-monotonic head. Drop
        // every client-side tail and force the DVR live at the new head; old
        // pending decisions and in-flight reads belong to the discarded seq
        // space and must never reappear.
        this.liveEpoch++;
        this.seeking++;
        this.match = f.match ?? this.match;
        this.decision = null;
        this.halted = null;
        this.seats = s.seats;
        this.seatSince = 0;
        this.dispatch({ type: 'rewind', match: `${this.table}/${this.match}`, head: s.head, turnStarts: s.turn_starts });
        if (this.seat) {
          this.withdrawPaintedDecision();
          void this.refreshLive();
          void this.backfillEvents(0);
        } else {
          this.setRenderedView(s.view, s.head);
        }
        return true;
      }
      case 'event': {
        if (this.seat) {
          // Chit the public seq so head/cursor track the match; the
          // REDACTED lines for these events are backfilled on the next
          // decision boundary (backfillEvents), because the frame body is
          // redacted for the spectator, not for this seat.
          const seq = frameSeq(f);
          if (seq !== null) this.dispatch({ type: 'head', seq });
          break;
        }
        this.dispatch({ type: 'event', body: f.body as EventBody });
        break;
      }
      case 'decision':
        this.decision = f.body as DecisionBody;
        if (this.dvr.live) {
          void this.refreshLive();
          if (this.seat) void this.backfillEvents(this.seatSince + 1);
        }
        break;
      case 'match_end':
        this.decision = null;
        if (this.dvr.live) {
          void this.refreshLive();
          if (this.seat) void this.backfillEvents(this.seatSince + 1);
        }
        break;
      case 'table_halted':
        this.halted = (f.body as TableHaltedBody).reason;
        break;
    }
    return false;
  }

  /**
   * setRenderedView is the sole view-assignment edge. DVR cursor state can
   * move before its asynchronous fetch resolves, so breadcrumbs must follow
   * this assigned view sequence rather than the requested cursor.
   */
  private setRenderedView(view: View | null, seq: number | null) {
    this.emitModel(view, seq);
    this.view = view;
    this.renderedSeq = seq;
    clientBreadcrumbs.setView(seq, view?.decision?.seq ?? null);
  }

  /**
   * emitModel is the gorge adapter's one emit edge. Only a live, forward step
   * of the painted board is a `step` (its transitions can animate); anything
   * else that changes the board — a new match, a snapshot, a rewind, a DVR
   * scrub — is a `reset`. A same-seq repaint changes nothing and emits
   * nothing. With no listener it does no work at all.
   */
  private emitModel(view: View | null, seq: number | null) {
    if (this.model.size === 0) return;
    const prev = this.view;
    const prevSeq = this.renderedSeq;
    if (prev === view || (seq !== null && seq === prevSeq && prev !== null && view !== null)) return;
    if (prev && view && prevSeq !== null && seq !== null && seq > prevSeq && this.dvr.live) {
      this.model.emit({ type: 'step', prev, next: view, transitions: stepTransitions(prev, view, prevSeq, seq, this.dvr.events) });
    } else {
      this.model.emit({ type: 'reset' });
    }
  }

  /**
   * withdrawPaintedDecision is the seated half of a rewind's rule that old
   * pending decisions never reappear. The seated path keeps painting the
   * PRE-rewind seat view until refreshLive fetches the seat's projection at
   * the new head (the pushed rewind body is the spectator's), and that view
   * embeds the seat's decision from the discarded seq space. SeatPanel adopts
   * view.decision whenever its adopt effect re-runs -- and the panel's own
   * rewind() writes re-run it -- so the discarded ask was re-adopted into the
   * fresh seq space the rewind had just opened, where it became the seqHigh
   * fence and the restored, lower-seq decision was refused for good: the
   * 2026-09-26 demo g3 undo, where every answer carried seq 1526 against the
   * restored 1519. The board stays on screen; only its ask is withdrawn.
   */
  private withdrawPaintedDecision() {
    const v = this.view;
    if (v?.decision === undefined || v.decision === null) return;
    this.setRenderedView({ ...v, decision: null }, this.renderedSeq);
  }

  dispatch(a: DvrAction) {
    const wasLive = this.dvr.live;
    this.dvr = dvrReducer(this.dvr, a);
    if (a.type === 'snapshot' || a.type === 'rewind' || a.type === 'reset') this.cache.clear();
    // Going live — whether by an explicit 'live' action or a new match's
    // snapshot arriving live — permanently invalidates any paused-cursor
    // fetch still in flight: bump the token so a late resolution can never
    // win the "am I still the freshest request" check in showCursor.
    if (!wasLive && this.dvr.live) this.seeking++;
    if (!this.dvr.live && a.type !== 'event') void this.showCursor();
    if (a.type === 'live') void this.refreshLive();
  }

  private fetchViewAt(seq: number): Promise<View> {
    return this.seat ? fetchView(this.table, this.match!, seq, this.seat) : fetchView(this.table, this.match!, seq);
  }

  private fetchEventsAt(k: number, since: number): Promise<EventBody[]> {
    return this.seat ? fetchEvents(this.table, k, since, this.seat) : fetchEvents(this.table, k, since);
  }

  // Seat claims are process-local and table-bound. A redeploy deliberately
  // rejects an old join token; leave the stale table instead of swallowing
  // that 401/403 forever and presenting a page that can never refresh.
  private leaveRejectedSeatClaim(e: unknown) {
    if (this.seat && typeof location !== 'undefined' && e instanceof ApiError && (e.status === 401 || e.status === 403)) location.assign(withBase('/'));
  }

  /** refreshLive is PL-16: one GET per burst, coalesced. */
  async refreshLive() {
    if (this.match === null) return;
    if (this.inflight) { this.again = true; return; }
    this.inflight = true;
    const epoch = this.liveEpoch;
    const seq = this.dvr.head;
    try {
      const v = await this.fetchViewAt(seq);
      if (this.dvr.live && epoch === this.liveEpoch) this.setRenderedView(v, seq);
    } catch (e) { this.leaveRejectedSeatClaim(e); /* a 409 while the head moved: the next burst refetches */ }
    finally {
      this.inflight = false;
      if (this.again) { this.again = false; void this.refreshLive(); }
    }
  }

  /**
   * resync refetches everything the stream may have dropped. It is the
   * recovery edge for an overflow frame: the client cannot know WHICH frames
   * were discarded, so it trusts nothing it holds and re-reads the view (and,
   * for a seat, the transcript from the top) at the server's current head.
   * A paused DVR keeps its cursor — the player is reading history, and the
   * live tail they return to is fetched then.
   *
   * This is the EARLY repaint, not the authoritative one: the view is refetched
   * at the head we know, which an overflow may itself have left behind. The
   * server closes an overflowed session, so the browser's reconnect brings a
   * fresh snapshot carrying the true head and that repaints exactly. What this
   * buys is the case where the reconnect is slow or its re-subscribe fails —
   * the difference between a stale board and a frozen one.
   */
  resync() {
    if (this.match === null || !this.dvr.live) return;
    void this.refreshLive();
    if (this.seat) {
      this.seatSince = 0;
      void this.backfillEvents(0);
    }
  }

  /** backfillEvents paints the redacted transcript lines for the seated path: events since `since`, capped at the current head (anything past it is a race the next backfill covers). Returns normally on failure — the next decision boundary retries. */
  async backfillEvents(since: number) {
    if (this.match === null || !this.seat) return;
    const epoch = this.liveEpoch;
    try {
      const head = this.dvr.head;
      const all = await this.fetchEventsAt(this.match, since);
      if (epoch !== this.liveEpoch) return;
      this.dispatch({ type: 'backfill', events: all.filter((b) => b.event.seq <= head) });
      this.seatSince = head;
    } catch (e) { this.leaveRejectedSeatClaim(e); /* next boundary retries */ }
  }

  /** showCursor renders the view at the cursor (paused) and backfills the transcript when the cursor precedes the known events. */
  async showCursor() {
    if (this.match === null || this.dvr.live) return;
    const seq = this.dvr.cursor;
    const token = ++this.seeking;
    const first = this.dvr.events[0]?.event.seq ?? this.dvr.head + 1;
    if (seq < first) {
      const since = Math.max(0, seq - 200);
      const older = await this.fetchEventsAt(this.match, since).catch(() => []);
      this.dispatch({ type: 'backfill', events: older.filter((e) => e.event.seq < first) });
    }
    const v = await this.cache.get(seq).catch(() => null);
    // !this.dvr.live is belt-and-suspenders: dispatch() already bumps
    // seeking on any transition to live, but this guards directly against
    // ever writing a paused-cursor view once we're no longer paused,
    // regardless of how the token bookkeeping got there.
    if (v && token === this.seeking && !this.dvr.live) this.setRenderedView(v, seq);
  }

  /**
   * loadFinished renders a match that is not live: no subscription,
   * everything from the JSON GETs. Never rejects — a missing match or a
   * failed fetch is reported through loadError instead, so a stale
   * `/t/{t}/m/{k}` link (fire-and-forget from Table.svelte) can't leave an
   * unhandled rejection behind. Callers render from loadError, not a catch.
   */
  async loadFinished(k: number) {
    this.loadError = null;
    try {
      const infos = await fetchMatches(this.table);
      const info = infos.find((m) => m.match === k);
      if (!info || info.events === 0) throw new Error(`no match ${k}`);
      this.match = k;
      this.seats = info.seats;
      const all = await fetchEvents(this.table, k, 0);
      this.dispatch({ type: 'snapshot', match: `${this.table}/${k}`, head: info.events - 1, turnStarts: turnStartsFrom(all) });
      this.dispatch({ type: 'backfill', events: all });
      this.dispatch({ type: 'pause' });
      this.dispatch({ type: 'scrub', seq: info.events - 1 });
    } catch (e) {
      this.loadError = e instanceof Error ? e.message : String(e);
    }
  }
}
