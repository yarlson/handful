"use client"

import * as React from "react"
import { cn } from "cn"
import { ChevronRight } from "lucide-react"
import { Collapsible as CollapsiblePrimitive } from "radix-ui"

function Collapsible({ ...props }: React.ComponentProps<typeof CollapsiblePrimitive.Root>) {
  return <CollapsiblePrimitive.Root data-slot="collapsible" {...props} />
}

function CollapsibleTrigger({ ...props }: React.ComponentProps<typeof CollapsiblePrimitive.CollapsibleTrigger>) {
  return <CollapsiblePrimitive.CollapsibleTrigger data-slot="collapsible-trigger" {...props} />
}

function CollapsibleContent({ ...props }: React.ComponentProps<typeof CollapsiblePrimitive.CollapsibleContent>) {
  return <CollapsiblePrimitive.CollapsibleContent data-slot="collapsible-content" {...props} />
}

// Disclosure shows its children under a labelled trigger with a chevron. Closed children stay
// mounted and hidden, so what the user typed in them survives a close.
function Disclosure({
  label,
  open,
  onOpenChange,
  className,
  triggerClassName,
  children,
}: {
  label: React.ReactNode
  // open, when set, overrides the user's choice.
  open?: boolean
  onOpenChange?: (open: boolean) => void
  className?: string
  triggerClassName?: string
  children: React.ReactNode
}) {
  const [chosen, setChosen] = React.useState(false)
  const shown = open ?? chosen
  return (
    <CollapsiblePrimitive.Root
      data-slot="collapsible"
      className={className}
      open={shown}
      onOpenChange={(next) => {
        setChosen(next)
        onOpenChange?.(next)
      }}
    >
      <CollapsiblePrimitive.CollapsibleTrigger
        data-slot="collapsible-trigger"
        className={cn(
          "group/disclosure text-muted-foreground hover:text-foreground flex min-h-7 cursor-pointer items-center gap-1 rounded-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50 max-md:min-h-11",
          triggerClassName,
        )}
      >
        <ChevronRight
          aria-hidden
          className="size-3.5 shrink-0 transition-transform group-data-[state=open]/disclosure:rotate-90"
        />
        {label}
      </CollapsiblePrimitive.CollapsibleTrigger>
      <CollapsiblePrimitive.CollapsibleContent data-slot="collapsible-content" forceMount hidden={!shown}>
        {children}
      </CollapsiblePrimitive.CollapsibleContent>
    </CollapsiblePrimitive.Root>
  )
}

export { Collapsible, CollapsibleTrigger, CollapsibleContent, Disclosure }
