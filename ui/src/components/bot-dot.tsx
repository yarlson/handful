import { useId, type SVGProps } from "react"

// BotDot is one bot from the Handful mark: a round dot with two eyes cut out of it, filled with
// currentColor. It is drawn for 16 px: the eyes are 2 × 4 px pills on whole pixels, so they stay
// sharp at 1x; the mark's own eye shapes blur away at this size.
export function BotDot(props: SVGProps<SVGSVGElement>) {
  const mask = useId()
  return (
    <svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" {...props}>
      <mask id={mask} maskUnits="userSpaceOnUse" x="0" y="0" width="16" height="16">
        <rect width="16" height="16" fill="#fff" />
        <rect x="5" y="5" width="2" height="4" rx="1" fill="#000" />
        <rect x="9" y="5" width="2" height="4" rx="1" fill="#000" />
      </mask>
      <circle cx="8" cy="8" r="6.5" mask={`url(#${mask})`} />
    </svg>
  )
}
