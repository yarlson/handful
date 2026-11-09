// isExternalLink reports whether href is a web link to an origin other than
// origin. Relative links, fragments, other schemes and unparsable links are
// not external. A missing href becomes the relative path "undefined".
export function isExternalLink(href: string | undefined, origin: string): boolean {
  let url: URL
  try {
    url = new URL(String(href), origin)
  } catch {
    return false
  }
  return (url.protocol === "http:" || url.protocol === "https:") && url.origin !== origin
}

// isLocalImage reports whether a post's image src is served by the app itself, such as an
// uploaded file under /api/media. Only such images load in a post: loading an image from
// another site would send a request there that the user never asked for.
export function isLocalImage(src: string | undefined, origin: string): boolean {
  if (!src) return false
  let url: URL
  try {
    url = new URL(src, origin)
  } catch {
    return false
  }
  // Only web URLs have an origin like the app's; others, such as javascript:, have "null".
  return url.origin === origin
}

// imageHost names where an image that doesn't load would come from, for its link.
export function imageHost(src: string, origin: string): string {
  try {
    return new URL(src, origin).host
  } catch {
    return src
  }
}
