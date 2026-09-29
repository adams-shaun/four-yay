import type { Decision, View } from '../protocol';
import { everyVisibleCard } from './board';

/**
 * prompt.ts is the prompt-context vocabulary (brief Job 3): what a prompt
 * surface needs to say about a decision beyond its own text — the source
 * card the decision resolves for, and the shape of the answer the engine
 * expects. Both come from fields already on the wire: `Decision.source`
 * (survey #18) and `kind`/`min`/`max`. Nothing here derives rules facts;
 * a source the view cannot see (it left every visible zone) degrades to
 * null and the context line simply omits it.
 */

/** SHAPE_NOUN names the answer's unit per decision kind, in the UI's own words. Kinds not listed fall back to the generic "option". */
const SHAPE_NOUN: Record<string, string> = {
  'target': 'target',
  'choose': 'choice',
  'modes': 'mode',
  'attackers': 'attacker',
  'blockers': 'block',
  'discard': 'card',
  'search': 'card',
};

/**
 * shapeOf renders the response shape a seat is being asked for, from the
 * decision's own kind/min/max — the wire facts, never option labels. An
 * ordering ask (arrange, trigger_order) says "Order N"; the count asks say
 * "Pick …" with the min/max range in words. `priority` returns null: its
 * shape ("act or pass") is stated by the transport controls themselves, and
 * a line repeating it is noise.
 */
export function shapeOf(d: Decision): string | null {
  switch (d.kind) {
    case 'arrange':
    case 'trigger_order': {
      if (d.min === d.max) return d.min === 1 ? 'Order 1' : `Order ${d.min}`;
      if (d.min === 0) return `Order up to ${d.max}`;
      return `Order ${d.min}–${d.max}`;
    }
    case 'mulligan':
      // The mulligan layout speaks for itself (keep/mulligan buttons, the
      // bottoming submit); a shape line would say it twice.
      return null;
    case 'trigger_optional':
      // The ask is a yes/no on an OPTIONAL triggered ability (Min == Max == 1
      // over "yes"/"no", engine rules/trigger_queue.go): "Pick 1 option"
      // reads as a mandatory selection of one of N, which is exactly the
      // misreading the optional-trigger report describes. The shape line says
      // both halves — the ability is optional, the answer is still a choice
      // between yes and no.
      return 'Optional ability — choose Yes or No';
    case 'priority':
      return null;
    default: {
      const noun = SHAPE_NOUN[d.kind] ?? 'option';
      const unit = (n: number) => (n === 1 ? `1 ${noun}` : `${n} ${noun}s`);
      if (d.min === d.max) return `Pick ${unit(d.min)}`;
      if (d.min === 0) return `Pick up to ${unit(d.max)}`;
      return `Pick ${d.min}–${d.max} ${noun}s`;
    }
  }
}

/**
 * sourceStackOf finds the stack object responsible for a decision source.
 * A spell uses its own object ID, while a trigger/activated ability has a
 * minted stack ID and carries the originating permanent in StackView.source.
 * Search top-down: when one permanent has several abilities on the stack,
 * the resolving (topmost) one is the only defensible best-effort cause.
 */
export function sourceStackOf(d: Decision, view: View) {
  if (d.source === undefined || d.source === 0) return null;
  for (let i = view.stack.length - 1; i >= 0; i -= 1) {
    const stack = view.stack[i];
    if (stack.id === d.source || stack.source === d.source) return stack;
  }
  return null;
}

/**
 * sourceNameOf resolves the decision's source object to the card name a
 * player would recognise it by. The stack is checked first (a resolving
 * spell or ability is the commonest source and carries its display name
 * even for an ability), then every visible zone card. A source the view
 * does not show — it was answered, exiled face down, or never public —
 * resolves null; the caller omits the fact rather than guessing.
 */
export function sourceNameOf(d: Decision, view: View): string | null {
  if (d.source === undefined || d.source === 0) return null;
  const stack = sourceStackOf(d, view);
  if (stack) return stack.name;
  const card = everyVisibleCard(view.players).find((c) => c.id === d.source);
  return card ? card.name : null;
}

/**
 * sourceCause is the best-effort CAUSE line (brief Job 3): what kind of
 * thing is asking. When the source sits on the stack, its StackView.kind
 * says whether the ask came from a resolving spell, a triggered ability or
 * an activated ability — the one cause fact the wire already carries, with
 * no server change. Any other source (a permanent's ETB ask, a cast-time
 * choice) has no cause fact on the wire and resolves null; the line omits
 * it rather than guessing.
 */
export function sourceCause(d: Decision, view: View): string | null {
  const stack = sourceStackOf(d, view);
  if (!stack) return null;
  switch (stack.kind) {
    case 'spell': return 'a resolving spell';
    case 'trigger': return 'a triggered ability';
    case 'ability': return 'an activated ability';
    default: return null;
  }
}

/** promptContext is the context line's facts: source, cause, answer shape, and whether this seat can decline the effect at resolution. Unknown facts are null. */
export interface PromptContext {
  source: string | null;
  cause: string | null;
  shape: string | null;
  declinable: string | null;
}

export function promptContext(d: Decision, view: View): PromptContext {
  const stack = sourceStackOf(d, view);
  const declinable = d.kind !== 'trigger_optional' && stack?.optional && stack.decider != null && stack.decider === view.viewer
    ? 'you choose whether the effect happens when it resolves' : null;
  return { source: sourceNameOf(d, view), cause: sourceCause(d, view), shape: shapeOf(d), declinable };
}

/** promptContextText renders the context as one line, omitting whichever facts are unknown. Null when all are. */
export function promptContextText(ctx: PromptContext): string | null {
  const parts: string[] = [];
  if (ctx.source !== null) parts.push(ctx.cause !== null ? `From ${ctx.source} (${ctx.cause})` : `From ${ctx.source}`);
  if (ctx.shape !== null) parts.push(ctx.shape);
  if (ctx.declinable !== null) parts.push(ctx.declinable);
  return parts.length === 0 ? null : parts.join(' · ');
}

/**
 * stuckDecision is the empty-answer safety net (the Squadron Hawk
 * fail-to-find soft-lock): a pending decision for this seat with NO options
 * is one the picker UI cannot render — there is nothing to click, and the
 * game is blocked on an answer. The engine resolves every such shape
 * silently since the empty-choose fix, so a server should never hold one;
 * if one ever arrives anyway (an older server, a new engine shape), the
 * caller surfaces it in the Pending tray instead of leaving the seat
 * "waiting" on an invisible question. `answerable` says whether the minimum
 * legal answer (the empty one, Min 0) can be submitted — a decision with no
 * options AND a positive Min is unanswerable outright and can only be
 * named. Null when the decision is renderable (or absent).
 */
export function stuckDecision(d: Decision | null | undefined): { prompt: string; answerable: boolean } | null {
  if (!d || d.options.length > 0) return null;
  return { prompt: d.prompt, answerable: d.min === 0 };
}
