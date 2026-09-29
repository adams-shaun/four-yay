import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { Decision, PaymentAction, View } from '../../protocol';
import { SeatPanelState } from '../../lib/seatpanel.svelte';
import PriorityOptions from './PriorityOptions.svelte';

const { postIntentMock } = vi.hoisted(() => ({ postIntentMock: vi.fn() }));
vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  postIntent: postIntentMock,
}));

const planless: PaymentAction = {
  id: 'planless', cast: { object: 54, face: 0, origin: 'hand' }, label: 'Cast Aether Vial', plans: [],
};
const decision: Decision = {
  seq: 109, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
  options: [{ index: 0, player: 0, kind: 'activate', label: 'Activate Rishadan Port for mana', obj: 15 }],
  payment_actions: [planless],
};
const view = { viewer: 0, players: [], decision } as unknown as View;
const ctx = { seat: 0, token: 'tok' };

describe('PriorityOptions ManaBrew cast', () => {
  it('renders the plan-less cast and routes it to announce-then-pay', async () => {
    postIntentMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
    expect(decision.payment_actions?.[0].plans).toEqual([]);
    const logic = new SeatPanelState('table', 1, ctx, null);
    logic.setAutoManaAvailable(true);
    logic.adoptView(decision);
    const { body } = render(PriorityOptions, { props: { decision, view, logic, seat: 0, placement: 'board' } });
    expect(body).toContain('data-announce="planless"');
    expect(body).toContain('Cast Aether Vial');
    expect(postIntentMock).not.toHaveBeenCalled();

    // The rendered data-announce button's handler is submitAnnounce(action).
    logic.submitAnnounce(planless);
    await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(postIntentMock.mock.calls[0][2]).toEqual({ seq: 109, player: 0, choices: [], announce: { action_id: 'planless' } });
  });
});
