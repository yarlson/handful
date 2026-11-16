import { useId, useRef, useState, type ReactElement } from "react"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from "@/components/ui/popover"

// Confirm asks before a destructive action, next to the control that starts it. Cancel,
// Escape or a click elsewhere closes it, and only the action button calls onConfirm. The
// trigger carries data-state="open" while the question shows.
export function Confirm({
  question,
  action,
  onConfirm,
  children,
}: {
  question: string
  action: string
  onConfirm: () => void
  children: ReactElement
}) {
  const [open, setOpen] = useState(false)
  const cancel = useRef<HTMLButtonElement>(null)
  const title = useId()
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent
        align="end"
        className="w-auto"
        aria-labelledby={title}
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          cancel.current?.focus()
        }}
      >
        <PopoverTitle id={title}>{question}</PopoverTitle>
        <div className="flex justify-end gap-2">
          <Button ref={cancel} size="sm" variant="outline" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => {
              setOpen(false)
              onConfirm()
            }}
          >
            {action}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
