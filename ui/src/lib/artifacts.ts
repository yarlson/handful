// The kinds an artifact can have, in display order. internal/core/artifacts.go
// keeps the same list.
export const ARTIFACT_KINDS = ["plan", "task", "issue", "review", "report", "note"] as const

export type ArtifactKind = (typeof ARTIFACT_KINDS)[number]

export type ArtifactFilter = { kind?: string; query?: string }

// artifactsPath is the list endpoint with the filters that are set.
export function artifactsPath(filter: ArtifactFilter): string {
  const params = new URLSearchParams()
  if (filter.kind) params.set("kind", filter.kind)
  const query = filter.query?.trim()
  if (query) params.set("q", query)
  const search = params.toString()
  return search ? `/api/artifacts?${search}` : "/api/artifacts"
}

// artifactAuthor names who saved an artifact: the user, a bot by its name, or
// a bot that no longer exists.
export function artifactAuthor(authorId: string, bots: { id: string; name: string }[]): string {
  if (authorId === "user") return "User"
  return bots.find((b) => b.id === authorId)?.name ?? "Deleted bot"
}
