<script>
  import LocateFixed from 'lucide-svelte/icons/locate-fixed';
  import Minus from 'lucide-svelte/icons/minus';
  import Square from 'lucide-svelte/icons/square';
  import X from 'lucide-svelte/icons/x';
  import { renderStationPopupHTML } from './popup.js';
  import { unitsState } from '../settings/units-store.svelte.js';

  let {
    windows = [],
    stations,
    nowMs = 0,
    onclose,
    ontoggle,
    onfront,
    onmove,
    onlocate,
    onbearing,
  } = $props();

  let layer = $state();
  let drag = null;

  function currentStation(card) {
    return stations?.get(card.callsign) || card.snapshot;
  }

  function cardHTML(card, tick, isMetric) {
    void tick;
    const station = currentStation(card);
    if (!station) return '';
    return renderStationPopupHTML(station, {
      hasStation: (callsign) => stations?.has(callsign) || false,
      showHeader: false,
      isMetric,
    });
  }

  function startDrag(event, card) {
    if (event.button !== 0 || event.target.closest('button')) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    drag = {
      callsign: card.callsign,
      startClientX: event.clientX,
      startClientY: event.clientY,
      startX: card.x,
      startY: card.y,
    };
    onfront?.(card.callsign);
    event.preventDefault();
  }

  function dragWindow(event) {
    if (!drag) return;
    const bounds = layer?.getBoundingClientRect();
    onmove?.(
      drag.callsign,
      drag.startX + event.clientX - drag.startClientX,
      drag.startY + event.clientY - drag.startClientY,
      { width: bounds?.width, height: bounds?.height }
    );
  }

  function stopDrag() {
    drag = null;
  }

  function handleBodyClick(event, callsign) {
    onfront?.(callsign);
    const bearing = event.target?.closest?.('.stn-bearing-button');
    if (bearing) {
      event.preventDefault();
      onbearing?.(currentStation(windows.find((card) => card.callsign === callsign)));
      return;
    }
    const link = event.target?.closest?.('.path-link');
    if (!link?.dataset.callsign) return;
    event.preventDefault();
    onlocate?.(link.dataset.callsign);
  }

  function handleBodyKeydown(event, callsign) {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    handleBodyClick(event, callsign);
  }

</script>

{#if windows.length}
  <div class="station-window-layer" bind:this={layer} aria-label="Pinned station windows">
    {#each windows as card (card.callsign)}
      <section
        class:minimized={card.minimized}
        class="station-window"
        style:left={`${card.x}px`}
        style:top={`${card.y}px`}
        style:z-index={card.z}
      >
        <header
          class="station-window-header"
          role="toolbar"
          tabindex="-1"
          aria-label={`Controls for ${card.callsign}`}
          onpointerdown={(event) => startDrag(event, card)}
          onpointermove={dragWindow}
          onpointerup={stopDrag}
          onpointercancel={stopDrag}
        >
          <span class="station-window-title">{card.callsign}</span>
          <span class="station-window-controls">
            <button type="button" title="Center map on station" aria-label={`Center map on ${card.callsign}`} onclick={() => onlocate?.(card.callsign)}>
              <LocateFixed size={15} aria-hidden="true" />
            </button>
            <button type="button" title={card.minimized ? 'Restore' : 'Minimize'} aria-label={card.minimized ? `Restore ${card.callsign}` : `Minimize ${card.callsign}`} onclick={() => ontoggle?.(card.callsign)}>
              {#if card.minimized}<Square size={13} aria-hidden="true" />{:else}<Minus size={15} aria-hidden="true" />{/if}
            </button>
            <button type="button" title="Close" aria-label={`Close ${card.callsign}`} onclick={() => onclose?.(card.callsign)}>
              <X size={16} aria-hidden="true" />
            </button>
          </span>
        </header>
        {#if !card.minimized}
          <!-- Delegation is only for the keyboard-accessible .path-link anchors inside {@html}. -->
          <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
          <div
            class="station-window-body"
            role="group"
            aria-label={`Details for ${card.callsign}`}
            onclick={(event) => handleBodyClick(event, card.callsign)}
            onkeydown={(event) => handleBodyKeydown(event, card.callsign)}
          >
            {@html cardHTML(card, nowMs, unitsState.isMetric)}
          </div>
        {/if}
      </section>
    {/each}
  </div>
{/if}

<style>
  .station-window-layer {
    position: absolute;
    inset: 0;
    z-index: 70;
    overflow: hidden;
    pointer-events: none;
  }

  .station-window {
    position: absolute;
    width: 360px;
    max-width: calc(100% - 24px);
    overflow: hidden;
    border: 1px solid var(--map-overlay-border);
    border-radius: 9px;
    background: var(--map-overlay-bg);
    -webkit-backdrop-filter: blur(var(--map-overlay-blur, 0));
    backdrop-filter: blur(var(--map-overlay-blur, 0));
    box-shadow: var(--map-overlay-shadow);
    color: var(--map-overlay-fg);
    pointer-events: auto;
  }

  .station-window-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 34px;
    padding: 0 5px 0 10px;
    border-bottom: 1px solid var(--map-overlay-border);
    background: color-mix(in srgb, var(--map-overlay-bg) 82%, var(--color-accent) 18%);
    cursor: move;
    touch-action: none;
    user-select: none;
  }

  .station-window-title {
    overflow: hidden;
    color: #d4a040;
    font-family: var(--font-mono);
    font-size: 13px;
    font-weight: 700;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .station-window-controls {
    display: flex;
    gap: 1px;
  }

  .station-window-controls button {
    display: grid;
    width: 27px;
    height: 27px;
    padding: 0;
    place-items: center;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--map-overlay-muted);
    cursor: pointer;
  }

  .station-window-controls button:hover,
  .station-window-controls button:focus-visible {
    background: color-mix(in srgb, var(--map-overlay-fg) 10%, transparent);
    color: var(--map-overlay-fg);
    outline: none;
  }

  .station-window-body {
    max-height: min(620px, calc(100vh - 120px));
    overflow: auto;
    padding: 10px;
  }

  .station-window.minimized {
    width: 230px;
  }

  .station-window.minimized .station-window-header {
    border-bottom: 0;
  }

  @media (max-width: 768px), (orientation: landscape) and (max-height: 500px) {
    .station-window-layer {
      display: flex;
      align-items: flex-end;
      gap: 8px;
      padding: 12px 12px calc(58px + env(safe-area-inset-bottom, 0px));
      overflow-x: auto;
      overflow-y: hidden;
      scroll-snap-type: x proximity;
    }

    .station-window {
      position: relative !important;
      top: auto !important;
      left: auto !important;
      flex: 0 0 min(360px, calc(100vw - 24px));
      max-height: 62vh;
      scroll-snap-align: center;
    }

    .station-window-header {
      cursor: default;
    }

    .station-window-body {
      max-height: calc(62vh - 36px);
    }
  }
</style>
