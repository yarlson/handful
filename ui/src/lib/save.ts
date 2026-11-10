// What a settings form says after Save or Delete. A form shows its newest result until the
// next one replaces it.
//   null:   nothing was saved or tried yet.
//   saved:  the server took the change; text says so.
//   failed: nothing changed; text says why.
export type SaveResult = { ok: boolean; text: string } | null

// saved is the result of a save that worked.
export function saved(text = "Saved."): SaveResult {
  return { ok: true, text }
}

// failed is the result of a save or delete that did not happen, with the reason.
export function failed(text: string): SaveResult {
  return { ok: false, text }
}

// modelError is the reason of a failed save when it is about the model, for the model
// field; null for a save that worked and for any other failure.
export function modelError(result: SaveResult): string | null {
  return result !== null && !result.ok && /model/i.test(result.text) ? result.text : null
}
