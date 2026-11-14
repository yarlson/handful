// The copy path that every Copy control uses. The Clipboard API exists only in a secure context,
// and the phone listener serves plain HTTP, so a phone has none; there a selected hidden textarea
// and execCommand("copy") may still copy. When both fail, the caller selects the shown text, so
// the user can copy it by hand.

export type CopyResult = "copied" | "failed"

export interface CopyEnv {
  // clipboard is navigator.clipboard, which is undefined outside a secure context.
  clipboard?: { writeText(text: string): Promise<void> }
  execCopy(text: string): boolean
}

// copyText copies text by the first step that works. On failure it calls selectShown. A browser
// reports a refused copy as a DOMException; any other error is a bug and propagates.
export async function copyText(text: string, env: CopyEnv, selectShown: () => void): Promise<CopyResult> {
  if (env.clipboard) {
    try {
      await env.clipboard.writeText(text)
      return "copied"
    } catch (e) {
      // A browser can refuse the write (no permission, no focus); the textarea may still copy.
      if (!(e instanceof DOMException)) throw e
    }
  }
  try {
    if (env.execCopy(text)) return "copied"
  } catch (e) {
    if (!(e instanceof DOMException)) throw e
  }
  selectShown()
  return "failed"
}

// execCopy copies text through a hidden textarea and gives the keyboard focus back.
export function execCopy(doc: Document, text: string): boolean {
  const area = doc.createElement("textarea")
  area.value = text
  // readonly keeps the phone's keyboard closed; 16 px stops iOS zooming in on the focused field.
  area.setAttribute("readonly", "")
  Object.assign(area.style, { position: "fixed", top: "0", left: "0", opacity: "0", fontSize: "16px" })
  const focused = doc.activeElement
  doc.body.append(area)
  area.select()
  // iOS Safari selects nothing with select() alone.
  area.setSelectionRange(0, text.length)
  try {
    return doc.execCommand("copy")
  } finally {
    area.remove()
    ;(focused as HTMLOrSVGElement | null)?.focus()
  }
}
