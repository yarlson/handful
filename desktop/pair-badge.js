// The Dock badge for a phone that asks to pair. The page tells the app when a
// pair request starts and ends; while one waits and the window is not in
// front, the Dock icon shows a badge and bounces once. Electron's dock comes
// in as a dependency, so the tests run without it.

const badgeText = "1"

// createPairBadge makes the badge's state.
//   dock: {setBadge(text), bounce(type)}, or null where there is no Dock
//   isFocused(): whether the window is in front, where the banner shows
function createPairBadge({ dock, isFocused }) {
  let waiting = false

  function clear() {
    dock?.setBadge("")
  }

  return {
    // change takes the page's news: true when a request starts, false when it
    // is allowed, denied or timed out. Anything but true counts as ended.
    change(value) {
      const now = value === true
      if (now === waiting) return
      waiting = now
      if (!now) return clear()
      if (isFocused()) return
      dock?.setBadge(badgeText)
      dock?.bounce("informational")
    },
    // focus: the window came to the front, where the banner shows the request.
    focus: clear,
    // closed: the page is gone, so it can no longer tell the request's end.
    closed() {
      waiting = false
      clear()
    },
  }
}

module.exports = { createPairBadge, badgeText }
