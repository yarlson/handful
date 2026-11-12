import type { SVGProps } from "react"

// TeamDots is a team: three bots from the Handful mark, one behind and two in front as in the
// logo's rows, drawn as 1 px outlines with filled eyes, in currentColor. Centres sit on whole
// pixels and radii end on half pixels, so the outlines land on pixel rows and stay sharp at 1x.
const bots = [
  [8, 5],
  [4, 11],
  [12, 11],
]

export function TeamDots(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1" aria-hidden="true" {...props}>
      {bots.map(([x, y]) => (
        <g key={`${x},${y}`}>
          <circle cx={x} cy={y} r="3.5" />
          <rect x={x - 2} y={y - 1} width="1" height="2" fill="currentColor" stroke="none" />
          <rect x={x + 1} y={y - 1} width="1" height="2" fill="currentColor" stroke="none" />
        </g>
      ))}
    </svg>
  )
}
