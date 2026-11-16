import { createContext, useContext, useEffect, useRef } from "react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

// UnsavedEdits takes a view's report that it holds edits that leaving it would drop.
export const UnsavedEdits = createContext<(unsaved: boolean) => void>(() => {})

// useUnsavedEdits reports a view's unsaved edits to the app while the view is shown.
export function useUnsavedEdits(unsaved: boolean) {
  const report = useContext(UnsavedEdits)
  useEffect(() => {
    report(unsaved)
    return () => report(false)
  }, [report, unsaved])
}

// returnFocus focuses the element that had the focus when the dialog opened. A tab gets the
// selected tab of its list instead: a tab selects itself on focus, so focusing the tab the user
// left for would ask again after Keep editing.
function returnFocus(before: HTMLElement | null) {
  if (!before?.isConnected) return
  const target =
    before.getAttribute("role") === "tab"
      ? before.closest('[role="tablist"]')?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')
      : before
  target?.focus()
}

// DiscardDialog asks before a move that drops unsaved edits. Keep editing, Escape or a click
// outside stays, and the keyboard goes back where it was.
export function DiscardDialog({
  open,
  onDiscard,
  onKeep,
}: {
  open: boolean
  onDiscard: () => void
  onKeep: () => void
}) {
  const before = useRef<HTMLElement | null>(null)
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onKeep()}>
      <DialogContent
        role="alertdialog"
        showCloseButton={false}
        onOpenAutoFocus={() => {
          before.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
        }}
        onCloseAutoFocus={(e) => {
          e.preventDefault()
          returnFocus(before.current)
        }}
      >
        <DialogHeader>
          <DialogTitle>Discard changes?</DialogTitle>
          <DialogDescription>The settings you changed are not saved.</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onKeep}>
            Keep editing
          </Button>
          <Button variant="destructive" onClick={onDiscard}>
            Discard
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
