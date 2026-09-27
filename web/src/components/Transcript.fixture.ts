import { mount } from 'svelte';
import '../app.css';
import TranscriptFixture from './Transcript.fixture.svelte';

mount(TranscriptFixture, { target: document.querySelector('#fixture')! });
