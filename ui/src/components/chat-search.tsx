import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import { Search, X } from "lucide-react"
import { api, type Entry } from "@/lib/api"
import { MAX_RESULTS, MAX_SEARCH, MIN_SEARCH, searchable, snippetOf, speakerOf, textOf } from "@/lib/search"
import { dateTime, fullDate } from "@/lib/dates"
import { useNow } from "@/components/now"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

// The search waits for a pause in the typing before it asks the server.
const PAUSE_MS = 200
const MINUTE = 60_000

type Found = { query: string; entries: Entry[]; more: boolean } | { query: string; error: string }

// ChatSearch is a chat's search bar and its results, newest first. Choosing a result hands its
// entry to onPick and folds the list; typing or focusing the box shows it again. A new focus
// value moves the keyboard into the box. Escape closes the search.
export function ChatSearch({
  chatId,
  chatName,
  focus,
  onPick,
  onClose,
}: {
  chatId: string
  chatName: string
  focus: number
  onPick: (entryId: string) => void
  onClose: () => void
}) {
  const [query, setQuery] = useState("")
  const [found, setFound] = useState<Found | null>(null)
  const [listed, setListed] = useState(true)
  const box = useRef<HTMLInputElement>(null)
  const now = useNow(MINUTE)
  useEffect(() => {
    box.current?.focus()
    box.current?.select()
  }, [focus])
  const closeNow = useRef(onClose)
  useLayoutEffect(() => {
    closeNow.current = onClose
  })
  const closesOnEscape = useCallback((el: HTMLElement | null) => {
    if (!el) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return
      e.preventDefault()
      closeNow.current()
    }
    el.addEventListener("keydown", onKey)
    return () => el.removeEventListener("keydown", onKey)
  }, [])
  const q = searchable(query)
  useEffect(() => {
    if (!q) return
    let live = true
    const t = setTimeout(
      () =>
        api.searchEntries(chatId, q).then(
          (r) => live && setFound({ query: q, ...r }),
          (e: Error) => live && setFound({ query: q, error: e.message }),
        ),
      PAUSE_MS,
    )
    return () => {
      live = false
      clearTimeout(t)
    }
  }, [chatId, q])
  const shown = q && found?.query === q ? found : null
  const status = !query.trim()
    ? ""
    : !q
      ? `Type ${MIN_SEARCH} or more characters.`
      : !shown
        ? "Searching…"
        : "error" in shown
          ? ""
          : shown.entries.length === 0
            ? "No messages match."
            : `${shown.entries.length} ${shown.entries.length === 1 ? "message" : "messages"}`
  return (
    <search className="relative block border-b" ref={closesOnEscape}>
      <div className="mx-auto flex max-w-(--chat-width) items-center gap-2 px-4 py-2">
        <Search className="text-muted-foreground size-4 shrink-0" aria-hidden />
        <Input
          ref={box}
          type="text"
          aria-label="Search this chat"
          placeholder="Search this chat"
          maxLength={MAX_SEARCH}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setListed(true)
          }}
          onFocus={() => setListed(true)}
          className="h-8 border-0 px-1 shadow-none focus-visible:ring-0 dark:bg-transparent"
        />
        <output className="text-muted-foreground shrink-0 text-xs whitespace-nowrap">{status}</output>
        <Button
          size="icon-sm"
          variant="ghost"
          className="shrink-0 max-md:size-11"
          aria-label="Close search"
          title="Close search"
          onClick={onClose}
        >
          <X />
        </Button>
      </div>
      {listed && shown && ("error" in shown || shown.entries.length > 0) && (
        <div className="absolute inset-x-0 top-full z-20 mx-auto max-w-(--chat-width) px-4">
          <div className="bg-popover text-popover-foreground max-h-[min(26rem,60dvh)] overflow-y-auto rounded-b-lg border border-t-0 shadow-lg">
            {"error" in shown ? (
              <p role="alert" className="text-destructive px-3 py-2 text-sm">
                {shown.error}
              </p>
            ) : (
              <ul aria-label="Search results" className="flex flex-col p-1">
                {shown.entries.map((e) => {
                  const part = snippetOf(textOf(e), shown.query)
                  return (
                    <li key={e.id}>
                      <button
                        type="button"
                        data-result={e.id}
                        className="hover:bg-muted focus-visible:bg-muted flex w-full flex-col gap-0.5 rounded-md px-3 py-2 text-left outline-none max-md:py-3"
                        onClick={() => {
                          setListed(false)
                          onPick(e.id)
                        }}
                      >
                        <span className="flex items-baseline gap-2 text-xs">
                          <span className="truncate font-medium">{speakerOf(e, chatName)}</span>
                          <time
                            dateTime={new Date(e.createdAt).toISOString()}
                            title={fullDate(e.createdAt)}
                            className="text-foreground/70 ml-auto whitespace-nowrap"
                          >
                            {dateTime(e.createdAt, now)}
                          </time>
                        </span>
                        <span className="line-clamp-2 text-sm break-words">
                          {part.before}
                          <mark className="bg-attention-surface text-foreground rounded-sm px-0.5">{part.match}</mark>
                          {part.after}
                        </span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
            {"more" in shown && shown.more && (
              <p className="text-foreground/70 border-t px-3 py-2 text-xs">
                Showing the {MAX_RESULTS} newest matches. Refine the search to find older ones.
              </p>
            )}
          </div>
        </div>
      )}
    </search>
  )
}
