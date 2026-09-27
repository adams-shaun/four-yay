import { mount } from 'svelte';
import '../app.css';
import AutoPayActPassFixture from './AutoPayActPass.fixture.svelte';

// The whole fixture is the Svelte component: one real SeatPanelState with
// pass-after-acting and auto-pay on, and the entry point named by ?surface=.
// See AutoPayActPass.fixture.svelte for the body.
mount(AutoPayActPassFixture, { target: document.getElementById('fixture')! });
