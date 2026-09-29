import type { ZoneRef } from '../clientmodel/types';
import { objSelectors, pick, recipientSelectors, VIEWER_HAND, zoneSelectors, type Box } from './anchors';
import type { Anchor, BatchPlan, MotionStep } from './plan';
import type { Running } from './queue';

/**
 * dom plays a BatchPlan with the Web Animations API. It works in two halves
 * because the board re-renders between them:
 *
 * - `capture` runs when the step is emitted, while the OLD board is still
 *   painted: it records every flight's source box (and clones the source
 *   card, so a card that has since left the DOM still flies as itself), and
 *   the pre-change box of every float/shake recipient (a creature that died
 *   in the same batch still gets its damage number where it stood).
 * - `play` runs once the new board is painted: it resolves destinations,
 *   builds flyers in the overlay layer and animates them.
 *
 * Nothing here blocks input. Flyers live in a `pointer-events: none` layer;
 * a destination card is only faded while its flight lands, never removed or
 * made inert, so a click on it still reaches it (and the click's pointerdown
 * finishes the queue first — MotionLayer.svelte).
 */

export interface Captured {
  sources: (({ box: Box; clone: HTMLElement | null }) | null)[];
  targets: (Box | null)[];
}

const CARD_W = 63;
const CARD_H = 88;

function zoneSel(z: ZoneRef, viewerSeat: number | null): string[] {
  const base = zoneSelectors(z);
  return z.zone === 'hand' && z.seat !== null && z.seat === viewerSeat ? [VIEWER_HAND, ...base] : base;
}

function anchorPick(root: ParentNode, a: Anchor, viewerSeat: number | null) {
  return (a.obj !== null ? pick(root, objSelectors(a.obj)) : null) ?? pick(root, zoneSel(a.zone, viewerSeat));
}

function cloneCard(el: Element): HTMLElement {
  const c = el.cloneNode(true) as HTMLElement;
  c.removeAttribute('data-obj');
  for (const n of c.querySelectorAll('[data-obj],[id]')) {
    n.removeAttribute('data-obj');
    n.removeAttribute('id');
  }
  c.removeAttribute('id');
  // Detach it from the layout it was measured in: a hand card is absolutely
  // placed along the fan, a tapped card rotated. The flyer box sizes it.
  Object.assign(c.style, { position: 'absolute', left: '0', top: '0', right: 'auto', bottom: 'auto', margin: '0', width: '100%', height: '100%', transform: 'none', translate: 'none', rotate: 'none', scale: 'none' });
  c.setAttribute('aria-hidden', 'true');
  c.setAttribute('inert', '');
  return c;
}

export function capture(plan: BatchPlan, viewerSeat: number | null, root: ParentNode = document): Captured {
  const sources: Captured['sources'] = [];
  const targets: Captured['targets'] = [];
  for (const s of plan.steps) {
    if (s.kind === 'flight') {
      const obj = s.from.obj !== null ? pick(root, objSelectors(s.from.obj)) : null;
      if (obj) {
        sources.push({ box: obj.box, clone: s.count === 1 ? cloneCard(obj.el) : null });
      } else {
        const z = pick(root, zoneSel(s.from.zone, viewerSeat));
        sources.push(z ? { box: z.box, clone: null } : null);
      }
      targets.push(null);
    } else if (s.kind === 'float' || s.kind === 'shake') {
      sources.push(null);
      targets.push(pick(root, recipientSelectors(s.at, s.kind === 'float' && s.life))?.box ?? null);
    } else {
      sources.push(null);
      targets.push(null);
    }
  }
  return { sources, targets };
}

const EASE = 'cubic-bezier(.3,.7,.3,1)';

function centre(b: Box) {
  return { x: b.left + b.width / 2, y: b.top + b.height / 2 };
}

/** play starts every step of the batch in `layer` and returns the handle that finishes it early. */
export function play(plan: BatchPlan, cap: Captured, layer: HTMLElement, viewerSeat: number | null, root: ParentNode = document): Running {
  const anims: Animation[] = [];
  const nodes: HTMLElement[] = [];
  const add = (el: HTMLElement) => {
    layer.appendChild(el);
    nodes.push(el);
    return el;
  };
  const animate = (el: Element, frames: Keyframe[], opts: KeyframeAnimationOptions) => {
    if (typeof (el as HTMLElement).animate !== 'function') return null;
    const a = (el as HTMLElement).animate(frames, opts);
    anims.push(a);
    return a;
  };

  plan.steps.forEach((s, i) => {
    try {
      step(s, i);
    } catch {
      // One unresolvable step never stops the rest of the batch.
    }
  });

  function step(s: MotionStep, i: number) {
    switch (s.kind) {
      case 'flight': {
        const src = cap.sources[i];
        const dst = anchorPick(root, s.to, viewerSeat);
        if (!src || !dst) return;
        const w = src.clone ? src.box.width : s.to.obj !== null && dst ? Math.min(dst.box.width, CARD_W * 1.4) : CARD_W;
        const h = src.clone ? src.box.height : s.to.obj !== null && dst ? Math.min(dst.box.height, CARD_H * 1.4) : CARD_H;
        const a = centre(src.box);
        const b = centre(dst.box);
        const f = add(document.createElement('div'));
        f.className = 'motion-flyer';
        if (src.clone) f.appendChild(src.clone);
        else f.classList.add('motion-flyer--back');
        if (s.count > 1) {
          const n = document.createElement('span');
          n.className = 'motion-flyer__count';
          n.textContent = `×${s.count}`;
          f.appendChild(n);
        }
        Object.assign(f.style, { left: `${a.x - w / 2}px`, top: `${a.y - h / 2}px`, width: `${w}px`, height: `${h}px`, opacity: '0' });
        const scale = Math.max(0.3, Math.min(1.6, Math.min(dst.box.width / w, dst.box.height / h)));
        const tx = b.x - a.x;
        const ty = b.y - a.y;
        const mid = `translate(${tx * 0.5}px, ${ty * 0.5 - 36}px) scale(${(1 + scale) / 2 + 0.06})`;
        const anim = animate(f, [
          { transform: 'none', opacity: 1 },
          { transform: mid, opacity: 1, offset: 0.55 },
          { transform: `translate(${tx}px, ${ty}px) scale(${scale})`, opacity: 0.85 },
        ], { duration: s.duration, delay: s.delay, easing: EASE, fill: 'both' });
        if (anim) anim.onfinish = () => f.remove();
        // The real card waits under the flight, still clickable.
        if (s.to.obj !== null && s.count === 1) {
          const real = pick(root, objSelectors(s.to.obj));
          if (real) animate(real.el, [{ opacity: 0 }, { opacity: 0, offset: 0.97 }, { opacity: 1 }], { duration: s.delay + s.duration, easing: 'linear' });
        }
        return;
      }
      case 'float': {
        const box = (pick(root, recipientSelectors(s.at, s.life))?.box) ?? cap.targets[i];
        if (!box) return;
        const c = centre(box);
        const f = add(document.createElement('div'));
        f.className = `motion-float motion-float--${s.tone}`;
        f.textContent = s.text;
        Object.assign(f.style, { left: `${c.x}px`, top: `${s.life ? box.top + box.height : c.y}px`, opacity: '0' });
        const anim = animate(f, [
          { transform: 'translate(-50%, -50%) scale(.7)', opacity: 0 },
          { transform: 'translate(-50%, -80%) scale(1.1)', opacity: 1, offset: 0.2 },
          { transform: 'translate(-50%, -180%) scale(1)', opacity: 0 },
        ], { duration: s.duration, delay: s.delay, easing: 'ease-out', fill: 'both' });
        if (anim) anim.onfinish = () => f.remove();
        return;
      }
      case 'shake': {
        const el = pick(root, recipientSelectors(s.at, false))?.el;
        if (!el) return;
        animate(el, [
          { translate: '0 0' }, { translate: '-5px 0', offset: 0.2 }, { translate: '5px 0', offset: 0.5 }, { translate: '-3px 0', offset: 0.8 }, { translate: '0 0' },
        ], { duration: s.duration, delay: s.delay, easing: 'ease-out' });
        return;
      }
      case 'bump': {
        const el = pick(root, zoneSel(s.zone, viewerSeat))?.el;
        if (!el) return;
        animate(el, [{ scale: '1' }, { scale: '1.18', offset: 0.4 }, { scale: '1' }], { duration: s.duration, delay: s.delay, easing: 'ease-out' });
        return;
      }
      case 'appear': {
        const el = pick(root, objSelectors(s.obj))?.el;
        if (!el) return;
        animate(el, [{ opacity: 0, scale: '0.85' }, { opacity: 1, scale: '1' }], { duration: s.duration, delay: s.delay, easing: EASE, fill: 'backwards' });
        return;
      }
    }
  }

  return {
    finish() {
      for (const a of anims) {
        try {
          a.finish();
        } catch {
          // An animation with an infinite or cancelled timeline: cancel instead.
          a.cancel();
        }
      }
      for (const n of nodes) n.remove();
    },
  };
}
