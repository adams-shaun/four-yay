import { mount } from 'svelte';
import '../app.css';
import AnnouncePayFixture from './AnnouncePay.fixture.svelte';

// The whole fixture is the Svelte component: one real SeatPanelState with
// Auto-pay off on an auto-mana table. See AnnouncePay.fixture.svelte.
mount(AnnouncePayFixture, { target: document.getElementById('fixture')! });
