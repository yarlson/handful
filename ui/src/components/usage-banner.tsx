import { useEffect, useState, type ReactNode } from "react"
import { X } from "lucide-react"
import { useNow } from "@/components/now"
import { Button } from "@/components/ui/button"
import {
  closePlanWarning,
  closeUsageBanner,
  closedPlanWarnings,
  closedUsageMessage,
  planWarning,
  usageBannerShown,
  usageMessage,
  warningText,
  type PlanWindow,
} from "@/lib/usage-banner"

// UsageBanner says until when new turns wait for the usage limit. Without a pause, it warns about
// the plan window closest to its limit, with "See usage" when onSeeUsage is given. Each has a
// button that closes it.
export function UsageBanner({
  pausedUntil,
  windows,
  onSeeUsage,
}: {
  pausedUntil: string | null
  windows: PlanWindow[]
  onSeeUsage?: () => void
}) {
  const [closed, setClosed] = useState(closedUsageMessage)
  const [closedWarnings, setClosedWarnings] = useState(closedPlanWarnings)
  // A warning whose window has reset hides within a minute.
  const now = useNow(60_000)
  // A banner closed in another tab closes here too.
  useEffect(() => {
    const reread = () => {
      setClosed(closedUsageMessage())
      setClosedWarnings(closedPlanWarnings())
    }
    addEventListener("storage", reread)
    return () => removeEventListener("storage", reread)
  }, [])
  if (pausedUntil) {
    const message = usageMessage(pausedUntil)
    if (!usageBannerShown(message, closed)) return null
    const close = () => {
      closeUsageBanner(message)
      setClosed(message)
    }
    return <Banner text={message} closeLabel="Close the usage limit message" onClose={close} />
  }
  const warning = planWarning(windows, closedWarnings, now)
  if (!warning) return null
  const close = () => {
    closePlanWarning(warning)
    setClosedWarnings(closedPlanWarnings())
  }
  return (
    <Banner text={warningText(warning)} closeLabel="Close the plan usage warning" onClose={close}>
      {onSeeUsage && (
        <Button size="xs" variant="link" onClick={onSeeUsage}>
          See usage
        </Button>
      )}
    </Banner>
  )
}

function Banner({
  text,
  closeLabel,
  onClose,
  children,
}: {
  text: string
  closeLabel: string
  onClose: () => void
  children?: ReactNode
}) {
  return (
    <div className="bg-attention-surface flex items-center gap-2 py-1 pr-2 pl-4 text-sm">
      <p className="min-w-0 flex-1 py-1">{text}</p>
      {children}
      <Button size="icon-xs" variant="ghost" aria-label={closeLabel} onClick={onClose}>
        <X />
      </Button>
    </div>
  )
}
