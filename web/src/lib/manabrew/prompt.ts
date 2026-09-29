import type { Decision, Intent, Option, PaymentAction, View } from '../../protocol';
import { objOfCardId, objOfStackId, seatOfPlayerId } from './ids';
import type { AgentPrompt, ClientMessage, PromptOutput, PromptType, TargetRef } from './wire';

/**
 * prompt.ts reconstructs the native-shaped `Decision` the seat panel answers
 * from a ManaBrew `AgentPrompt` (the inverse of gorge's
 * internal/manabrew/prompt_*.go), and maps the panel's `Intent` — option
 * indices — back to the ManaBrew `response` for that prompt.
 *
 * Option indices are DENSE (0..n-1) and each one is bound to the wire answer
 * it stands for; the server's own `opt-N` action ids travel in that binding
 * and are never assumed equal to the index. Where the protocol has dropped a
 * fact the panel's layout keys on (a card pick's verb, the starting-player
 * ask), it is inferred from presentation text — presentation only: the
 * server matches every answer by id, never by that inference.
 */

export class AnswerError extends Error {}

/** One option's wire meaning. */
type Bound =
  | { t: 'act'; actionId: string }
  | { t: 'pass' }
  | { t: 'concede' }
  | { t: 'target'; ref: TargetRef }
  | { t: 'attack'; attackerId: string; targetId: string }
  | { t: 'block'; blockerId: string; attackerId: string }
  | { t: 'keep'; keep: boolean }
  | { t: 'card'; id: string }
  | { t: 'index'; i: number }
  | { t: 'bool'; value: boolean }
  | { t: 'number'; n: number }
  | { t: 'color'; color: string }
  | { t: 'item'; id: string }
  | { t: 'pay'; auto: boolean }
  | { t: 'cancel' }
  | { t: 'ack' };

export interface PromptBinding {
  prompt: AgentPrompt;
  decision: Decision | null;
  /** respond maps the panel's intent to the ManaBrew message that answers this prompt. */
  respond(intent: Intent): ClientMessage;
}

export interface PromptContext {
  /** The view the prompt arrived with (names, zones, the stack), already projected. */
  view: View | null;
}

const COLOR_NAME: Record<string, string> = { W: 'White', U: 'Blue', B: 'Black', R: 'Red', G: 'Green', C: 'Colorless' };

const seat = (id: string | undefined): number => seatOfPlayerId(id) ?? 0;

function cardName(v: View | null, obj: number | null, fallback = ''): string {
  if (!v || obj === null) return fallback;
  for (const p of v.players) for (const z of [p.hand ?? [], p.battlefield, p.graveyard, p.exile, p.command]) for (const c of z) if (c.id === obj) return c.name || fallback;
  for (const s of v.stack) if (s.id === obj) return s.name || fallback;
  return fallback;
}

function playerName(v: View | null, s: number): string {
  return v?.players.find((p) => p.seat === s)?.name ?? `Player ${s}`;
}

/** where locates a card in the view: its zone, and its controller on the battlefield. */
function where(v: View | null, obj: number): { zone: string; controller: number } | null {
  if (!v) return null;
  for (const p of v.players) {
    for (const c of p.battlefield) if (c.id === obj) return { zone: 'battlefield', controller: c.controller };
    for (const c of p.graveyard) if (c.id === obj) return { zone: 'graveyard', controller: p.seat };
    for (const c of p.exile) if (c.id === obj) return { zone: 'exile', controller: p.seat };
    for (const c of p.hand ?? []) if (c.id === obj) return { zone: 'hand', controller: p.seat };
  }
  return null;
}

/** pickVerb infers a card pick's option kind from its title (presentation only). */
function pickVerb(title: string): string {
  const t = title.toLowerCase();
  if (t.includes('discard')) return 'discard';
  if (t.includes('search')) return 'search';
  if (t.includes('sacrific')) return 'sacrifice';
  if (t.includes('exile')) return 'exile';
  if (t.includes('keep')) return 'keep';
  return 'dig';
}

const out = (type: PromptType, output: PromptOutput['output']): PromptOutput => ({ type, output });

/**
 * decisionFor reconstructs the Decision for a prompt, and its binding.
 * `gameOver` has no decision (it takes no answer).
 */
export function bindPrompt(p: AgentPrompt, ctx: PromptContext): PromptBinding {
  const v = ctx.view;
  const player = seat(p.decidingPlayerId);
  const opts: Option[] = [];
  const bound: Bound[] = [];
  const add = (o: Omit<Option, 'index' | 'player'> & { player?: number }, b: Bound) => {
    opts.push({ index: opts.length, player, ...o } as Option);
    bound.push(b);
  };
  const input = p.input;
  const title = 'presentation' in input ? (input.presentation?.title ?? '') : '';
  const source = objOfCardId(p.sourceCard?.id);
  const base = { seq: p.promptId, player, prompt: title, ...(source !== null && source !== 0 ? { source } : {}) };
  let d: Decision | null = null;
  const payment: PaymentAction[] = [];

  switch (input.type) {
    case 'chooseAction': {
      for (const a of input.actions) {
        const obj = objOfCardId(a.cardId) ?? undefined;
        const label = a.label ?? a.modeLabel ?? a.description ?? '';
        if (a.type === 'cast' && a.id.startsWith('pay-')) {
          // An announce-able cast: the protocol carries no payment plans,
          // so it is offered plan-less (see the plan's ruling).
          payment.push({ id: a.id.slice(4), cast: { object: obj ?? 0, face: 0, origin: 'hand' }, label, plans: [] });
          continue;
        }
        if (a.type === 'cast') add({ kind: a.mode === 'play' ? 'play_land' : 'cast', label, obj, ...(a.mode && a.mode !== 'play' ? { mode: a.mode } : {}) }, { t: 'act', actionId: a.id });
        else if (a.type === 'activateAbility') add({ kind: a.isManaAbility ? 'activate' : 'ability', label, obj, ...(a.abilityIndex !== undefined ? { ability: a.abilityIndex } : {}) }, { t: 'act', actionId: a.id });
        else add({ kind: a.type === 'undoMana' ? 'undo_tap' : 'ability', label, obj }, { t: 'act', actionId: a.id });
      }
      add({ kind: 'pass', label: 'Pass priority' }, { t: 'pass' });
      add({ kind: 'concede', label: 'Concede' }, { t: 'concede' });
      const prompt = v ? `turn ${v.turn}, ${v.step} — ${playerName(v, player)} has priority` : 'Priority';
      d = { ...base, kind: 'priority', prompt, min: 1, max: 1, options: opts, ...(payment.length ? { payment_actions: payment } : {}) };
      break;
    }
    case 'chooseBoardTargets': {
      for (const c of input.candidates) {
        const label = c.oracle ?? c.id;
        if (c.kind === 'player') add({ kind: 'player', label, player: seat(c.id) }, { t: 'target', ref: c });
        else if (c.kind === 'spell') {
          const obj = objOfStackId(c.id) ?? 0;
          const s = v?.stack.find((x) => x.id === obj);
          add({ kind: s && s.kind !== 'spell' ? s.kind : 'spell', label, obj, player: s?.controller ?? 0 }, { t: 'target', ref: c });
        } else {
          const obj = objOfCardId(c.id) ?? 0;
          const at = where(v, obj);
          add({ kind: at?.zone === 'graveyard' ? 'graveyard' : 'permanent', label, obj, player: at?.controller ?? 0 }, { t: 'target', ref: c });
        }
      }
      d = { ...base, kind: 'target', min: input.minTargets, max: input.maxTargets, options: opts };
      break;
    }
    case 'chooseAttackers': {
      const tlabel = new Map(input.attackTargets.map((t) => [t.id, t.label]));
      for (const a of input.attackers) {
        const obj = objOfCardId(a.attackerId) ?? 0;
        const name = cardName(v, obj, a.attackerId);
        for (const tid of a.validTargetIds) {
          const tseat = seatOfPlayerId(tid);
          const label = `Attack with ${name} at ${tlabel.get(tid) ?? tid}`;
          const at = tseat !== null ? { player: tseat } : { player: 0, battle: objOfCardId(tid) ?? 0 };
          add({ kind: 'attacker', label, obj, ...at, ...(a.mustAttack ? { required: true } : {}) }, { t: 'attack', attackerId: a.attackerId, targetId: tid });
        }
      }
      d = { ...base, kind: 'attackers', prompt: v ? `turn ${v.turn} — declare attackers` : 'Declare attackers', min: 0, max: input.attackers.length, options: opts };
      break;
    }
    case 'chooseBlockers': {
      for (const a of input.attackers) {
        const attacker = objOfCardId(a.attackerId) ?? 0;
        for (const bid of a.validBlockerIds) {
          const obj = objOfCardId(bid) ?? 0;
          add({
            kind: 'block', label: `${cardName(v, obj, bid)} blocks ${cardName(v, attacker, a.attackerId)}`, obj, attacker, group: `blocker:${obj}`,
            ...(a.minBlockers ? { min_blockers: a.minBlockers } : {}), ...(a.maxBlockers ? { max_blockers: a.maxBlockers } : {}),
          }, { t: 'block', blockerId: bid, attackerId: a.attackerId });
        }
      }
      d = { ...base, kind: 'blockers', prompt: v ? `turn ${v.turn} — declare blockers` : 'Declare blockers', min: 0, max: opts.length, options: opts };
      break;
    }
    case 'mulligan':
      add({ kind: 'keep', label: 'keep' }, { t: 'keep', keep: true });
      add({ kind: 'mulligan', label: 'mulligan' }, { t: 'keep', keep: false });
      d = { ...base, kind: 'mulligan', prompt: 'Keep your hand or take a mulligan?', min: 1, max: 1, options: opts };
      break;
    case 'mulliganPutBack':
      input.handCardIds.forEach((id, i) => {
        const obj = objOfCardId(id) ?? 0;
        add({ kind: 'bottom', label: input.cards[i]?.identity?.name ?? cardName(v, obj, id), obj }, { t: 'card', id });
      });
      d = { ...base, kind: 'mulligan', prompt: `Put ${input.count} card(s) on the bottom of your library`, min: input.count, max: input.count, options: opts };
      break;
    case 'chooseFromSelection': {
      const names = new Map((v?.players ?? []).map((pl) => [pl.name, pl.seat]));
      const players = input.options.length === (v?.players.length ?? -1) && input.options.every((o) => names.has(o.label));
      input.options.forEach((o, i) => {
        if (players) add({ kind: 'player', label: o.label, player: names.get(o.label) ?? 0 }, { t: 'index', i });
        else add({ kind: 'choice', label: o.label, ...(o.weight !== undefined && o.weight !== 1 ? { amount: o.weight } : {}) }, { t: 'index', i });
      });
      const repeat = input.options.some((o) => o.canRepeat);
      d = { ...base, kind: players ? 'starting_player' : 'choose', min: input.minTotal, max: input.maxTotal, options: opts, ...(repeat ? { repeatable: true } : {}) };
      break;
    }
    case 'chooseBoolean':
      add({ kind: 'yes', label: input.confirmLabel || 'Yes' }, { t: 'bool', value: true });
      add({ kind: 'no', label: input.denyLabel || 'No' }, { t: 'bool', value: false });
      d = { ...base, kind: 'choose', min: 1, max: 1, options: opts };
      break;
    case 'chooseCards': {
      const verb = pickVerb(title);
      for (const c of input.cards) {
        const name = c.identity?.name ?? '';
        add({ kind: verb, label: verb === 'discard' ? `Discard ${name}` : name, obj: objOfCardId(c.id) ?? 0 }, { t: 'card', id: c.id });
      }
      d = { ...base, kind: 'choose', min: input.min, max: input.max, options: opts };
      break;
    }
    case 'chooseNumber':
      for (let n = input.min; n <= input.max; n++) add({ kind: 'number', label: String(n), amount: n }, { t: 'number', n });
      d = { ...base, kind: 'choose', min: 1, max: 1, options: opts };
      break;
    case 'chooseColor':
      for (const c of input.validColors) add({ kind: 'color', label: COLOR_NAME[c] ?? c, mana_symbol: c }, { t: 'color', color: c });
      d = { ...base, kind: 'choose', min: input.amount, max: input.amount, options: opts, ...(input.repeatAllowed ? { repeatable: true } : {}) };
      break;
    case 'reorder':
      for (const it of input.items) add({ kind: 'trigger', label: it.oracle ?? it.card?.identity?.name ?? it.id, obj: objOfCardId(it.card?.id) ?? undefined }, { t: 'item', id: it.id });
      d = { ...base, kind: 'trigger_order', min: opts.length, max: opts.length, options: opts };
      break;
    case 'scry': {
      const dest = input.zones[1] ?? 'libraryBottom';
      const kind = dest === 'graveyard' ? 'graveyard' : dest === 'exile' ? 'exile' : dest === 'hand' ? 'hand' : 'bottom';
      for (const c of input.cards) add({ kind, label: c.identity?.name ?? '', obj: objOfCardId(c.id) ?? 0 }, { t: 'card', id: c.id });
      d = { ...base, kind: 'arrange', min: 0, max: opts.length, options: opts, restable: true };
      break;
    }
    case 'payManaCost':
      for (const a of input.actions) {
        const kind = a.type === 'undoMana' ? 'undo_tap' : a.type === 'autofill' ? 'autofill' : 'mana';
        if (kind === 'autofill') add({ kind, label: a.description ?? 'Auto-pay' }, { t: 'pay', auto: true });
        else add({ kind, label: a.description ?? '', obj: objOfCardId(a.cardId) ?? undefined }, { t: 'act', actionId: a.id });
      }
      if (input.canConfirmFromPool) add({ kind: 'done', label: 'Done' }, { t: 'pay', auto: false });
      add({ kind: 'cancel_cast', label: 'Cancel' }, { t: 'cancel' });
      d = { ...base, kind: 'choose', prompt: title || `Pay ${input.manaCost} for ${input.cardName}`, min: 1, max: 1, options: opts };
      break;
    case 'revealCards':
    case 'diceRolled':
      add({ kind: 'yes', label: 'Continue' }, { t: 'ack' });
      d = { ...base, kind: 'choose', min: 1, max: 1, options: opts };
      break;
    case 'chooseDamageAssignmentOrder':
      for (const id of input.blockerIds) add({ kind: 'trigger', label: cardName(v, objOfCardId(id), id), obj: objOfCardId(id) ?? 0 }, { t: 'item', id });
      d = { ...base, kind: 'trigger_order', prompt: 'Order blockers for damage', min: opts.length, max: opts.length, options: opts };
      break;
    case 'chooseCombatDamageAssignment':
      // gorge never poses this (trample is automatic, division is a
      // selection); a server that did would get a loud, unanswerable ask.
      d = { ...base, kind: 'choose', prompt: 'Combat damage assignment is not supported by this client', min: 1, max: 1, options: [] };
      break;
    case 'gameOver':
      d = null;
      break;
  }

  const respond = (intent: Intent): ClientMessage => {
    if (d === null) throw new AnswerError('this prompt takes no answer');
    if (intent.seq !== p.promptId) throw new AnswerError(`answer for ${intent.seq}, but the open prompt is ${p.promptId}`);
    const picks = intent.choices.map((i) => {
      const b = bound[i];
      if (b === undefined) throw new AnswerError(`option ${i} is not offered`);
      return b;
    });
    const response = (action: PromptOutput): ClientMessage => ({ kind: 'response', promptId: p.promptId, action });
    switch (input.type) {
      case 'chooseAction': {
        if (intent.payment) throw new AnswerError('planned payments are not available over ManaBrew; tap mana, then cast');
        if (intent.announce) return response(out('chooseAction', { type: 'act', actionId: `pay-${intent.announce.action_id}` }));
        const b = picks[0];
        if (!b) throw new AnswerError('no option chosen');
        if (b.t === 'pass') return response(out('chooseAction', { type: 'pass', exhaustStack: false }));
        if (b.t === 'concede') return { kind: 'directive', directive: { type: 'concede' } };
        if (b.t === 'act') return response(out('chooseAction', { type: 'act', actionId: b.actionId }));
        break;
      }
      case 'chooseBoardTargets':
        return response(out('chooseBoardTargets', { type: 'boardTargets', chosen: picks.map((b) => (b.t === 'target' ? b.ref : null)).filter((r) => r !== null) }));
      case 'chooseAttackers':
        return response(out('chooseAttackers', { type: 'declareAttackers', assignments: picks.flatMap((b) => (b.t === 'attack' ? [{ attackerId: b.attackerId, targetId: b.targetId }] : [])) }));
      case 'chooseBlockers':
        return response(out('chooseBlockers', { type: 'declareBlockers', assignments: picks.flatMap((b) => (b.t === 'block' ? [{ blockerId: b.blockerId, attackerId: b.attackerId }] : [])) }));
      case 'mulligan': {
        const b = picks[0];
        if (b?.t === 'keep') return response(out('mulligan', { type: 'mulliganDecision', keep: b.keep }));
        break;
      }
      case 'mulliganPutBack':
        return response(out('mulliganPutBack', { type: 'mulliganPutBackDecision', cardIds: picks.flatMap((b) => (b.t === 'card' ? [b.id] : [])) }));
      case 'chooseFromSelection':
        return response(out('chooseFromSelection', { type: 'selectionDecision', chosenIndices: picks.flatMap((b) => (b.t === 'index' ? [b.i] : [])) }));
      case 'chooseBoolean': {
        const b = picks[0];
        if (b?.t === 'bool') return response(out('chooseBoolean', { type: 'decision', value: b.value }));
        break;
      }
      case 'chooseCards':
        return response(out('chooseCards', { type: 'chooseCardsDecision', chosenCardIds: picks.flatMap((b) => (b.t === 'card' ? [b.id] : [])) }));
      case 'chooseNumber': {
        const b = picks[0];
        if (b?.t === 'number') return response(out('chooseNumber', { type: 'numberDecision', chosenNumber: b.n }));
        break;
      }
      case 'chooseColor': {
        const chosen: Record<string, number> = {};
        for (const b of picks) if (b.t === 'color') chosen[b.color] = (chosen[b.color] ?? 0) + 1;
        return response(out('chooseColor', { type: 'colorDecision', chosenColors: chosen }));
      }
      case 'reorder': {
        // The panel answers a trigger order in gorge's placement order
        // (first chosen goes on the stack first, so resolves LAST); ManaBrew
        // wants resolution order (first id resolves first): reverse once.
        const ids = picks.flatMap((b) => (b.t === 'item' ? [b.id] : []));
        return response(out('reorder', { type: 'reorderDecision', orderedIds: ids.reverse() }));
      }
      case 'chooseDamageAssignmentOrder':
        return response(out('chooseDamageAssignmentOrder', { type: 'damageAssignmentOrderDecision', orderedBlockerIds: picks.flatMap((b) => (b.t === 'item' ? [b.id] : [])) }));
      case 'scry': {
        const top = picks.flatMap((b) => (b.t === 'card' ? [b.id] : []));
        const restIdx = intent.rest ?? bound.map((_, i) => i).filter((i) => !intent.choices.includes(i));
        const rest = restIdx.map((i) => bound[i]).flatMap((b) => (b?.t === 'card' ? [b.id] : []));
        return response(out('scry', { type: 'scryDecision', zoneCardIds: [top, rest] }));
      }
      case 'payManaCost': {
        const b = picks[0];
        if (b?.t === 'act') return response(out('payManaCost', { type: 'act', actionId: b.actionId }));
        if (b?.t === 'pay') return response(out('payManaCost', { type: 'pay', auto: b.auto }));
        if (b?.t === 'cancel') return response(out('payManaCost', { type: 'cancel' }));
        break;
      }
      case 'revealCards':
        return response(out('revealCards', { type: 'revealCardsAcknowledged' }));
      case 'diceRolled':
        return response(out('diceRolled', { type: 'diceRolledAcknowledged' }));
      default:
        break;
    }
    throw new AnswerError(`cannot answer ${input.type} with options ${intent.choices.join(',')}`);
  };

  return { prompt: p, decision: d, respond };
}

/** concedeMessage is the out-of-band concession (legal at any time). */
export const concedeMessage = (): ClientMessage => ({ kind: 'directive', directive: { type: 'concede' } });

/** undoMessage asks the server to undo this seat's last answer (scope spec §6.4): only while a chooseAction is open. */
export function undoMessage(p: AgentPrompt | null): ClientMessage {
  if (!p || p.input.type !== 'chooseAction') throw new AnswerError('undo is only available at a priority prompt over ManaBrew');
  return { kind: 'response', promptId: p.promptId, action: out('chooseAction', { type: 'restoreSnapshot', checkpointId: p.promptId }) };
}
