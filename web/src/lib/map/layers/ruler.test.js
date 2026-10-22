import test from 'node:test';
import assert from 'node:assert/strict';
import { mountRulerLayer } from './ruler.js';

test('two clicks complete a measurement; clear and teardown restore map interaction', () => {
  const domListeners = new Map();
  const mapListeners = new Map();
  const layers = new Map();
  let source = null;
  let zoomEnabled = true;
  let completed = 0;
  let measured = null;
  const container = {
    classList: { add() {}, remove() {} },
    addEventListener(name, listener) { domListeners.set(name, listener); },
    removeEventListener(name) { domListeners.delete(name); },
    getBoundingClientRect() { return { left: 0, top: 0 }; },
  };
  const map = {
    style: {},
    getContainer: () => container,
    addSource() { source = { setData() {} }; },
    getSource: () => source,
    removeSource() { source = null; },
    addLayer(layer) { layers.set(layer.id, layer); },
    getLayer: (id) => layers.get(id),
    removeLayer(id) { layers.delete(id); },
    on(name, listener) { mapListeners.set(name, listener); },
    off(name) { mapListeners.delete(name); },
    unproject: ([x, y]) => ({ lng: x, lat: y }),
    doubleClickZoom: {
      isEnabled: () => zoomEnabled,
      enable() { zoomEnabled = true; },
      disable() { zoomEnabled = false; },
    },
  };
  const ruler = mountRulerLayer(map, {
    onMeasure(value) { measured = value; },
    onComplete() { completed += 1; },
  });
  ruler.start();
  assert.equal(zoomEnabled, false);
  function click(x, y) {
    let consumed = false;
    domListeners.get('click')({ clientX: x, clientY: y,
      target: { closest: () => null },
      stopImmediatePropagation() { consumed = true; }, preventDefault() {},
    });
    assert.equal(consumed, true);
  }
  click(0, 0);
  assert.equal(measured, null);
  click(1, 0);
  assert.equal(completed, 1);
  assert.ok(measured.distanceKm > 111 && measured.distanceKm < 112);
  assert.equal(zoomEnabled, true);
  ruler.clear();
  assert.equal(measured, null);
  ruler.start();
  ruler.destroy();
  assert.equal(zoomEnabled, true);
  assert.equal(domListeners.size, 0);
  assert.equal(mapListeners.size, 0);
  assert.equal(layers.size, 0);
  assert.equal(source, null);
});
