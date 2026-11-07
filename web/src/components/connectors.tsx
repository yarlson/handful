import { useCallback, useEffect, useState } from "react"
import { api, type Bot, type Connector } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const statusLabel: Record<Connector["status"], string> = {
  static: "token in file",
  connected: "connected",
  "needs-sign-in": "needs sign-in",
  expired: "expired",
}

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
  const [list, setList] = useState<Connector[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const reload = useCallback(() => {
    api.connectors().then(setList, (e: Error) => setError(e.message))
  }, [])
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
      setError(null)
    } catch (e) {
      tab?.close()
      setError((e as Error).message)
    }
  }
  const disconnect = async (name: string) => {
    try {
      await api.disconnect(name)
      reload()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const botName = (id: string) => bots.find((b) => b.id === id)?.name ?? id

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Connectors</DialogTitle>
          <DialogDescription>
            External MCP servers from connectors.json. A server without headers signs in here.
          </DialogDescription>
        </DialogHeader>
        {error && <p className="text-destructive text-sm">{error}</p>}
        {list?.length === 0 && (
          <p className="text-muted-foreground text-sm">
            No connectors. Add them to connectors.json in the data folder.
          </p>
        )}
        <div className="flex min-w-0 flex-col gap-2">
          {list?.map((c) => (
            <div key={c.name} className="flex items-start gap-2 rounded-md border p-2 text-sm">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{c.name}</span>
                  <Badge variant={c.status === "connected" || c.status === "static" ? "secondary" : "destructive"}>
                    {statusLabel[c.status]}
                  </Badge>
                </div>
                <p className="text-muted-foreground truncate text-xs" title={c.url}>
                  {c.url}
                </p>
                <p className="text-muted-foreground text-xs">
                  {c.bots.length ? c.bots.map(botName).join(", ") : "No bots"}
                </p>
              </div>
              {c.status !== "static" && (
                <div className="flex shrink-0 gap-1">
                  <Button
                    size="xs"
                    variant={c.status === "connected" ? "ghost" : "default"}
                    onClick={() => void connect(c.name)}
                  >
                    {c.status === "connected" ? "Reconnect" : "Connect"}
                  </Button>
                  {c.status !== "needs-sign-in" && (
                    <Button size="xs" variant="ghost" onClick={() => void disconnect(c.name)}>
                      Disconnect
                    </Button>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
