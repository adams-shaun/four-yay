<script lang="ts">
  import { tick } from 'svelte';
  import type { CardView } from '../protocol';
  import { CardHover } from '../lib/carddetail.svelte';
  import type { CardOptions } from '../lib/cardoptions';
  import { ACTION_GLYPHS, actionAccessibleLabel, laterLabel, postSingleAction, singleActionIcon, tileScenario, tileOptions } from '../lib/cardoptions';
  import CardDetail from './CardDetail.svelte';
  import CardImage from './CardImage.svelte';

  /**
   * PileModal is the single card-list surface for hand, graveyard and exile.
   * It is portalled to body so the rail's capped, scrolling regions cannot
   * clip it. The wire never exposes library cards, so library never reaches
   * this component. Cards render as real card faces (CardImage), the same
   * face a board tile shows, in a wrapping grid — not a text index; a pile
   * is cards, not a table of contents.
   *
   * Piles are also ACTIONABLE (fb-20260916T225802Z): with the table's
   * card-options bundle (options), a pile card the pending decision offers
   * something to wears the tone ring and carries the HandFan affordance —
   * one offer is a direct action icon, several a badge + menu, every item
   * posting the option's own wire index (R-E4-1). A target option on a card
   * in ANY seat's pile glows there too (not gated by pile owner); the
   * engine validates every posted option, so a stale highlight can never do
   * damage. Without options (spectator, nothing pending) the pile is the
   * reading surface it always was.
   *
   * Each card in the list carries the shared hover inspector (the SeatPanel
   * arrange-strip pattern): one CardHover for the whole list, pointer dwell
   * (250 ms) or keyboard focus opens the portalled CardDetail, leave/blur
   * close it, and a card leaving the pile closes its panel through supervise
   * — a card can leave the graveyard as play continues while the modal sits
   * open, and a removed element never fires pointerleave. CardDetail portals
   * itself to <body>, so it is a sibling of this modal's backdrop, not a
   * descendant of it; CardDetail's z-index (1001) sits above the backdrop's
   * 1000 so the panel reads over the dim rather than under it.
   */
  let { open, title, cards, returnFocus = null, onClose, options = null }: {
    open: boolean;
    title: string;
    cards: CardView[];
    returnFocus?: HTMLElement | null;
    onClose: () => void;
    /** the table's pending-decision card-options bundle (PileHost forwards
     *  Table.svelte's boardOptions). A pile card the decision offers
     *  something to — a flashback/escape cast on a graveyard card, a
     *  warp-recast on an exile card, a target on a card in ANY seat's pile
     *  (not gated by pile owner; the engine validates every posted option) —
     *  wears the tone ring and carries the HandFan options affordance:
     *  one offer is a direct action icon, several a badge + menu, each item
     *  posting the option's OWN wire index (R-E4-1 — never a rebuilt
     *  position). Null for a spectator / nothing pending: the pile stays a
     *  reading surface. */
    options?: CardOptions | null;
  } = $props();

  let dialog = $state<HTMLElement | null>(null);
  let wasOpen = false;
  let lastReturnFocus: HTMLElement | null = null;

  // The list's shared hover inspector. One panel serves every card in the
  // pile (CardHover keeps the pointer's card and the focused card separate
  // internally, so the two input owners cannot close each other).
  const hover = new CardHover();

  // The per-card options menu (the HandFan contract): at most one open at
  // a time, keyed by card id. Nothing on the wire drives it.
  let openCard = $state<number | null>(null);
  function toggleCard(id: number) {
    openCard = openCard === id ? null : id;
  }

  $effect(() => {
    if (open) {
      wasOpen = true;
      lastReturnFocus = returnFocus;
      void tick().then(() => dialog?.focus());
    } else if (wasOpen) {
      wasOpen = false;
      // The modal is gone; a detail panel must not survive it (its state
      // lives in this component, which stays mounted while the rail does).
      hover.close();
      lastReturnFocus?.focus();
      lastReturnFocus = null;
    }
  });

  // PANEL LIFETIME FOLLOWS THE PILE, NOT THE POINTER (the arrange-strip
  // contract): while the modal sits open, play continues and a card can
  // leave the pile — the row disappears, pointerleave never fires, and the
  // panel would hang open on a card that no longer exists. Whenever the
  // shown card is no longer among the present cards the hover closes.
  $effect(() => {
    hover.supervise(cards);
  });

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

  function backdropClick(event: MouseEvent): void {
    if (event.target === event.currentTarget) onClose();
  }

  /**
   * Escape peels layers from ONE window-level handler, because the panel has
   * two open paths and only one of them leaves the key event on a card
   * button: keyboard focus sits on the focused button, but a pointer dwell
   * leaves focus on the dialog — the event bubbles past the list and this
   * window handler is the only place BOTH paths reach. With the panel open
   * the first Escape closes it (pointer or focus path alike) and the modal
   * stays; the SECOND Escape — with no panel open — closes the modal, exactly
   * the modal's Escape-close as it always was.
   */
  function keydown(event: KeyboardEvent): void {
    if (!open || event.key !== 'Escape') return;
    event.preventDefault();
    if (openCard !== null) {
      openCard = null;
      return;
    }
    if (hover.hover.show) {
      hover.close();
      return;
    }
    onClose();
  }
</script>

<svelte:window onkeydown={keydown} />

{#if open}
  <!-- The backdrop is pointer-dismissable while Escape is handled globally;
       it is not itself a keyboard stop because focus belongs in the dialog.
       data-pile-modal is the modal guard's marker (lib/modals.ts): while this
       modal is open, the document-level hotkeys are suppressed so Space
       cannot post PASS and Enter cannot arm a run underneath it. -->
  <div class="backdrop" data-pile-backdrop data-pile-modal role="presentation" use:portal onclick={backdropClick}>
    <div
      class="dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="pile-modal-title"
      tabindex="-1"
      bind:this={dialog}
    >
      <header>
        <h2 id="pile-modal-title">{title}</h2>
        <button type="button" class="close" aria-label={`Close ${title}`} onclick={onClose}>×</button>
      </header>
      <ul class="cards" data-pile-scroll>
        {#each cards as card (card.id)}
          <!-- A pile card the pending decision offers something to wears the
               tone ring and carries the HandFan options affordance (badge /
               direct icon + menu), adapted to the pile grid: the affordance
               is a SIBLING of the inspector button (a real button is never
               nested inside one), anchored to the card's top edge. Every
               menu item posts the option's own wire index, R-E4-1. -->
          {@const opt = options ? tileOptions(options, card.id) : null}
          {@const later = opt?.later ?? []}
          {@const total = (opt?.list.length ?? 0) + later.length}
          <li data-obj={card.id} class:marked={!!opt}>
            <button
              type="button"
              class="pile-card"
              data-tone={opt?.tone ?? ''}
              onpointerenter={(e) => hover.arm(card, e.currentTarget)}
              onpointerleave={() => hover.leave(card)}
              onfocus={(e) => hover.open(card, e.currentTarget)}
              onblur={() => hover.blur(card)}
              aria-describedby={hover.hover.show && hover.card?.id === card.id ? `card-detail-${card.id}` : undefined}
            >
              <CardImage {card} size="tile" pt={false} />
              <span class="card-name">{card.name}</span>
              <span class="types">{card.types}</span>
            </button>
            {#if opt}
              <div class="tile-actions">
                {#if opt.list.length === 1 && later.length === 0}
                  {@const action = opt.list[0]}
                  {@const icon = singleActionIcon(action)}
                  <button
                    class="action-icon badge--{opt.tone}"
                    type="button"
                    data-single-action
                    data-action-icon={icon}
                    aria-label={actionAccessibleLabel(action)}
                    title={actionAccessibleLabel(action)}
                    onclick={(event) => postSingleAction(opt, false, event.ctrlKey)}
                  >
                    <span aria-hidden="true">{ACTION_GLYPHS[icon]}</span>
                  </button>
                {:else}
                  <button
                    class="badge badge--{opt.tone}"
                    type="button"
                    aria-haspopup="menu"
                    aria-expanded={openCard === card.id}
                    aria-label={tileScenario(opt) && later.length === 0
                      ? `${opt.list.length} ${tileScenario(opt)!.noun} for ${card.name}`
                      : `${total} ${total === 1 ? 'action' : 'actions'} for ${card.name}`}
                    title="Options for {card.name}"
                    data-action-icon={tileScenario(opt)?.icon}
                    onclick={() => toggleCard(card.id)}
                  >
                    {#if tileScenario(opt)}
                      <span class="badge__icon" aria-hidden="true">{ACTION_GLYPHS[tileScenario(opt)!.icon]}</span>
                    {/if}
                    <span class="badge__n data">{total}</span>
                  </button>
                {/if}
                {#if (total > 1 || later.length > 0) && openCard === card.id}
                  <ul class="menu" role="menu" aria-label="Options for {card.name}">
                    {#each opt.list as o (o.index)}
                      <li role="none">
                        <button class="menu__item" type="button" role="menuitem" onclick={(event) => opt.post(o.index, false, event.ctrlKey)}>
                          {o.label}
                        </button>
                      </li>
                    {/each}
                    {#each later as action, i (`${action.ability ?? 0}:${i}`)}
                      <li role="none">
                        <button class="menu__item menu__item--later" type="button" role="menuitem" disabled aria-disabled="true" data-later-ability>
                          {laterLabel(action)}
                        </button>
                      </li>
                    {/each}
                  </ul>
                {/if}
              </div>
            {/if}
          </li>
        {/each}
      </ul>
      {#if hover.hover.show && hover.card && hover.anchor}<CardDetail card={hover.card} anchor={hover.anchor} />{/if}
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 1000;
    display: grid;
    place-items: center;
    padding: min(8vh, 4rem) var(--sp-4);
    background: rgb(5 8 12 / 72%);
  }
  .dialog {
    width: min(48rem, calc(100vw - 2rem));
    max-height: 84vh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    color: var(--ink-inst);
    background: var(--instrument);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    box-shadow: 0 1rem 3rem rgb(0 0 0 / 45%);
    outline: none;
  }
  .dialog:focus-visible {
    border-color: var(--initiative);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-4);
    border-bottom: 1px solid var(--edge-inst);
  }
  h2 {
    margin: 0;
    font-size: var(--t-14);
    font-weight: 600;
    color: var(--ink);
    text-transform: capitalize;
  }
  .close {
    border: 0;
    padding: 0.1rem 0.35rem;
    background: none;
    color: var(--ink-dim);
    font: inherit;
    font-size: 1.35rem;
    line-height: 1;
    cursor: pointer;
  }
  .close:hover {
    color: var(--ink);
  }
  .cards {
    margin: 0;
    padding: var(--sp-3) var(--sp-4) var(--sp-4);
    max-height: 70vh;
    overflow-y: auto;
    list-style: none;
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-3);
    --card-w: var(--card-w-large, 132px);
  }
  li {
    width: var(--card-w);
    position: relative;
  }
  /* A marked card (the decision offers it something) can open a menu that
     must read over its neighbours. */
  li.marked {
    z-index: 1;
  }
  /* The card is a real button so keyboard focus is the native affordance
     (focus opens the detail immediately, blur closes it); the button carries
     the layout the li used to hold. Without options the button's click does
     nothing — the pile list is then a reading surface, so the cursor stays
     default; with options the affordance badge beside it is the actor, and
     the face keeps its inspector role. */
  .pile-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.3rem;
    width: 100%;
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-align: center;
    cursor: default;
  }
  .pile-card:focus-visible {
    outline: var(--edge-w) solid var(--initiative);
    outline-offset: 2px;
  }
  /* The tone ring, the card-tile register (HandFan's face[data-tone]): a
     card the pending decision offers something to. */
  .pile-card[data-tone='initiative'] {
    box-shadow: 0 0 0 2px var(--initiative);
    border-radius: var(--radius-card);
  }
  .pile-card[data-tone='offered'] {
    box-shadow: 0 0 0 2px var(--offered);
    border-radius: var(--radius-card);
  }
  /* The options affordance, anchored to the card's top edge — the HandFan
     badge/menu, adapted to the pile grid (one offer is a direct icon,
     several a count badge + menu; every item posts the option's own index). */
  .tile-actions {
    position: absolute;
    top: 1px;
    right: 1px;
    z-index: 20;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
    line-height: 1;
  }
  .badge,
  .action-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 2px;
    min-width: 1.375rem;
    height: 1.375rem;
    padding: 0 0.25rem;
    border-radius: 3px;
    border: var(--edge-w) solid var(--edge-inst);
    background: var(--instrument);
    color: var(--ink);
    font-family: var(--font-data);
    font-size: var(--t-12);
    font-weight: 600;
    cursor: pointer;
  }
  .badge--initiative {
    background: var(--initiative);
    border-color: var(--initiative);
    color: var(--felt-sunk);
  }
  .badge--offered {
    background: var(--offered);
    border-color: var(--offered);
    color: var(--felt-sunk);
  }
  .action-icon {
    width: 1.6875rem;
    padding: 0;
    font-size: calc(var(--t-14) * 1.25);
    line-height: 1;
  }
  .badge__n {
    font-size: inherit;
  }
  .badge:hover,
  .badge[aria-expanded='true'],
  .action-icon:hover {
    border-color: var(--ink-dim);
    color: var(--ink);
  }
  .badge--initiative:hover,
  .badge--initiative[aria-expanded='true'],
  .action-icon:hover {
    color: var(--felt-sunk);
  }
  /* The menu opens downward over the pile grid (the modal is a centred
     dialog, not a board edge — there is nothing below to leave). */
  .menu {
    position: absolute;
    top: calc(100% + 3px);
    right: 0;
    z-index: 20;
    margin: 0;
    padding: 2px;
    list-style: none;
    min-width: 9rem;
    max-width: 14rem;
    max-height: 12rem;
    overflow-y: auto;
    background: var(--instrument);
    border: var(--edge-w) solid var(--edge-inst);
    border-radius: var(--radius);
    box-shadow: var(--shadow-lift);
  }
  .menu__item {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: 0;
    border-left: 2px solid transparent;
    border-radius: 0;
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    line-height: 1.35;
    padding: var(--sp-1) var(--sp-2);
    cursor: pointer;
  }
  .menu__item:hover,
  .menu__item:focus-visible {
    background: color-mix(in srgb, var(--ink) 7%, var(--instrument));
    border-left-color: var(--ink-dim);
    color: var(--ink);
  }
  .card-name {
    max-width: 100%;
    overflow: hidden;
    color: var(--ink-inst);
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--t-11);
    text-align: center;
  }
  .types {
    max-width: 100%;
    overflow: hidden;
    color: var(--ink-faint);
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--t-10);
    text-align: center;
  }
</style>
