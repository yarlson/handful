import { assert, test } from "vitest"
import {
  centre,
  clampScale,
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
  swipeDistance,
  swipePage,
  toggleScale,
  wheelPixels,
  wheelScale,
  zoomAround,
  zoomStep,
  type Frame,
} from "./zoom"

// A 2880×1800 screenshot on a 1440×900 stage whose padded area is 1312×772: it fits at
// min(1312/2880, 772/1800) = 772/1800.
const fit = 772 / 1800
const shot: Frame = { natural: { width: 2880, height: 1800 }, stage: { width: 1440, height: 900 }, fit }

const close = (actual: number, expected: number, what: string) =>
  assert.approximately(actual, expected, 1e-9, `${what}: ${actual}, want ${expected}`)

test("the constants are the spec's", () => {
  assert.deepEqual([maxScale, zoomStep, dragSlop, swipeDistance, doubleTapMs, doubleTapSlop], [8, 1.25, 4, 60, 300, 30])
})

test("fit is the scale at which the whole image fits, and never above 100 %", () => {
  close(fitScale({ width: 2880, height: 1800 }, { width: 1312, height: 772 }), 772 / 1800, "a wide screenshot")
  close(fitScale({ width: 1000, height: 3000 }, { width: 1312, height: 772 }), 772 / 3000, "a tall image")
  close(fitScale({ width: 2000, height: 500 }, { width: 1000, height: 772 }), 0.5, "limited by width")
  assert.equal(fitScale({ width: 200, height: 100 }, { width: 1312, height: 772 }), 1, "a small image stays at 100 %")
})

test("the scale stays between fit and 800 %", () => {
  assert.equal(clampScale(0.1, 0.4), 0.4)
  assert.equal(clampScale(0.4, 0.4), 0.4)
  assert.equal(clampScale(2, 0.4), 2)
  assert.equal(clampScale(8, 0.4), 8)
  assert.equal(clampScale(9, 0.4), 8)
})

test("zoomed means above fit, with no rounding error", () => {
  assert.isFalse(isZoomed(fit, fit))
  assert.isFalse(isZoomed((fit * 1.25) / 1.25, fit))
  assert.isFalse(isZoomed(fit / 2, fit))
  assert.isTrue(isZoomed(fit * 1.01, fit))
  assert.isTrue(isZoomed(fit + 2e-6, fit), "a millionth above fit is zoomed")
})

test("a step multiplies or divides by 1.25", () => {
  close(stepScale(0.4, 0.3, true), 0.5, "in")
  close(stepScale(0.5, 0.3, false), 0.4, "out")
  close(stepScale(2, 0.3, true), 2.5, "in above 100 %")
  close(stepScale(2.5, 0.3, false), 2, "out above 100 %")
})

test("a step that would cross 100 % stops at 100 %", () => {
  assert.equal(stepScale(0.9, 0.3, true), 1, "in from 90 %")
  assert.equal(stepScale(1.1, 0.3, false), 1, "out from 110 %")
  close(stepScale(1, 0.3, true), 1.25, "in from 100 %")
  close(stepScale(1, 0.3, false), 0.8, "out from 100 %")
  close(stepScale(0.8, 0.3, true), 1, "in to exactly 100 %")
})

test("a step stops at fit and at 800 %", () => {
  assert.equal(stepScale(0.35, 0.3, false), 0.3, "out past fit")
  assert.equal(stepScale(0.3, 0.3, false), 0.3, "out at fit")
  assert.equal(stepScale(7, 0.3, true), 8, "in past 800 %")
  assert.equal(stepScale(8, 0.3, true), 8, "in at 800 %")
  assert.isFalse(isZoomed(stepScale(fit * 1.25, fit, false), fit), "back to fit")
})

test("three steps in from the screenshot's fit raise it by 1.25 each, and three out return to fit", () => {
  let s = fit
  const seen: number[] = []
  for (const dir of [true, true, true, false, false, false]) {
    s = stepScale(s, fit, dir)
    seen.push(percent(s))
  }
  assert.deepEqual(seen, [54, 67, 84, 67, 54, 43])
  assert.isFalse(isZoomed(s, fit))
})

test("a pinch zooms smoothly; a mouse wheel notch is one step", () => {
  close(wheelScale(1, 0.3, -10, 0), Math.exp(0.1), "pinch out")
  close(wheelScale(1, 0.3, 10, 0), Math.exp(-0.1), "pinch in")
  close(wheelScale(1, 0.3, 49, 0), Math.exp(-0.49), "a large pinch delta")
  close(wheelScale(2, 0.3, -100, 0), 2.5, "a notch up in pixels")
  close(wheelScale(2, 0.3, 50, 0), 1.6, "a notch down of exactly 50 pixels")
  close(wheelScale(2, 0.3, -50, 0), 2.5, "a notch up of exactly 50 pixels")
  close(wheelScale(2, 0.3, 3, 1), 1.6, "a notch down in lines")
  close(wheelScale(2, 0.3, -1, 2), 2.5, "a page notch up")
  assert.equal(wheelScale(2, 0.3, 0, 1), 2, "a sideways notch")
  assert.equal(wheelScale(7.9, 0.3, -40, 0), 8, "a pinch stops at 800 %")
  assert.equal(wheelScale(0.31, 0.3, 40, 0), 0.3, "a pinch stops at fit")
})

test("wheel deltas turn into pixels", () => {
  assert.equal(wheelPixels(5, 0, 900), 5)
  assert.equal(wheelPixels(3, 1, 900), 48)
  assert.equal(wheelPixels(-1, 2, 900), -900)
})

test("a double-click goes to 100 % at fit, to 200 % for a small image, and back to fit when zoomed", () => {
  assert.equal(toggleScale(fit, fit), 1)
  assert.equal(toggleScale(1, 1), 2)
  assert.equal(toggleScale(1, fit), fit)
  assert.equal(toggleScale(2, 1), 1)
})

test("fit centres the whole image", () => {
  const v = fitView(shot)
  close(v.scale, fit, "scale")
  close(v.x, (1440 - 2880 * fit) / 2, "x")
  close(v.y, (900 - 1800 * fit) / 2, "y")
})

test("a zoomed image's edges never move inside the stage's edges", () => {
  const frame = { ...shot }
  // At 100 % the image is 2880×1800 on a 1440×900 stage: x is between -1440 and 0.
  assert.deepEqual(clampView({ scale: 1, x: 100, y: 50 }, frame), { scale: 1, x: 0, y: 0 })
  assert.deepEqual(clampView({ scale: 1, x: -2000, y: -1000 }, frame), { scale: 1, x: -1440, y: -900 })
  assert.deepEqual(clampView({ scale: 1, x: -700, y: -300 }, frame), { scale: 1, x: -700, y: -300 })
  assert.deepEqual(clampView({ scale: 1, x: -1440, y: -900 }, frame), { scale: 1, x: -1440, y: -900 })
})

test("an image smaller than the stage along an axis stays centred on it", () => {
  // At 60 % the image is 1728×1080: wider and taller than the 1440×900 stage.
  // A tall image at 50 % is 500 wide: centred across, limited down.
  const tall: Frame = { natural: { width: 1000, height: 3000 }, stage: { width: 1440, height: 900 }, fit: 0.25 }
  assert.deepEqual(clampView({ scale: 0.5, x: 0, y: 20 }, tall), { scale: 0.5, x: 470, y: 0 })
  assert.deepEqual(clampView({ scale: 0.5, x: 0, y: -1000 }, tall), { scale: 0.5, x: 470, y: -600 })
  const exact: Frame = { natural: { width: 1440, height: 900 }, stage: { width: 1440, height: 900 }, fit: 1 }
  assert.deepEqual(clampView({ scale: 1, x: -5, y: 5 }, exact), { scale: 1, x: 0, y: 0 })
})

test("clampView keeps the scale between fit and 800 %", () => {
  close(clampView({ scale: 0.1, x: 0, y: 0 }, shot).scale, fit, "below fit")
  assert.equal(clampView({ scale: 20, x: 0, y: 0 }, shot).scale, 8)
})

test("zooming around a point keeps the image point under it in place", () => {
  const start = { scale: 1, x: -700, y: -300 }
  const point = { x: 400, y: 500 }
  // Image point under (400, 500): ((400+700)/1, (500+300)/1) = (1100, 800).
  const v = zoomAround(start, 2, point, shot)
  assert.deepEqual(v, { scale: 2, x: 400 - 1100 * 2, y: 500 - 800 * 2 })
  const back = zoomAround(v, 1, point, shot)
  assert.deepEqual(back, start)
})

test("zooming around a point gives way to the pan limits and the scale limits", () => {
  // From fit around the top left corner: the corner's image point moves, since the image
  // can't leave a gap.
  const v = zoomAround(fitView(shot), 1, { x: 0, y: 0 }, shot)
  assert.deepEqual(v, { scale: 1, x: 0, y: 0 })
  assert.equal(zoomAround({ scale: 4, x: -100, y: -100 }, 50, { x: 0, y: 0 }, shot).scale, 8)
  const out = zoomAround({ scale: 1, x: -100, y: -100 }, 0.01, { x: 700, y: 400 }, shot)
  assert.deepEqual(out, fitView(shot))
})

test("panning moves the image within the limits", () => {
  assert.deepEqual(panBy({ scale: 1, x: -700, y: -300 }, -100, 50, shot), { scale: 1, x: -800, y: -250 })
  assert.deepEqual(panBy({ scale: 1, x: -700, y: -300 }, -5000, 5000, shot), { scale: 1, x: -1440, y: 0 })
  assert.deepEqual(panBy(fitView(shot), 100, 100, shot), fitView(shot), "at fit nothing moves")
})

test("the centre, the key pan and the readout", () => {
  assert.deepEqual(centre({ width: 1440, height: 900 }), { x: 720, y: 450 })
  assert.deepEqual(keyPan({ width: 1440, height: 900 }, false), { width: 144, height: 90 })
  assert.deepEqual(keyPan({ width: 1440, height: 900 }, true), { width: 720, height: 450 })
  assert.equal(percent(fit), 43)
  assert.equal(percent(0.385), 39)
  assert.equal(percent(8), 800)
})

test("a long sideways swipe pages; a short or upright one doesn't", () => {
  assert.equal(swipePage(-60, 0), 1, "left to the next image")
  assert.equal(swipePage(60, 10), -1, "right to the previous image")
  assert.equal(swipePage(-59, 0), 0, "too short")
  assert.equal(swipePage(59, 0), 0, "too short to the right")
  assert.equal(swipePage(-80, 80), 0, "as much down as across")
  assert.equal(swipePage(80, -100), 0, "mostly upright")
})
