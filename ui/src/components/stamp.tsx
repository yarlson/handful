import { useEffect, useRef, useState, type PointerEvent } from "react"
import { Tooltip } from "radix-ui"
import { clock, fullDate } from "@/lib/dates"
import { cn } from "@/lib/utils"

// LONG_PRESS_MS is how long a touch on a time must last to show its full date.
const LONG_PRESS_MS = 500

// Stamp is a post's time in the browser's locale. Hover, keyboard focus or a long press shows
// the full date and time.
export function Stamp({ ms, className }: { ms: number; className?: string }) {
  const [open, setOpen] = useState(false)
  const press = useRef<ReturnType<typeof setTimeout>>(undefined)
  // held says a long press opened the tooltip, so the click that may end the press keeps it open.
  const held = useRef(false)
  const endPress = () => clearTimeout(press.current)
  useEffect(() => endPress, [])
  const startPress = (e: PointerEvent) => {
    if (e.pointerType !== "touch") return
    held.current = false
    endPress()
    press.current = setTimeout(() => {
      held.current = true
      setOpen(true)
    }, LONG_PRESS_MS)
  }
  return (
    <Tooltip.Provider delayDuration={300}>
      <Tooltip.Root open={open} onOpenChange={setOpen}>
        <Tooltip.Trigger asChild>
          <button
            type="button"
            className={cn("cursor-default whitespace-nowrap select-none [-webkit-touch-callout:none]", className)}
            onPointerDown={startPress}
            onPointerUp={endPress}
            onPointerCancel={endPress}
            onContextMenu={(e) => held.current && e.preventDefault()}
            onClick={(e) => {
              // A handled event stops the tooltip's own click handler, which would close it.
              if (held.current) e.preventDefault()
              held.current = false
            }}
          >
            <time dateTime={new Date(ms).toISOString()}>{clock(ms)}</time>
          </button>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            sideOffset={4}
            className="bg-popover text-popover-foreground z-50 rounded-md border px-2 py-1 text-xs shadow-md"
          >
            {fullDate(ms)}
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  )
}
