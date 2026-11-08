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
