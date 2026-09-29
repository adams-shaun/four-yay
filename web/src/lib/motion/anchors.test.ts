import { describe, expect, it } from 'vitest';
import { lifeSelectors, recipientSelectors, zoneSelectors } from './anchors';

describe('motion anchors', () => {
  it('the anchor attribute comes first, then the fallbacks', () => {
    const g = zoneSelectors({ seat: 1, zone: 'graveyard' });
    expect(g[0]).toBe('[data-motion-anchor="1:graveyard"]');
    expect(g).toContain('[data-seat="1"] [data-pile="graveyard"]');
    expect(g).toContain('[data-seat-row="1"] [data-stat="graveyard"]');
    expect(zoneSelectors({ seat: 1, zone: 'hand' })[1]).toBe('[data-seat-row="1"] [data-stat="hand"]');
    expect(g.at(-1)).toBe('[data-seat="1"]');
  });

  it('the stack is shared', () => {
    expect(zoneSelectors({ seat: 0, zone: 'stack' })).toEqual(['[data-motion-anchor="stack"]', '.rail section.stack', 'aside.rail']);
  });

  it('life and recipients', () => {
    expect(lifeSelectors(2)[0]).toBe('[data-motion-anchor="2:life"]');
    expect(lifeSelectors(2)).toContain('[data-seat-anchor="2"]');
    expect(recipientSelectors({ obj: 7 }, false)).toEqual(['[data-obj="7"]']);
    expect(recipientSelectors({ seat: 0 }, true)).toEqual(lifeSelectors(0));
  });
});
