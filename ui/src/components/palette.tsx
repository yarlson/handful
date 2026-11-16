import { useRef, useState, type KeyboardEvent } from "react"
import {
  Check,
  Inbox,
  Monitor,
  Moon,
  PanelLeft,
  PanelRight,
  Plus,
  Search,
  Settings as SettingsIcon,
  Sun,
} from "lucide-react"
import { ChatIcon } from "@/components/bot-colors"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { matching, moveActive, type Action, type Command } from "@/lib/palette"
import { MAC, shortcutLabel, shortcutOf } from "@/lib/shortcuts"
import { cn } from "@/lib/utils"

function ActionIcon({ action }: { action: Action }) {
  switch (action.kind) {
    case "chat":
      return <ChatIcon chat={action.chat} />
    case "waiting":
      return <Inbox className="size-4 shrink-0" />
    case "settings":
      return <SettingsIcon className="size-4 shrink-0" />
    case "new":
      return <Plus className="size-4 shrink-0" />
    case "sidebar":
      return <PanelLeft className="size-4 shrink-0" />
    case "panel":
      return <PanelRight className="size-4 shrink-0" />
    case "search":
      return <Search className="size-4 shrink-0" />
    case "theme": {
      const Icon = action.theme === "dark" ? Moon : action.theme === "light" ? Sun : Monitor
      return <Icon className="size-4 shrink-0" />
    }
  }
}

// PaletteList is the palette's rows, grouped, with the active one marked; theme is the current
// theme, which its row marks.
export function PaletteList({
  rows,
  active,
  theme,
  onActive,
  onRun,
}: {
  rows: Command[]
  active: number
  theme: string
  onActive: (i: number) => void
  onRun: (c: Command) => void
}) {
  if (rows.length === 0) return <p className="text-muted-foreground px-3 py-6 text-center text-sm">No matches</p>
  const groups = [...new Set(rows.map((r) => r.group))]
  return (
    <div
      id="palette-list"
      // A native select can't hold the icons and shortcuts of a row.
      // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role
      role="listbox"
      aria-label="Commands"
      className="max-h-[min(24rem,60dvh)] overflow-y-auto p-1"
    >
      {groups.map((g) => (
        <div
          key={g}
          // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role
          role="group"
          aria-labelledby={`palette-group-${g}`}
        >
          <div id={`palette-group-${g}`} className="text-muted-foreground px-2 pt-2 pb-1 text-xs font-medium">
            {g}
          </div>
          {rows.map((c, i) => {
            if (c.group !== g) return null
            const current = c.action.kind === "theme" && c.action.theme === theme
            return (
              <div
                key={c.id}
                id={`palette-option-${i}`}
                // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role
                role="option"
                aria-selected={i === active}
                tabIndex={-1}
                className={cn(
                  "flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm max-md:py-2.5",
                  i === active && "bg-accent text-accent-foreground",
                )}
                onMouseMove={() => i !== active && onActive(i)}
                onMouseDown={(e) => {
                  // Keep the focus in the input, so it goes back where it was on close.
                  e.preventDefault()
                  onRun(c)
                }}
              >
                <ActionIcon action={c.action} />
                <span className="min-w-0 flex-1 truncate">{c.label}</span>
                {current && <Check className="size-4 shrink-0" aria-label="Current theme" />}
                {c.shortcut && (
                  <kbd className={cn("font-sans text-xs", i !== active && "text-muted-foreground")}>
                    {shortcutLabel(c.shortcut, MAC)}
                  </kbd>
                )}
              </div>
            )
          })}
        </div>
      ))}
    </div>
  )
}

// CommandPalette finds a chat, a place or an action by name and runs it. Arrow keys move, Enter
// runs, and Escape or Mod+K closes it. On close the keyboard goes back where it was; a command
// runs after that, so a command that moves the keyboard keeps it where it put it.
export function CommandPalette({
  open,
  onOpenChange,
  commands,
  theme,
  onRun,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  commands: Command[]
  theme: string
  onRun: (c: Command) => void
}) {
  const [query, setQuery] = useState("")
  const [active, setActive] = useState(0)
  const returnTo = useRef<HTMLElement | null>(null)
  const chosen = useRef<Command | null>(null)
  const rows = matching(commands, query)
  const shown = Math.min(active, Math.max(rows.length - 1, 0))
  const showActive = (i: number) => {
    setActive(i)
    document.getElementById(`palette-option-${i}`)?.scrollIntoView({ block: "nearest" })
  }
  const run = (c: Command) => {
    chosen.current = c
    onOpenChange(false)
  }
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return
    if (shortcutOf(e.nativeEvent, MAC) === "palette") {
      e.preventDefault()
      onOpenChange(false)
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault()
      showActive(moveActive(shown, rows.length, e.key))
    } else if (e.key === "Enter" && rows.length > 0) {
      e.preventDefault()
      run(rows[shown])
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        aria-describedby={undefined}
        className="top-[12dvh] translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-lg"
        // Radix calls this before it moves the focus into the palette.
        onOpenAutoFocus={() => {
          returnTo.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
        }}
        // Radix would focus the dialog's trigger, and the palette has none.
        onCloseAutoFocus={(e) => {
          e.preventDefault()
          setQuery("")
          setActive(0)
          if (returnTo.current?.isConnected) returnTo.current.focus()
          const c = chosen.current
          chosen.current = null
          if (c) onRun(c)
        }}
      >
        <DialogTitle className="sr-only">Command palette</DialogTitle>
        <div className="flex items-center gap-2 border-b px-3">
          <Search className="text-muted-foreground size-4 shrink-0" />
          <input
            role="combobox"
            aria-label="Find a chat or a command"
            aria-expanded
            aria-controls="palette-list"
            aria-autocomplete="list"
            aria-activedescendant={rows.length > 0 ? `palette-option-${shown}` : undefined}
            placeholder="Find a chat or a command"
            autoComplete="off"
            spellCheck={false}
            className="placeholder:text-muted-foreground h-11 min-w-0 flex-1 bg-transparent text-sm outline-none max-md:text-base"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setActive(0)
            }}
            onKeyDown={onKeyDown}
          />
        </div>
        <PaletteList rows={rows} active={shown} theme={theme} onActive={setActive} onRun={run} />
        <p className="text-muted-foreground flex flex-wrap gap-x-3 border-t px-3 py-2 text-xs">
          <span>↑↓ to move</span>
          <span>Enter to run</span>
          <span>Esc to close</span>
          <span>{shortcutLabel("palette", MAC)} to open or close</span>
        </p>
      </DialogContent>
    </Dialog>
  )
}
