import { useEffect, useState } from "react"

// useNow is the current time in ms, updated every everyMs; null stops the updates.
export function useNow(everyMs: number | null) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (everyMs === null) return
    const t = setInterval(() => setNow(Date.now()), everyMs)
    return () => clearInterval(t)
  }, [everyMs])
  return now
}
