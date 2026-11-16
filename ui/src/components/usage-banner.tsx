import { useEffect, useState } from "react"
import { X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { closeUsageBanner, closedUsageMessage, usageBannerShown, usageMessage } from "@/lib/usage-banner"

// UsageBanner says until when new turns wait for the usage limit, with a button that closes it.
export function UsageBanner({ pausedUntil }: { pausedUntil: string | null }) {
  const [closed, setClosed] = useState(closedUsageMessage)
  // A banner closed in another tab closes here too.
  useEffect(() => {
    const reread = () => setClosed(closedUsageMessage())
    addEventListener("storage", reread)
    return () => removeEventListener("storage", reread)
  }, [])
  const message = pausedUntil ? usageMessage(pausedUntil) : null
  if (!usageBannerShown(message, closed)) return null
  const close = () => {
    closeUsageBanner(message)
    setClosed(message)
  }
  return (
    <div className="bg-attention-surface flex items-center gap-2 py-1 pr-2 pl-4 text-sm">
      <p className="min-w-0 flex-1 py-1">{message}</p>
      <Button size="icon-xs" variant="ghost" aria-label="Close the usage limit message" onClick={close}>
        <X />
      </Button>
    </div>
  )
}
