<script lang="ts">
  import type { CardView, View } from '../protocol';
  import type { DvrState } from '../lib/dvr';
  import type { AutoPassLog } from '../lib/autolog';
  import { buildCardOwnerColour } from '../lib/logrender';
  import Transcript from './Transcript.svelte';

  /**
   * Transcript fixture: a real browser-mounted transcript for the card-name
   * hover preview (fb-20260927T154603Z). It mounts the production component
   * with the SAME two props Table.svelte wires — the flattened view cards and
   * the owner-colour resolver built over them — so the Playwright test
   * exercises the pointer-dwell path through Document-level hit-testing and
   * the portalled panel, not a hand-rolled imitation.
   *
   * The visible set is Jace (#4) and one Lightning Bolt (#31). The second log
   * line names Lightning Bolt #30 — the NAME is in the view's key set (so the
   * parser still renders a card span with its title) but that object id is
   * absent, which is the honest degrade case: the span keeps the title and
   * opens no panel.
   */
  const jace = {
    id: 4, name: 'Jace, the Mind Sculptor', types: 'Legendary Planeswalker Jace',
    mana_cost: '2 U U', tapped: false, power: 0, toughness: 0, damage: 0,
    attacking: false, controller: 0, owner: 0, summon_sick: false,
    printing: { name: 'Jace, the Mind Sculptor' }, token: '',
  } as CardView;
  const bolt = {
    id: 31, name: 'Lightning Bolt', types: 'Instant', mana_cost: 'R',
    tapped: false, power: 0, toughness: 0, damage: 0, attacking: false,
    controller: 1, owner: 1, summon_sick: false,
    printing: { name: 'Lightning Bolt' }, token: '',
  } as CardView;

  const cards: CardView[] = [jace, bolt];
  const cardColour = buildCardOwnerColour(cards, (owner) => (owner === 0 ? '#e5484d' : '#3b82f6'));
  const identities = [
    { name: 'Ann', colour: '#e5484d' },
    { name: 'Bob', colour: '#3b82f6' },
  ];
  const notes: AutoPassLog[] = [{
    id: 1, turn: 2, text: 'Auto-passed: your main 1',
    diagnostics: {
      verdict: 'pass', optionKinds: ['cast'], actionableOptions: [], castableAfterTap: [],
      respondableOption: false, respondableAfterTap: false, yieldsHit: false,
      view: { snapshot_marker: 'seat-redacted-view' } as unknown as View,
    },
  }];

  const dvr: DvrState = {
    match: 'm1', head: 2, cursor: 2, live: true, turnStarts: [], gap: false,
    events: [
      { event: { seq: 1, kind: 'put_on_stack', player: 0 }, line: 'Ann casts Jace, the Mind Sculptor #4' },
      { event: { seq: 2, kind: 'put_on_stack', player: 1 }, line: 'Bob casts Lightning Bolt #30' },
    ],
  };
</script>

<Transcript {dvr} {cards} {cardColour} {identities} {notes} onSeek={() => {}} />
