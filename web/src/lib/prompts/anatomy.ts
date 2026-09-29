import type { CardView, Decision, View } from '../../protocol';
import { findCardAnywhere } from '../board';
import { sourceStackOf } from '../prompt';
import { arrangeDestination } from '../arrange';
import { mulliganPhase } from './decision';
import { rendererFor, type RendererKind } from './renderer';

/**
 * anatomy.ts is the prompt anatomy's words (UI rework spec §4): the source
 * line, the serif title that asks the question, and the one plain-language
 * line saying what happens. Every fact comes from the wire — the decision's
 * kind/min/max/source/target_effect and the view's stack and cards — so the
 * client derives no rules. The server's own `prompt` text stays the
 * authority where it IS the question (a `choose`, a `replacement`, a kind
 * this module does not know), and is shown as the plain line under a
 * kind-level title everywhere else.
 */

export interface PromptSource {
  /** The source card, when the view shows it (its art is the dock banner). */
  card: CardView | null;
  /** The source line, e.g. "Mira's Rhystic Study · triggered ability". */
  line: string;
}

export interface Anatomy {
  renderer: RendererKind;
  source: PromptSource | null;
  title: string;
  plain: string | null;
}

const WORDS = ['zero', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten'];
/** countWord spells small counts the way card text does ("Choose two"). */
export function countWord(n: number): string {
  return WORDS[n] ?? String(n);
}

/** possessive names a seat as the viewer reads it: "Your" for the viewer, "Mira's" otherwise. */
function owner(view: View, seat: number | undefined | null): string | null {
  if (seat === undefined || seat === null) return null;
  if (seat === view.viewer) return 'Your';
  const name = view.players.find((p) => p.seat === seat)?.name;
  return name ? `${name}'s` : null;
}

/** firstType is a card's leading card type in lower case ("instant", "creature"), for the source line. */
function firstType(card: CardView | null | undefined): string | null {
  if (!card) return null;
  const main = card.types.split(/\s+[—-]\s+/)[0] ?? '';
  const words = main.split(/\s+/).filter((w) => !['Legendary', 'Basic', 'Snow', 'World', 'Tribal', 'Kindred'].includes(w));
  return words.length > 0 ? words[words.length - 1].toLowerCase() : null;
}

/**
 * promptSource resolves the source line and card. A stack source names its
 * controller, its name and what kind of thing is asking ("instant you're
 * casting", "triggered ability", "activated ability"); a board or hand
 * source names its controller and name; an unknown source is null and the
 * line is omitted rather than guessed.
 */
export function promptSource(d: Decision, view: View): PromptSource | null {
  if (d.source === undefined || d.source === 0) return null;
  const stack = sourceStackOf(d, view);
  if (stack) {
    const card = stack.card ?? findCardAnywhere(view, stack.source ?? stack.id);
    const who = owner(view, stack.controller);
    let cause: string | null = null;
    if (stack.kind === 'trigger') cause = 'triggered ability';
    else if (stack.kind === 'ability') cause = 'activated ability';
    else if (stack.kind === 'spell') {
      const t = firstType(stack.card ?? card);
      cause = stack.controller === view.viewer ? `${t ?? 'spell'} you're casting` : t ?? 'spell';
    }
    const name = who ? `${who} ${stack.name}` : stack.name;
    return { card: card ?? null, line: cause ? `${name} · ${cause}` : name };
  }
  const card = findCardAnywhere(view, d.source);
  if (!card) return null;
  const who = owner(view, card.controller);
  return { card, line: who ? `${who} ${card.name}` : card.name };
}

/** countPhrase is "a target", "two targets", "up to two targets" or "one to three targets". */
function countPhrase(d: Decision, noun: string, plural = `${noun}s`): string {
  const unit = (n: number) => (n === 1 ? noun : plural);
  if (d.min === d.max) return d.min === 1 ? `a ${noun}` : `${countWord(d.min)} ${unit(d.min)}`;
  if (d.min === 0) return `up to ${countWord(d.max)} ${unit(d.max)}`;
  return `${countWord(d.min)} to ${countWord(d.max)} ${plural}`;
}

/** effectSentence reads a target decision's host-independent effect (target_effect) as one plain sentence, or null. */
function effectSentence(d: Decision): string | null {
  const e = d.target_effect;
  if (!e) return null;
  const amount = e.damage?.amount;
  if (typeof amount === 'number' && amount > 0) return `Deals ${amount} damage to ${d.max > 1 ? 'the targets' : 'the target'}.`;
  const removal = e.removal?.kind;
  switch (removal) {
    case 'destroy': return 'Destroys the target.';
    case 'exile': return 'Exiles the target.';
    case 'bounce': return "Returns the target to its owner's hand.";
    case 'sacrifice': return 'The target is sacrificed.';
    default: return null;
  }
}

/** sentence tidies server text into one line ending with a full stop. */
function sentence(text: string | null | undefined): string | null {
  const t = (text ?? '').trim();
  if (t === '') return null;
  return /[.?!]$/.test(t) ? t : `${t}.`;
}

/** afterDash is the part of a "question — label" prompt after its em dash (the trigger's own description), or null. */
function afterDash(text: string): string | null {
  const at = text.indexOf(' — ');
  return at < 0 ? null : text.slice(at + 3).trim() || null;
}

/** promptAnatomy is the dock's words for one decision. */
export function promptAnatomy(d: Decision, view: View): Anatomy {
  const renderer = rendererFor(d);
  const source = promptSource(d, view);
  const srcName = source?.card?.name ?? null;
  const stackText = sourceStackOf(d, view)?.text?.trim() || null;
  let title = d.prompt;
  let plain: string | null = null;
  switch (renderer) {
    case 'target': {
      title = `Choose ${countPhrase(d, 'target')}`;
      const effect = effectSentence(d) ?? sentence(stackText);
      plain = [effect, 'Click a highlighted card or player, or pick below.'].filter(Boolean).join(' ');
      break;
    }
    case 'attackers':
      title = 'Declare attackers';
      plain = 'Click your creatures on the board to attack, or pick below.';
      break;
    case 'blockers':
      title = 'Declare blockers';
      plain = 'Click one of your creatures to block with it, or pick a pairing below.';
      break;
    case 'mulligan': {
      const m = mulliganPhase(d);
      if (m?.phase === 'bottom') {
        title = `Put ${d.min === 1 ? 'a card' : `${countWord(d.min)} cards`} on the bottom`;
        plain = 'Pick the cards to put on the bottom of your library.';
      } else {
        title = 'Keep this hand?';
        plain = sentence(d.prompt);
      }
      break;
    }
    case 'arrange': {
      const n = d.options.length;
      const verb = /^\s*scry/i.test(d.prompt) ? 'Scry' : /^\s*surveil/i.test(d.prompt) ? 'Surveil' : null;
      title = verb ? `${verb} ${n}` : `Order the top ${n === 1 ? 'card' : `${countWord(n)} cards`}`;
      plain = verb
        ? `Pick the cards to keep on top, in order; the rest go to ${arrangeDestination(d)}.`
        : 'The first card you pick goes on top.';
      break;
    }
    case 'discard':
      title = `Choose ${countPhrase(d, 'card')} to discard`;
      plain = sentence(d.prompt);
      break;
    case 'search':
      title = 'Search your library';
      plain = `Choose ${countPhrase(d, 'card')}. Type to filter.`;
      break;
    case 'name':
      title = 'Name a card';
      plain = 'Type to filter the names.';
      break;
    case 'payment':
      title = srcName ? `Pay for ${srcName}` : 'Pay the mana cost';
      plain = 'Tap sources on the board or below, or let Auto-fill pick them.';
      break;
    case 'priority':
      plain = null;
      break;
    case 'list':
      switch (d.kind) {
        case 'modes':
          title = d.min === d.max ? `Choose ${countWord(d.min)}` : d.min === 0 ? `Choose up to ${countWord(d.max)}` : `Choose ${countWord(d.min)} to ${countWord(d.max)}`;
          plain = d.repeatable
            ? 'You may choose the same mode more than once.'
            : d.min === d.max && d.min > 1 ? `Pick exactly ${countWord(d.min)} different modes.` : sentence(stackText);
          break;
        case 'trigger_order':
          title = 'Order your triggers';
          plain = 'The one you pick first goes on the stack first, so it resolves last.';
          break;
        case 'trigger_optional':
          title = srcName ? `Use ${srcName}'s ability?` : 'Use this optional ability?';
          plain = sentence(afterDash(d.prompt)) ?? sentence(d.prompt);
          break;
        case 'commander_zone':
          title = srcName ? `Return ${srcName} to the command zone?` : 'Return to the command zone?';
          plain = sentence(d.prompt);
          break;
        case 'starting_player':
          title = 'Who plays first?';
          plain = 'You won the toss.';
          break;
        case 'replacement':
          title = 'Choose which applies first';
          plain = sentence(d.prompt);
          break;
        default:
          // choose and any kind this module does not know: the server's
          // prompt IS the question.
          title = d.prompt;
          plain = d.max > 1 || d.min !== 1 ? `Choose ${countPhrase(d, 'option')}.` : null;
      }
      break;
  }
  return { renderer, source, title, plain: plain === title ? null : plain };
}

/**
 * selectionStatus is the footer's live count for a multi-pick decision
 * ("2 of 2 chosen"); null for a single-click ask, where the click is the
 * answer and a count would say nothing.
 */
export function selectionStatus(d: Decision, picked: number): string | null {
  if (d.min === 1 && d.max === 1) return null;
  if (d.min === d.max) return `${picked} of ${d.max} chosen`;
  if (d.min === 0) return `${picked} chosen · up to ${d.max}`;
  return `${picked} chosen · ${d.min}–${d.max}`;
}

/** submitLabel is the commit button's words for a multi-pick decision, by kind. */
export function submitLabel(d: Decision): string {
  switch (d.kind) {
    case 'attackers': return 'Confirm attackers';
    case 'blockers': return 'Confirm blocks';
    case 'target': return d.max > 1 ? 'Choose targets' : 'Choose target';
    case 'trigger_order': return 'Confirm order';
    case 'arrange': return d.min === d.max ? 'Confirm order' : 'Confirm';
    default:
      return d.min === 0 ? 'Confirm' : d.min === d.max ? `Choose ${d.min}` : `Choose ${d.min}–${d.max}`;
  }
}
