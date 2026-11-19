// Shared wording for the 43-character key fields (command key, route key).

// Character count shown under a key field; '' while the field is empty.
export function keyLengthText(value) {
  const n = String(value ?? '').trim().length;
  if (n === 0) return '';
  return n === 43 ? '43/43 characters' : `${n}/43 characters — a key is exactly 43 characters`;
}

export function keyLengthWrong(value) {
  const n = String(value ?? '').trim().length;
  return n !== 0 && n !== 43;
}

// Stored-key fingerprint line; the iGate WebUI shows the same 6 hex digits.
export function fingerprintText(fingerprint) {
  return fingerprint ? `Saved key fingerprint: ${fingerprint}. The iGate's Remote Management page must show the same.` : '';
}
