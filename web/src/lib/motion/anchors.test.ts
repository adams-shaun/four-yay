import { describe, expect, it } from 'vitest';
import { lifeSelectors, recipientSelectors, zoneSelectors } from './anchors';

describe('motion anchors', () => {
  it('the anchor attribute comes first, then the fallbacks', () => {
    const g = zoneSelectors({ seat: 1, zone: 'graveyard' });
    expect(g[0]).toBe('[data-motion-anchor="1:graveyard"]');
    expect(g).toContain('[data-player-pill="1"] [data-pile="graveyard"]');
    expect(g.at(-1)).toBe('[data-seat="1"]');
  });

  it('the stack is shared', () => {
    expect(zoneSelectors({ seat: 0, zone: 'stack' })).toEqual(['[data-motion-anchor="stack"]', '.rail section.stack', 'aside.rail']);
  });

  it('life and recipients', () => {
    expect(lifeSelectors(2)[0]).toBe('[data-motion-anchor="2:life"]');
    expect(recipientSelectors({ obj: 7 }, false)).toEqual(['[data-obj="7"]']);
    expect(recipientSelectors({ seat: 0 }, true)).toEqual(lifeSelectors(0));
  });
});
