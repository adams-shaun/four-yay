import { mount } from 'svelte';
import '../app.css';
import HandFanCastFixture from './HandFanCast.fixture.svelte';

// The fixture reads ?case itself, so the case is a plain URL value rather than
// a reactive prop referenced in a one-time initializer.
mount(HandFanCastFixture, { target: document.querySelector('#fixture')! });
