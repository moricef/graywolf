// Unsaved station settings stay in memory only; route keys never enter browser storage.
const drafts = new Map();
export function rememberOtaDraft(target, settings, frequencies) {
  drafts.set(target, { settings: structuredClone(settings), frequencies: { ...frequencies } });
}
export function otaDraft(target) {
  const draft = drafts.get(target);
  return draft ? structuredClone(draft) : null;
}
export function discardOtaDraft(target) { drafts.delete(target); }
export function frequencyHz(text) {
  const value = String(text).trim().replace(',', '.');
  if (!/^\d{3}(?:\.\d{1,6})?$/.test(value)) throw new Error('Enter a frequency in MHz, for example 433.775.');
  return Math.round(Number(value) * 1000000);
}
