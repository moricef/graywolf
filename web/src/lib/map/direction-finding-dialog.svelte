<script>
  import { Button } from '@chrissnell/chonky-ui';
  import Modal from '../../components/Modal.svelte';
  import { fmtLat, fmtLon } from './popup-helpers.js';

  let {
    open = $bindable(false),
    station = null,
    onConfirm = undefined,
  } = $props();

  let mission = $state('');
  let bearingDeg = $state(undefined);
  let uncertaintyDeg = $state(10);
  let frequencyMHz = $state(121.5);
  let observedAtLocal = $state('');
  let note = $state('');
  let error = $state('');

  function nowForLocalInput() {
    const now = new Date();
    const local = new Date(now.getTime() - now.getTimezoneOffset() * 60_000);
    return local.toISOString().slice(0, 16);
  }

  $effect(() => {
    if (open) {
      mission = '';
      bearingDeg = undefined;
      uncertaintyDeg = 10;
      frequencyMHz = 121.5;
      observedAtLocal = nowForLocalInput();
      note = '';
      error = '';
    }
  });

  function confirm() {
    const bearing = Number(bearingDeg);
    const uncertainty = Number(uncertaintyDeg);
    const frequency = Number(frequencyMHz);
    const observedAt = new Date(observedAtLocal);
    if (!station?.callsign || !Number.isFinite(station.lat) || !Number.isFinite(station.lon)) {
      error = 'The reporting station has no usable position.';
      return;
    }
    if (bearingDeg == null || bearingDeg === '' || !Number.isFinite(bearing) || bearing < 0 || bearing >= 360) {
      error = 'Bearing must be between 0 and less than 360 degrees.';
      return;
    }
    if (!Number.isFinite(uncertainty) || uncertainty < 0 || uncertainty > 90) {
      error = 'Uncertainty must be between 0 and 90 degrees.';
      return;
    }
    if (!Number.isFinite(frequency) || frequency <= 0) {
      error = 'Frequency must be greater than zero.';
      return;
    }
    if (Number.isNaN(observedAt.getTime())) {
      error = 'Observation time is required.';
      return;
    }
    error = '';
    onConfirm?.({
      callsign: station.callsign,
      lat: station.lat,
      lon: station.lon,
      mission,
      bearingDeg: bearing,
      uncertaintyDeg: uncertainty,
      frequencyMHz: frequency,
      observedAt: observedAt.toISOString(),
      note,
    });
    open = false;
  }
</script>

<Modal bind:open title="Add direction finding">
  <div class="df-form">
    <div class="df-origin">
      <strong>{station?.callsign || 'Unknown station'}</strong>
      {#if Number.isFinite(station?.lat) && Number.isFinite(station?.lon)}
        <span>{fmtLat(station.lat)} {fmtLon(station.lon)}</span>
      {/if}
    </div>

    <label class="df-field">
      <span>Mission / search</span>
      <input type="text" bind:value={mission} maxlength="80" placeholder="e.g. SATER-01" />
    </label>

    <div class="df-grid">
      <label class="df-field">
        <span>True bearing (°)</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input type="number" min="0" max="359.9" step="0.1" bind:value={bearingDeg} autofocus />
      </label>
      <label class="df-field">
        <span>Uncertainty (±°)</span>
        <input type="number" min="0" max="90" step="0.5" bind:value={uncertaintyDeg} />
      </label>
      <label class="df-field">
        <span>Frequency (MHz)</span>
        <input type="number" min="0.001" step="0.001" bind:value={frequencyMHz} />
      </label>
    </div>

    <div class="df-help">Bearing is measured clockwise from true north, in the direction of the transmitter.</div>

    <label class="df-field">
      <span>Observed at</span>
      <input type="datetime-local" bind:value={observedAtLocal} />
    </label>

    <label class="df-field">
      <span>Note</span>
      <textarea bind:value={note} maxlength="500" rows="3" placeholder="Signal quality, reflections, terrain…"></textarea>
    </label>

    {#if error}<div class="df-error" role="alert">{error}</div>{/if}

    <div class="df-actions">
      <Button onclick={() => (open = false)}>Cancel</Button>
      <Button variant="primary" onclick={confirm}>Plot bearing</Button>
    </div>
  </div>
</Modal>

<style>
  .df-form { display: flex; min-width: 360px; flex-direction: column; gap: 14px; }
  .df-origin { display: flex; align-items: baseline; justify-content: space-between; gap: 16px; font-family: var(--font-mono); }
  .df-origin strong { color: #d4a040; }
  .df-origin span { color: var(--color-text-muted); font-size: 12px; }
  .df-field { display: flex; flex-direction: column; gap: 6px; }
  .df-field > span { color: var(--color-text-muted); font-size: 11px; letter-spacing: 1px; text-transform: uppercase; }
  .df-field input, .df-field textarea {
    box-sizing: border-box;
    width: 100%;
    padding: 8px 10px;
    border: 1px solid var(--color-border);
    border-radius: 4px;
    background: var(--color-surface);
    color: var(--color-text);
    font: 14px var(--font-mono);
  }
  .df-field input:focus, .df-field textarea:focus { border-color: var(--color-primary, #4a9eff); outline: none; }
  .df-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
  .df-help { color: var(--color-text-muted); font-size: 12px; }
  .df-error { color: var(--color-danger, #ff6b6b); font-size: 12px; }
  .df-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
  @media (max-width: 520px) {
    .df-form { min-width: 0; }
    .df-grid { grid-template-columns: 1fr; }
  }
</style>
