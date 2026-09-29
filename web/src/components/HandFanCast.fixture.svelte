<script lang="ts">
  import type { CardView, Decision, PaymentAction, PlayerView } from '../protocol';
  import { castableActions } from '../lib/announcepay';
  import HandFan from './HandFan.svelte';

  /**
   * HandFanCast fixture mounts HandFan exactly as Table.svelte wires it
   * (castableActions(active, autoManaAvailable, autoPayMana) + onCastPayment),
   * so a mounted test can prove the CAST affordance the fan renders. `case`
   * picks the seat's Auto-pay preference over ONE decision whose only cast is
   * a plan-less ManaBrew payment action:
   *
   *  - 'autopay-off' — the reported seat: the plan-less action announces, so
   *    the fan must show a live CAST button and the click must reach
   *    onCastPayment.
   *  - 'autopay-on' — castAction has no route for a plan-less action, so the
   *    fan must render NO CAST button (a rendered-but-inert control is worse
   *    than none).
   */

  const cast: unknown[] = [];
  (window as unknown as { __casts: () => unknown[] }).__casts = () => JSON.parse(JSON.stringify(cast)) as unknown[];

  const planless: PaymentAction = {
    id: 'pay-4ad785b7', cast: { object: 54, face: 0, origin: 'hand' }, label: 'Cast Aether Vial', plans: [],
  };
  const decision: Decision = {
    seq: 109, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
    options: [{ index: 0, player: 0, kind: 'activate', label: 'Activate Rishadan Port for mana', obj: 15 }],
    payment_actions: [planless],
  };

  const autoPay = new URLSearchParams(window.location.search).get('case') === 'autopay-on';
  const card = (id: number, name: string, types: string): CardView => ({
    id, name, types, printing: { name }, token: '', tapped: false, power: 0, toughness: 0,
    damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
  });
  const player: PlayerView = {
    seat: 0, name: 'Ari', life: 20, lost: false, library_size: 40, hand_size: 1, graveyard_size: 0,
    completed_dungeons: 0,
    hand: [card(54, 'Aether Vial', 'Artifact')], battlefield: [card(15, 'Rishadan Port', 'Land')],
    graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
  };
</script>

<HandFan {player} width={900} paymentActions={castableActions(decision, true, autoPay)} {autoPay} onCastPayment={(action) => cast.push(action.id)} />
