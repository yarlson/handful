import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useImperativeHandle,
  type PointerEvent,
  type ReactNode,
  type Ref,
} from "react"
import { Shrink, ZoomIn, ZoomOut } from "lucide-react"
import { cn } from "@/lib/utils"
import {
  centre,
  clampView,
  doubleTapMs,
  doubleTapSlop,
  dragSlop,
  fitScale,
  fitView,
  isZoomed,
  keyPan,
  maxScale,
  panBy,
  percent,
  stepScale,
  swipePage,
  toggleScale,
  wheelPixels,
  wheelScale,
  zoomAround,
  type Frame,
  type Point,
  type View,
} from "@/lib/zoom"

// viewerButton is a round button of the media viewer. Its dark backing reads on a dark overlay
// and on a light image zoomed to fill the screen.
const viewerLook =
  "place-items-center rounded-full bg-black/55 text-white ring-1 ring-white/15 transition-colors hover:bg-black/75 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-5"
export const viewerButton = `grid size-10 ${viewerLook}`

// A gesture's announcement and its will-change wait this long after its last event.
const settleMs = 300

type Pointer = { start: Point; last: Point }

// ZoomHandle lets the viewer pass its keys on: key handles + - 0 1, and the arrows when zoomed,
// and tells whether it used the key.
export type ZoomHandle = {
  key: (e: {
    key: string
    shiftKey: boolean
    metaKey: boolean
    ctrlKey: boolean
    altKey: boolean
    preventDefault: () => void
  }) => boolean
}

// ZoomImage shows an image of the media viewer, which zooms and pans, with its zoom toolbar.
// children, the viewer's other controls, come between the image and the toolbar, in paint and
// in tab order. ref receives the handle for the zoom keys. onPage pages after a swipe, and
// onClose closes the viewer after a click beside the image at fit.
export function ZoomImage({
  url,
  alt,
  ref,
  onPage,
  onClose,
  children,
}: {
  url: string
  alt: string
  ref: Ref<ZoomHandle>
  onPage: (step: number) => void
  onClose: () => void
  children: ReactNode
}) {
  const stage = useRef<HTMLDivElement>(null)
  const [frame, setFrame] = useState<(Frame & { url: string }) | null>(null)
  const [view, setView] = useState<View>({ scale: 1, x: 0, y: 0 })
  const [animate, setAnimate] = useState(false)
  const [busy, setBusy] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [status, setStatus] = useState("")
  const ready = frame !== null && frame.url === url
  const zoomed = ready && isZoomed(view.scale, frame.fit)

  // The latest values, for listeners that outlive a render.
  const current = { view, frame: ready ? frame : null, zoomed, onPage, onClose }
  const latest = useRef(current)
  useLayoutEffect(() => {
    latest.current = current
  })
  const settle = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(settle.current), [])

  // apply shows next. animate eases a button's or a key's change; a gesture follows its input.
  // The zoom level is announced once the change settles.
  const apply = useCallback((next: View, eased: boolean) => {
    latest.current.view = next
    setView(next)
    setAnimate(eased)
    setBusy(true)
    clearTimeout(settle.current)
    settle.current = setTimeout(() => {
      setBusy(false)
      setStatus(`Zoom ${percent(latest.current.view.scale)} %`)
    }, settleMs)
  }, [])

  const zoomTo = useCallback(
    (scale: number, point: Point | null, eased: boolean) => {
      const f = latest.current.frame
      if (!f) return
      apply(zoomAround(latest.current.view, scale, point ?? centre(f.stage), f), eased)
    },
    [apply],
  )
  const step = (zoomIn: boolean) => frame && zoomTo(stepScale(view.scale, frame.fit, zoomIn), null, true)
  const toFit = () => frame && apply(fitView(frame), true)
  const toggle = useCallback(
    (point: Point) => {
      const f = latest.current.frame
      if (f) zoomTo(toggleScale(latest.current.view.scale, f.fit), point, true)
    },
    [zoomTo],
  )

  // measure reads the stage and the image into a frame. A new image, or a resize at fit, shows
  // it whole; a resize when zoomed keeps the zoom inside the new limits.
  const measure = useCallback((img: HTMLImageElement | null) => {
    const el = stage.current
    if (!el || !img || !img.naturalWidth) return
    const style = getComputedStyle(el)
    const padX = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight)
    const padY = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom)
    const natural = { width: img.naturalWidth, height: img.naturalHeight }
    const size = { width: el.clientWidth, height: el.clientHeight }
    const next = {
      url: img.getAttribute("src") ?? "",
      natural,
      stage: size,
      fit: fitScale(natural, { width: size.width - padX, height: size.height - padY }),
    }
    const prev = latest.current
    const keep = prev.frame?.url === next.url && prev.zoomed
    const v = keep ? clampView(prev.view, next) : fitView(next)
    latest.current.frame = next
    latest.current.view = v
    setFrame(next)
    setView(v)
    setAnimate(false)
  }, [])
  const img = useRef<HTMLImageElement>(null)
  useLayoutEffect(() => {
    if (img.current?.complete) measure(img.current)
  }, [url, measure])
  useEffect(() => {
    const el = stage.current
    if (!el) return
    const observer = new ResizeObserver(() => measure(img.current))
    observer.observe(el)
    return () => observer.disconnect()
  }, [measure])

  const point = (e: { clientX: number; clientY: number }): Point => {
    const r = stage.current!.getBoundingClientRect()
    return { x: e.clientX - r.left, y: e.clientY - r.top }
  }

  // Wheel and Safari's gesture events need preventDefault, so that the page behind never
  // zooms or scrolls, and React's onWheel is passive: the listeners go on the dialog, the
  // stage's parent, so that a pinch over a button zooms the image too.
  useEffect(() => {
    const el = stage.current?.parentElement
    if (!el) return
    let gestureScale: number | null = null
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const { view: v, frame: f, zoomed: z } = latest.current
      if (!f) return
      if (e.ctrlKey || e.metaKey) {
        // Safari sends a pinch as gesture events; a ctrl-wheel during one would zoom twice.
        if (gestureScale === null) zoomTo(wheelScale(v.scale, f.fit, e.deltaY, e.deltaMode), point(e), false)
        return
      }
      if (!z) return
      const dx = wheelPixels(e.deltaX, e.deltaMode, f.stage.width)
      const dy = wheelPixels(e.deltaY, e.deltaMode, f.stage.height)
      apply(panBy(v, -dx, -dy, f), false)
    }
    // Safari's GestureEvent: scale is the pinch since gesturestart.
    const onGesture = (e: Event) => {
      e.preventDefault()
      const g = e as Event & { scale: number; clientX: number; clientY: number }
      if (e.type === "gesturestart") gestureScale = latest.current.view.scale
      else if (e.type === "gestureend") gestureScale = null
      else if (gestureScale !== null) zoomTo(gestureScale * g.scale, point(g), false)
    }
    el.addEventListener("wheel", onWheel, { passive: false })
    const gestures = ["gesturestart", "gesturechange", "gestureend"]
    for (const type of gestures) el.addEventListener(type, onGesture)
    return () => {
      el.removeEventListener("wheel", onWheel)
      for (const type of gestures) el.removeEventListener(type, onGesture)
    }
  }, [apply, zoomTo])

  // Pointers: one drags (pans when zoomed, swipes at fit on touch), two pinch. dragged keeps the
  // click after a drag from closing the viewer.
  const pointers = useRef(new Map<number, Pointer>())
  const gesture = useRef({ dragged: false, pinched: false, zoomedAtStart: false, type: "mouse" })
  const lastTap = useRef<{ at: number; point: Point } | null>(null)
  const pinchOf = () => {
    const [a, b] = [...pointers.current.values()].map((p) => p.last)
    return { distance: Math.hypot(a.x - b.x, a.y - b.y), middle: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } }
  }

  const onPointerDown = (e: PointerEvent) => {
    if (e.pointerType === "mouse" && e.button !== 0) return
    const p = point(e)
    if (pointers.current.size === 0) {
      gesture.current = { dragged: false, pinched: false, zoomedAtStart: latest.current.zoomed, type: e.pointerType }
    }
    pointers.current.set(e.pointerId, { start: p, last: p })
    if (pointers.current.size === 2) {
      gesture.current.pinched = gesture.current.dragged = true
      for (const id of pointers.current.keys()) stage.current?.setPointerCapture(id)
    }
  }

  const onPointerMove = (e: PointerEvent) => {
    const me = pointers.current.get(e.pointerId)
    if (!me) return
    const f = latest.current.frame
    const p = point(e)
    if (pointers.current.size === 2) {
      const before = pinchOf()
      me.last = p
      const after = pinchOf()
      if (!f || before.distance === 0) return
      const v = latest.current.view
      const zoomedView = zoomAround(v, (v.scale * after.distance) / before.distance, after.middle, f)
      apply(panBy(zoomedView, after.middle.x - before.middle.x, after.middle.y - before.middle.y, f), false)
      return
    }
    const dx = p.x - me.last.x
    const dy = p.y - me.last.y
    me.last = p
    if (!gesture.current.dragged && Math.hypot(p.x - me.start.x, p.y - me.start.y) > dragSlop) {
      gesture.current.dragged = true
      stage.current?.setPointerCapture(e.pointerId)
      setDragging(latest.current.zoomed)
    }
    if (gesture.current.dragged && f && latest.current.zoomed) apply(panBy(latest.current.view, dx, dy, f), false)
  }

  const onPointerEnd = (e: PointerEvent) => {
    const me = pointers.current.get(e.pointerId)
    if (!me) return
    pointers.current.delete(e.pointerId)
    if (pointers.current.size > 0) return
    setDragging(false)
    const g = gesture.current
    if (g.type !== "touch" || e.type === "pointercancel" || g.pinched) return
    if (g.dragged) {
      const page = g.zoomedAtStart ? 0 : swipePage(me.last.x - me.start.x, me.last.y - me.start.y)
      if (page !== 0) latest.current.onPage(page)
      return
    }
    // A double tap toggles the zoom where it is.
    const tap = lastTap.current
    const now = e.timeStamp
    if (
      tap &&
      now - tap.at <= doubleTapMs &&
      Math.hypot(me.last.x - tap.point.x, me.last.y - tap.point.y) <= doubleTapSlop
    ) {
      lastTap.current = null
      toggle(me.last)
    } else {
      lastTap.current = { at: now, point: me.last }
    }
  }

  // keys handles + - 0 1, and the arrows when zoomed. ⌘ or Ctrl keeps a key the page's.
  useImperativeHandle(ref, () => ({
    key: (e) => {
      const { view: v, frame: f, zoomed: z } = latest.current
      if (!f || e.metaKey || e.ctrlKey || e.altKey) return false
      const arrows: Record<string, [number, number]> = {
        ArrowLeft: [1, 0],
        ArrowRight: [-1, 0],
        ArrowUp: [0, 1],
        ArrowDown: [0, -1],
      }
      let next: View | null = null
      if (e.key === "+" || e.key === "=") next = zoomAround(v, stepScale(v.scale, f.fit, true), centre(f.stage), f)
      else if (e.key === "-") next = zoomAround(v, stepScale(v.scale, f.fit, false), centre(f.stage), f)
      else if (e.key === "0") next = fitView(f)
      else if (e.key === "1") next = zoomAround(v, 1, centre(f.stage), f)
      else if (z && e.key in arrows) {
        const [sx, sy] = arrows[e.key]
        const d = keyPan(f.stage, e.shiftKey)
        next = panBy(v, sx * d.width, sy * d.height, f)
      }
      if (!next) return false
      e.preventDefault()
      apply(next, true)
      return true
    },
  }))

  // A toolbar button that its own press turns off (Zoom out at fit) would drop the focus out of
  // the dialog, and with it the keys: the focus moves to the readout, which is never off.
  const focusedTool = useRef<HTMLButtonElement | null>(null)
  const readout = useRef<HTMLButtonElement>(null)
  useLayoutEffect(() => {
    if (focusedTool.current?.disabled) readout.current?.focus()
  })

  const fit = ready ? frame.fit : 1
  const atFit = !zoomed
  const level = `${percent(ready ? view.scale : fit)} %`
  return (
    <>
      <div
        ref={stage}
        // The stage takes pointer input only; its keys are the dialog's.
        role="presentation"
        className={cn(
          "absolute inset-0 touch-none overflow-hidden p-4 select-none sm:p-16",
          zoomed && (dragging ? "cursor-grabbing" : "cursor-grab"),
        )}
        onClick={(e) => {
          if (e.target === e.currentTarget && !latest.current.zoomed && !gesture.current.dragged) onClose()
        }}
        onDoubleClick={(e) => {
          if (gesture.current.type !== "touch") toggle(point(e))
        }}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerEnd}
        onPointerCancel={onPointerEnd}
      >
        <img
          ref={img}
          key={url}
          src={url}
          alt={alt}
          draggable={false}
          onLoad={(e) => measure(e.currentTarget)}
          className={cn(
            "absolute top-0 left-0 max-w-none origin-top-left",
            !ready && "invisible",
            animate && "transition-transform duration-150 ease-out motion-reduce:transition-none",
            busy && "will-change-transform",
          )}
          style={{
            transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})`,
            // The corners stay 6 px round on screen at any zoom.
            borderRadius: `${6 / view.scale}px`,
          }}
        />
      </div>
      {children}
      <div
        role="toolbar"
        aria-label="Zoom"
        onFocus={(e) => {
          focusedTool.current = e.target instanceof HTMLButtonElement ? e.target : null
        }}
        // Focus that moves to another control leaves the toolbar. A button that turns itself off
        // loses focus with no new target, so that case keeps focusedTool for the move above.
        onBlur={(e) => {
          if (e.relatedTarget instanceof Node && !e.currentTarget.contains(e.relatedTarget)) focusedTool.current = null
        }}
        className="absolute bottom-[calc(12px+env(safe-area-inset-bottom))] left-1/2 flex -translate-x-1/2 items-center gap-1 rounded-full bg-black/60 p-1 ring-1 ring-white/10"
      >
        <button
          type="button"
          aria-label="Zoom out (−)"
          title="Zoom out (−)"
          className={cn(viewerButton, "max-md:size-11")}
          disabled={!ready || atFit}
          onClick={() => step(false)}
        >
          <ZoomOut />
        </button>
        <button
          ref={readout}
          type="button"
          aria-label={`Zoom level ${level}`}
          title="Zoom level"
          className={cn("grid h-10 w-15 text-sm tabular-nums max-md:h-11", viewerLook)}
          disabled={!ready}
          onClick={() => frame && toggle(centre(frame.stage))}
        >
          {level}
        </button>
        <button
          type="button"
          aria-label="Zoom in (+)"
          title="Zoom in (+)"
          className={cn(viewerButton, "max-md:size-11")}
          disabled={!ready || view.scale >= maxScale}
          onClick={() => step(true)}
        >
          <ZoomIn />
        </button>
        <button
          type="button"
          aria-label="Fit to screen (0)"
          title="Fit to screen (0)"
          className={cn(viewerButton, "max-md:size-11")}
          disabled={!ready || atFit}
          onClick={toFit}
        >
          <Shrink />
        </button>
        <button
          type="button"
          aria-label="Actual size (1)"
          title="Actual size (1)"
          className={cn(viewerButton, "text-sm font-semibold max-md:size-11")}
          disabled={!ready || percent(view.scale) === 100}
          onClick={() => zoomTo(1, null, true)}
        >
          1:1
        </button>
      </div>
      <output className="sr-only">{status}</output>
    </>
  )
}
