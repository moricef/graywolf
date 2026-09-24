const PREFIX = 'graywolf.messages.tx-channel.';

export function getThreadChannel(threadId, storage) {
  if (!threadId) return 0;
  try {
    const source = storage ?? globalThis.localStorage;
    const value = Number(source?.getItem(PREFIX + threadId));
    return Number.isSafeInteger(value) && value > 0 ? value : 0;
  } catch {
    return 0;
  }
}

export function setThreadChannel(threadId, channel, storage) {
  if (!threadId) return;
  try {
    const target = storage ?? globalThis.localStorage;
    if (!target) return;
    if (Number.isSafeInteger(channel) && channel > 0) {
      target.setItem(PREFIX + threadId, String(channel));
    } else {
      target.removeItem(PREFIX + threadId);
    }
  } catch {
    // Storage may be disabled; the in-memory selection still works.
  }
}
