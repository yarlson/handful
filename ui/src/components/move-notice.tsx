import { useState } from "react"
import { Button } from "@/components/ui/button"
import type { Bot } from "@/lib/api"
import { handfulName } from "@/lib/move-notice"

// showOldApp is the desktop app's way to show YarGPT.app in Finder; a browser has none.
function showOldApp(): (() => void) | undefined {
  return (window as unknown as { handfulSettings?: { showYarGPTApp?: () => void } }).handfulSettings?.showYarGPTApp
}

// MoveNotice welcomes the user above the chat list after the move from YarGPT (spec ad09e72c
// §5, S4). It offers each team named after YarGPT its Handful name, and renames a team only
// when the user confirms.
export function MoveNotice({
  teams,
  error = null,
  onDone,
  onRename,
}: {
  teams: Bot[]
  error?: string | null
  onDone: () => void
  onRename: (team: Bot, name: string) => void
}) {
  const [confirming, setConfirming] = useState<string | null>(null)
  const show = typeof window === "undefined" ? undefined : showOldApp()
  const offers = teams.flatMap((t) => {
    const name = handfulName(t.name)
    return name ? [{ team: t, name }] : []
  })
  return (
    <section
      aria-label="Welcome to Handful"
      className="bg-sidebar-foreground/5 mx-2 mb-2 flex flex-col gap-2 rounded-md p-2 text-xs"
    >
      <p>
        <strong>Welcome to Handful.</strong> Your bots, chats and teams moved to ~/.handful. ~/.yargpt is now a link to
        it, so leave it in place. You can delete YarGPT.app now.
      </p>
      {offers.map(({ team, name }) =>
        confirming === team.id ? (
          <div key={team.id} className="flex flex-wrap items-center gap-1">
            <span>{`Rename ${team.name} to ${name}?`}</span>
            <Button size="xs" onClick={() => onRename(team, name)}>
              Rename
            </Button>
            <Button size="xs" variant="ghost" onClick={() => setConfirming(null)}>
              Cancel
            </Button>
          </div>
        ) : (
          <Button
            key={team.id}
            size="xs"
            variant="outline"
            className="self-start"
            onClick={() => setConfirming(team.id)}
          >
            {`Rename to ${name}…`}
          </Button>
        ),
      )}
      {error && <p className="text-destructive">{error}</p>}
      <div className="flex flex-wrap gap-1">
        {show && (
          <Button size="xs" variant="outline" onClick={show}>
            Show YarGPT.app in Finder
          </Button>
        )}
        <Button size="xs" variant="outline" onClick={onDone}>
          Done
        </Button>
      </div>
    </section>
  )
}
