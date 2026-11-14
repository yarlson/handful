import { useCallback, useEffect, useState } from "react"
import { copyText, execCopy, type CopyResult } from "@/lib/copy"

export type CopyState = "idle" | CopyResult

// copyLabel is what a Copy control says: the result for 2 s after a copy, else its own label.
export function copyLabel(state: CopyState, idle: string) {
  return state === "copied" ? "Copied" : state === "failed" ? "Copy failed" : idle
}

// useCopy copies through the one copy path and keeps the result for 2 s. On failure it selects
// the element that shown returns, so the user can copy its text by hand.
export function useCopy() {
  const [state, setState] = useState<CopyState>("idle")
  useEffect(() => {
    if (state === "idle") return
    const timer = setTimeout(() => setState("idle"), 2000)
    return () => clearTimeout(timer)
  }, [state])
  const copy = useCallback((text: string, shown?: () => Element | null | undefined) => {
    const env = { clipboard: navigator.clipboard, execCopy: (t: string) => execCopy(document, t) }
    void copyText(text, env, () => {
      const el = shown?.()
      if (el) window.getSelection()?.selectAllChildren(el)
    }).then(setState)
  }, [])
  return [state, copy] as const
}
