import { useCallback, useEffect, useReducer, useState } from "react"
import { api, type Bot } from "@/lib/api"
import { CONNECTOR_VIEWS, connectorsPhase, connectorsStep, NO_CONNECTORS } from "@/lib/connectors"
import { latestOnly } from "@/lib/latest"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

// ConnectorsDialog lists the MCP servers of connectors.json and signs in to
// the ones that use OAuth. Sign-in opens in a new tab; the list reloads when
// the user comes back to this one.
export function ConnectorsDialog({
  open,
  onOpenChange,
  bots,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  bots: Bot[]
}) {
  // connectors.ts holds the dialog's states; only the newest load's result applies.
  const [state, dispatch] = useReducer(connectorsStep, NO_CONNECTORS)
  const [run] = useState(latestOnly)
  const reload = useCallback(() => {
    run(
      api.connectors(),
      (list) => dispatch({ type: "loaded", list }),
      (e) => dispatch({ type: "loadFailed", text: e.message }),
    )
  }, [run])
  useEffect(() => {
    if (!open) return
    reload()
    window.addEventListener("focus", reload)
    return () => window.removeEventListener("focus", reload)
  }, [open, reload])

  const connect = async (name: string) => {
    // Open the tab now: a tab opened after the request returns counts as a popup.
    const tab = window.open("", "_blank")
    try {
      const { url } = await api.connect(name)
      if (!tab) throw new Error("The browser blocked the sign-in tab; allow pop-ups for YarGPT.")
      tab.opener = null
      tab.location.href = url
      dispatch({ type: "actionDone" })
    } catch (e) {
      tab?.close()
      dispatch({ type: "actionFailed", text: (e as Error).message })
    }
  }
  const disconnect = async (name: string) => {
    try {
      await api.disconnect(name)
      dispatch({ type: "actionDone" })
      reload()
    } catch (e) {
      dispatch({ type: "actionFailed", text: (e as Error).message })
    }
  }
  const botName = (id: string) => bots.find((b) => b.id === id)?.name ?? id
  const { list, error } = state
  const phase = connectorsPhase(state)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" data-connectors={phase}>
        <DialogHeader>
          <DialogTitle>Connectors</DialogTitle>
          <DialogDescription>
            External MCP servers from connectors.json. A server without headers signs in here.
          </DialogDescription>
        </DialogHeader>
        {error && <p className="text-destructive text-sm">{error.text}</p>}
        {phase === "loading" && <p className="text-muted-foreground text-sm">Loading connectors…</p>}
        {list?.length === 0 && (
          <p className="text-muted-foreground text-sm">
            No connectors. Add them to connectors.json in the data folder.
          </p>
        )}
        <div className="flex min-w-0 flex-col gap-2">
          {list?.map((c) => {
            const view = CONNECTOR_VIEWS[c.status]
            return (
              <div key={c.name} className="flex items-start gap-2 rounded-md border p-2 text-sm">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{c.name}</span>
                    <Badge variant={view.ok ? "secondary" : "destructive"}>{view.label}</Badge>
                  </div>
                  <p className="text-muted-foreground truncate text-xs" title={c.url}>
                    {c.url}
                  </p>
                  <p className="text-muted-foreground text-xs">
                    {c.bots.length ? c.bots.map(botName).join(", ") : "No bots"}
                  </p>
                </div>
                {view.signIn && (
                  <div className="flex shrink-0 gap-1">
                    <Button
                      size="xs"
                      variant={view.signIn === "Reconnect" ? "ghost" : "default"}
                      onClick={() => void connect(c.name)}
                    >
                      {view.signIn}
                    </Button>
                    {view.disconnect && (
                      <Button size="xs" variant="ghost" onClick={() => void disconnect(c.name)}>
                        Disconnect
                      </Button>
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </DialogContent>
    </Dialog>
  )
}
