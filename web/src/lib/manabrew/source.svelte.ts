import type { CardView, Decision, DecisionBody, EventBody, Intent, MatchInfo, SeatInfo, View } from '../../protocol';
import { ApiError, fetchMatches, postNativeUndo } from '../api';
import { dvrReducer, initialDvr, type DvrAction, type DvrState } from '../dvr';
import type { SeatCtx, SeatTransport } from '../seat';
import { ModelBus } from '../clientmodel/gorge';
import { transitionLine } from '../clientmodel/log';
import { diffViews } from '../clientmodel/snapshotdiff';
import type { ClientModelSource, ModelListener, PendingDecision, Transition } from '../clientmodel/types';
import { applyPatch, PatchError } from './patch';
import { compatPass, loadCompat, nothingToDo } from './compat';
import { AnswerError, bindPrompt, undoMessage, type PromptBinding } from './prompt';
import { projectView, rememberCards } from './project';
import { httpTransport, MbError, streamURL, type MbStream, type Transport } from './transport';
import type { AgentPrompt, ClientMessage, EngineMessage, GameViewDto } from './wire';

/**
 * ManaBrewMatch is the ManaBrew adapter (UI rework sub-project 0): the table
 * route's game source when the protocol setting is ManaBrew. It carries the
 * same surface the route reads from MatchState (match, view, seats, dvr,
 * dispatch, halted, …) and implements ClientModelSource, so the board,
 * motion, log and seat panel are unchanged.
 *
 * The protocol has no event log. Transitions come from diffing successive
 * projected snapshots (diffViews); the transcript is built from those
 * transitions (transitionLine); and the DVR is a client-side ring of
 * snapshots, each at a synthetic seq that follows its log lines.
 *
 * It is also the seat's SeatTransport: the panel's intents, pending reads
 * and undo requests are answered over ManaBrew's /send, never the native
 * intent route.
 */

const RING = 500;

export type StartResult = { ok: true } | { ok: false; reason: string };

interface RingEntry { seq: number; view: View }

interface Deps {
  transport: Transport;
  fetchMatches: (t: string) => Promise<MatchInfo[]>;
  /** The compat auto-pass (compat.ts); read from the setting when not given. */
  compat: boolean;
}

export class ManaBrewMatch implements ClientModelSource, SeatTransport {
  readonly protocol = 'manabrew' as const;
  match = $state<number | null>(null);
  view = $state<View | null>(null);
  renderedSeq = $state<number | null>(null);
  seats = $state<SeatInfo[]>([]);
  dvr = $state<DvrState>(initialDvr);
  /** The spectator decision line; a ManaBrew source is always seated, so never set. */
  decision = $state<DecisionBody | null>(null);
  halted = $state<string | null>(null);
  loadError = $state<string | null>(null);
  /** The last protocol error or degradation worth showing, until dismissed or superseded. */
  notice = $state<string | null>(null);
  /** Called when a prompt arrives with a LOWER id than the last: an undo landed. */
  onRewind: (() => void) | null = null;

  private model = new ModelBus();
  private stream: MbStream | null = null;
  private deps: Deps;
  private gv: GameViewDto | null = null;
  private live: View | null = null;
  private binding: PromptBinding | null = null;
  private lastPromptId = -1;
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- private projection cache (last-known card faces), never rendered
  private known = new Map<number, CardView>();
  private ring: RingEntry[] = [];
  private seq = 0;
  private closed = false;
  /** How many prompts the compat rule answered with a pass (diagnostics). */
  autoPassed = 0;

  constructor(readonly table: string, readonly seat: SeatCtx, deps: Partial<Deps> = {}) {
    this.deps = { transport: deps.transport ?? httpTransport, fetchMatches: deps.fetchMatches ?? fetchMatches, compat: deps.compat ?? loadCompat() };
  }

  /** The seat context the panel answers through: this adapter is its transport. */
  get ctx(): SeatCtx {
    return { seat: this.seat.seat, token: this.seat.token, transport: this };
  }

  get clientDecision(): PendingDecision | null {
    return this.view?.decision ?? null;
  }

  onModel(fn: ModelListener): () => void {
    return this.model.on(fn);
  }

  /**
   * start finds the table's live match, probes the ManaBrew state route and
   * opens the stream. A server without -manabrew (or a refused token)
   * resolves {ok:false} with a reason and leaves nothing open, so the route
   * can fall back to the native wire without waiting on a stream that will
   * never speak.
   */
  async start(): Promise<StartResult> {
    let info: MatchInfo | undefined;
    try {
      const ms = await this.deps.fetchMatches(this.table);
      info = ms.find((m) => m.state === 'live') ?? [...ms].sort((a, b) => b.match - a.match)[0];
    } catch (e) {
      return { ok: false, reason: `could not list this table's matches (${e instanceof Error ? e.message : String(e)})` };
    }
    if (!info) return { ok: false, reason: 'the table has no match yet' };
    let first: EngineMessage[];
    try {
      first = await this.deps.transport.fetchState(this.table, info.match, this.seat.token);
    } catch (e) {
      const why = e instanceof MbError ? e : null;
      if (why && why.status === 404) return { ok: false, reason: `ManaBrew is not enabled on this server (${why.message})` };
      if (why && (why.status === 401 || why.status === 403)) return { ok: false, reason: `the ManaBrew route refused this seat (${why.message})` };
      return { ok: false, reason: `the ManaBrew route did not answer (${e instanceof Error ? e.message : String(e)})` };
    }
    if (!first.some((m) => m.kind === 'state')) return { ok: false, reason: 'the ManaBrew route answered without a game state' };
    if (this.closed) return { ok: true };
    this.seats = info.seats;
    this.match = info.match;
    this.dvr = dvrReducer(this.dvr, { type: 'snapshot', match: `${this.table}/${info.match}`, head: 0, turnStarts: [] });
    for (const m of first) this.handle(m);
    this.stream = this.deps.transport.openStream(streamURL(this.table, info.match, this.seat.token), (m) => this.handle(m), () => this.onStreamError());
    return { ok: true };
  }

  close() {
    this.closed = true;
    this.stream?.close();
    this.stream = null;
  }

  private onStreamError() {
    // EventSource reconnects by itself and the server re-sends the full
    // state and open prompt on connect; say so only if nothing arrives.
    if (!this.closed && this.view === null) this.notice = 'ManaBrew stream interrupted; reconnecting…';
  }

  /** handle consumes one engine message (exported for tests through the class). */
  handle(m: EngineMessage) {
    switch (m.kind) {
      case 'state':
        this.acceptState(m.gameView);
        break;
      case 'stateDelta': {
        if (this.gv === null) {
          void this.resync('a patch arrived before any state');
          break;
        }
        try {
          this.acceptState(applyPatch(this.gv, m.patch));
        } catch (e) {
          void this.resync(e instanceof PatchError ? e.message : String(e));
        }
        break;
      }
      case 'prompt':
        this.acceptPrompt(m);
        break;
      case 'error':
        this.notice = `ManaBrew ${m.error.code}: ${m.error.message}`;
        break;
      case 'display':
        break; // WIP upstream, not authoritative
    }
  }

  private async resync(why: string) {
    if (this.match === null) return;
    try {
      const msgs = await this.deps.transport.fetchState(this.table, this.match, this.seat.token);
      for (const m of msgs) if (m.kind !== 'stateDelta') this.handle(m);
    } catch (e) {
      this.notice = `ManaBrew resync failed after ${why}: ${e instanceof Error ? e.message : String(e)}`;
    }
  }

  private projected(gv: GameViewDto): View {
    return this.decorate(projectView(gv, { viewer: this.seat.seat, known: this.known }));
  }

  /**
   * decorate lays the open prompt over a projected board: the decision, and
   * the seat's potential plays. ManaBrew carries no potential_actions, but
   * an announce-able cast (`pay-<id>`) is exactly one — a spell the engine
   * would cast once mana is paid — so the autopilot's smart stops and the
   * float-then-cast affordances see the same "you have a play" the native
   * projection gives them.
   */
  private decorate(v: View): View {
    const d = this.binding?.decision ?? null;
    const plays = (d?.payment_actions ?? []).map((a) => ({ kind: 'cast', obj: a.cast.object, label: a.label }));
    return {
      ...v,
      decision: d,
      players: v.players.map((p) => (p.seat === this.seat.seat ? { ...p, potential_actions: plays.length > 0 ? plays : undefined } : p)),
    };
  }

  private acceptState(gv: GameViewDto) {
    this.gv = gv;
    const prev = this.live;
    const next = this.projected(gv);
    rememberCards(next, this.known);
    this.live = next;
    if (prev === null) {
      this.pushRing(next);
      this.show(next, 'reset');
      return;
    }
    const transitions = diffViews(prev, next);
    const lines: string[] = [];
    if (next.turn !== prev.turn) lines.push(`Turn ${next.turn} — ${this.seatName(next.active)}`);
    for (const t of transitions) {
      const l = transitionLine(t, (s) => this.seatName(s));
      if (l !== null) lines.push(l);
    }
    for (const line of lines) this.log(line, next);
    if (next.turn !== prev.turn) this.dvr = { ...this.dvr, turnStarts: [...this.dvr.turnStarts, this.seq - lines.length + 1] };
    this.pushRing(next);
    this.show(next, 'step', prev, transitions);
  }

  private acceptPrompt(p: AgentPrompt) {
    const rewound = p.promptId < this.lastPromptId;
    if (rewound) {
      // A restored, lower-seq ask: an undo landed. The board's history
      // stays in the ring; the panel re-bases its seq fences.
      this.onRewind?.();
      this.model.emit({ type: 'reset' });
    }
    this.lastPromptId = p.promptId;
    // Compat: a priority ask with nothing to do but make mana is passed
    // unseen, as ManaBrew's own engine never poses one. A rewound ask (an
    // undo) is always shown, so the undo lands where the player can see it.
    if (this.deps.compat && !rewound && nothingToDo(p)) {
      this.autoPassed += 1;
      if (this.binding !== null) this.withdraw();
      this.sendMsg(compatPass(p)).catch(() => {
        // sendMsg already put the failure on the notice banner.
      });
      return;
    }
    this.binding = bindPrompt(p, { view: this.live });
    if (this.live === null) return;
    this.live = this.decorate(this.live);
    if (this.ring.length > 0) this.ring[this.ring.length - 1] = { ...this.ring[this.ring.length - 1], view: this.live };
    if (this.dvr.live) this.assign(this.live);
  }

  private seatName(s: number): string {
    return this.seats[s]?.name || this.live?.players.find((p) => p.seat === s)?.name || `Seat ${s}`;
  }

  private log(line: string, v: View) {
    this.seq++;
    const body: EventBody = { event: { seq: this.seq, kind: 'manabrew', player: v.active }, line };
    this.dvr = dvrReducer(this.dvr, { type: 'event', body });
  }

  private pushRing(v: View) {
    const last = this.ring[this.ring.length - 1];
    if (last && last.seq === this.seq) last.view = v;
    else this.ring.push({ seq: this.seq, view: v });
    if (this.ring.length > RING) this.ring.splice(0, this.ring.length - RING);
    if (this.dvr.head < this.seq) this.dvr = dvrReducer(this.dvr, { type: 'head', seq: this.seq });
  }

  /** show paints a live view: a forward step animates, anything else resets motion. */
  private show(next: View, how: 'step' | 'reset', prev?: View, transitions: Transition[] = []) {
    if (!this.dvr.live) return;
    if (how === 'step' && prev && this.view !== null) this.model.emit({ type: 'step', prev: this.view, next, transitions });
    else this.model.emit({ type: 'reset' });
    this.assign(next);
  }

  private assign(v: View) {
    this.view = v;
    this.renderedSeq = this.seq;
  }

  /** dispatch drives the DVR over the snapshot ring. */
  dispatch(a: DvrAction) {
    this.dvr = dvrReducer(this.dvr, a);
    if (this.dvr.live) {
      if (this.live && this.view !== this.live) {
        this.model.emit({ type: 'reset' });
        this.assign(this.live);
      }
      return;
    }
    const at = this.snapshotAt(this.dvr.cursor);
    if (at && at !== this.view) {
      this.model.emit({ type: 'reset' });
      this.view = { ...at, decision: null };
      this.renderedSeq = this.dvr.cursor;
    }
  }

  /** snapshotAt is the newest ring snapshot at or before seq. */
  snapshotAt(seq: number): View | null {
    let found: View | null = null;
    for (const e of this.ring) if (e.seq <= seq) found = e.view;
    return found ?? this.ring[0]?.view ?? null;
  }

  // ---- SeatTransport -------------------------------------------------

  async postIntent(_t: string, _k: number, intent: Intent): Promise<void> {
    const b = this.binding;
    if (b === null || b.decision === null) throw new ApiError(409, 'stalePrompt', 'no ManaBrew prompt is open');
    let msg: ClientMessage;
    try {
      msg = b.respond(intent);
    } catch (e) {
      const text = e instanceof AnswerError ? e.message : String(e);
      this.notice = `ManaBrew: ${text}`;
      throw new ApiError(400, 'unsupported', text);
    }
    await this.sendMsg(msg);
    // Answered: withdraw that prompt, unless a newer one already arrived.
    if (this.binding?.prompt.promptId === b.prompt.promptId) this.withdraw();
  }

  async fetchPending(): Promise<Decision> {
    const d = this.binding?.decision ?? null;
    if (d === null) throw new ApiError(409, 'conflict', 'nothing is pending for this seat');
    return d;
  }

  async postUndo(): Promise<void> {
    const prompt = this.binding?.prompt ?? null;
    if (prompt?.input.type !== 'chooseAction') {
      if (this.match === null) throw new ApiError(409, 'conflict', 'no match');
      return postNativeUndo(this.table, this.match, this.seat);
    }
    await this.sendMsg(undoMessage(prompt));
  }

  private async sendMsg(msg: ClientMessage) {
    if (this.match === null) throw new ApiError(409, 'conflict', 'no match');
    try {
      await this.deps.transport.send(this.table, this.match, this.seat.token, msg);
    } catch (e) {
      if (e instanceof MbError) {
        this.notice = `ManaBrew ${e.code}: ${e.message}`;
        throw new ApiError(e.status, e.code, e.message);
      }
      throw e;
    }
  }

  private withdraw() {
    this.binding = null;
    if (this.live) {
      this.live = this.decorate(this.live);
      if (this.dvr.live) this.assign(this.live);
    }
  }

  /** The currently open prompt (tests and diagnostics). */
  get openPrompt(): AgentPrompt | null {
    return this.binding?.prompt ?? null;
  }
}
