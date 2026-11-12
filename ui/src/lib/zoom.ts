// The maths of the image viewer's zoom. A view draws the image at its natural size, scaled by
// scale from its top left corner, which sits at (x, y) in the stage. Sizes and points are CSS
// pixels; a scale of 1 shows one image pixel per CSS pixel.

export type Size = { width: number; height: number }
export type Point = { x: number; y: number }
export type View = { scale: number; x: number; y: number }

// The image as the viewer shows it: its natural size, the stage it is drawn in, and the fit
// scale at which the whole image fits the stage's padded area.
export type Frame = { natural: Size; stage: Size; fit: number }

export const maxScale = 8
export const zoomStep = 1.25
// A pointer that moves farther than dragSlop between down and up drags instead of clicking.
export const dragSlop = 4
// A one-finger swipe this long, mostly sideways, pages to the next or the previous image.
export const swipeDistance = 60
// A second tap within doubleTapMs and doubleTapSlop of the first is a double tap.
export const doubleTapMs = 300
export const doubleTapSlop = 30

// micro is a scale in millionths, so that a zoom in and out again lands back on the same value
// despite rounding errors.
function micro(scale: number): number {
  return Math.round(scale * 1e6)
}

// fitScale is the scale at which the whole image fits area. It is never above 1, so a small
// image is not blown up.
export function fitScale(natural: Size, area: Size): number {
  return Math.min(1, area.width / natural.width, area.height / natural.height)
}

// clampScale keeps scale between fit and maxScale.
export function clampScale(scale: number, fit: number): number {
  return Math.min(maxScale, Math.max(fit, scale))
}

// isZoomed tells whether scale is above fit.
export function isZoomed(scale: number, fit: number): boolean {
  return micro(scale) > micro(fit)
}

// stepScale is one zoom step in or out from scale: zoomStep times larger or smaller. A step that
// would cross 100 % stops at 100 %, and one past a limit stops at it.
export function stepScale(scale: number, fit: number, zoomIn: boolean): number {
  if (zoomIn) {
    const next = scale * zoomStep
    return clampScale(micro(scale) < micro(1) ? Math.min(next, 1) : next, fit)
  }
  const next = scale / zoomStep
  return clampScale(micro(scale) > micro(1) ? Math.max(next, 1) : next, fit)
}

// wheelScale is the scale after a ⌘/Ctrl wheel event. A trackpad pinch sends many small deltas,
// which zoom smoothly; a mouse wheel notch (lines, or 50 pixels or more) is one zoom step, and
// a notch with no vertical part changes nothing.
export function wheelScale(scale: number, fit: number, deltaY: number, deltaMode: number): number {
  if (deltaMode !== 0 || Math.abs(deltaY) >= 50) {
    const direction = Math.sign(-deltaY)
    return direction === 0 ? scale : stepScale(scale, fit, direction === 1)
  }
  return clampScale(scale * Math.exp(-deltaY * 0.01), fit)
}
// wheelPixels is a wheel delta in CSS pixels: deltaMode 0 is pixels, 1 lines and 2 pages.
export function wheelPixels(delta: number, deltaMode: number, page: number): number {
  if (deltaMode === 1) return delta * 16
  if (deltaMode === 2) return delta * page
  return delta
}

// toggleScale is where a double-click or a click on the readout goes: back to fit when zoomed,
// else to 100 %, or to 200 % for a small image that is already at 100 % at fit.
export function toggleScale(scale: number, fit: number): number {
  if (isZoomed(scale, fit)) return fit
  return micro(fit) === micro(1) ? 2 : 1
}

// clampAxis is an offset along one axis: centred when the image is no larger than the stage,
// else kept so that the image's edges never move inside the stage's edges. free is the room left
// beside the image: the offset is free / 2 when free ≥ 0, and between free and 0 when it is
// negative.
function clampAxis(offset: number, size: number, stage: number): number {
  const free = stage - size
  return Math.min(Math.max(0, free / 2), Math.max(Math.min(free, free / 2), offset))
}

// clampView keeps view's image inside the pan limits of frame, at a scale between fit and
// maxScale.
export function clampView(view: View, frame: Frame): View {
  const scale = clampScale(view.scale, frame.fit)
  return {
    scale,
    x: clampAxis(view.x, frame.natural.width * scale, frame.stage.width),
    y: clampAxis(view.y, frame.natural.height * scale, frame.stage.height),
  }
}

// fitView is the whole image, centred.
export function fitView(frame: Frame): View {
  return clampView({ scale: frame.fit, x: 0, y: 0 }, frame)
}

// zoomAround zooms view to scale and keeps the image point under point where it is, unless the
// pan limits move it.
export function zoomAround(view: View, scale: number, point: Point, frame: Frame): View {
  const next = clampScale(scale, frame.fit)
  const ratio = next / view.scale
  return clampView(
    { scale: next, x: point.x - (point.x - view.x) * ratio, y: point.y - (point.y - view.y) * ratio },
    frame,
  )
}

// panBy moves view's image by (dx, dy), within the pan limits.
export function panBy(view: View, dx: number, dy: number, frame: Frame): View {
  return clampView({ scale: view.scale, x: view.x + dx, y: view.y + dy }, frame)
}

// centre is the middle of the stage.
export function centre(stage: Size): Point {
  return { x: stage.width / 2, y: stage.height / 2 }
}

// keyPan is how far an arrow key pans: a tenth of the stage, or half of it with Shift.
export function keyPan(stage: Size, shift: boolean): Size {
  const part = shift ? 0.5 : 0.1
  return { width: stage.width * part, height: stage.height * part }
}

// swipePage is the page a one-finger swipe of (dx, dy) goes to: 1 for the next image after a
// swipe to the left, -1 for the previous one, and 0 for a swipe that is too short or not
// mostly sideways.
export function swipePage(dx: number, dy: number): -1 | 0 | 1 {
  if (Math.abs(dx) < swipeDistance || Math.abs(dx) <= Math.abs(dy)) return 0
  return Math.sign(-dx) as -1 | 1
}

// percent is scale as a whole percentage, as the readout shows it.
export function percent(scale: number): number {
  return Math.round(scale * 100)
}
