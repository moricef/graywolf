// Isolated browser-test fixture. Every API response is intercepted by the test.
import '../src/app.css';
import { mount } from 'svelte';
import RadioOtaPreview from './RadioOtaPreview.svelte';
window.reopenRadioOtaPreview = () => mount(RadioOtaPreview, {target:document.getElementById('app')});
window.reopenRadioOtaPreview();
