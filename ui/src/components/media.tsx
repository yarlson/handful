import { useState } from "react"
import { ChevronLeft, ChevronRight, ExternalLink, Play, X } from "lucide-react"
import { Dialog } from "radix-ui"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"

type MediaKind = "image" | "gif" | "video"

// mediaKind tells how to show a stored media file. The server names each
// file by its sniffed type, so the extension is reliable.
function mediaKind(name: string): MediaKind {
  const ext = name.slice(name.lastIndexOf(".") + 1)
  if (ext === "gif") return "gif"
  if (ext === "mp4" || ext === "webm" || ext === "mov") return "video"
  return "image"
}

const kindLabel: Record<MediaKind, string> = { image: "Image", gif: "GIF", video: "Video" }

// MediaGallery shows the images, GIFs and videos of a message: one file at
// its own shape, several as a grid of tiles. A click opens the viewer.
export function MediaGallery({ names }: { names: string[] }) {
  const [open, setOpen] = useState<number | null>(null)
  const single = names.length === 1
  return (
    <>
      <div className={cn(single ? "max-w-full" : "grid w-80 max-w-full grid-cols-2 gap-1")}>
        {names.map((name, i) => {
          const kind = mediaKind(name)
          return (
            <button
              key={name}
              type="button"
              aria-label={`Open ${kindLabel[kind].toLowerCase()} ${i + 1} of ${names.length}`}
              onClick={() => setOpen(i)}
              className={cn(
                "bg-muted focus-visible:ring-ring relative block overflow-hidden rounded-xl border outline-none focus-visible:ring-2",
                single && kind === "video" && "aspect-video w-80 max-w-full",
                !single && "aspect-square rounded-lg",
                names.length === 3 && i === 0 && "col-span-2 aspect-[2/1]",
              )}
            >
              <Tile name={name} kind={kind} fill={!single || kind === "video"} />
            </button>
          )
        })}
      </div>
      {open !== null && <MediaViewer names={names} index={open} onIndex={setOpen} onClose={() => setOpen(null)} />}
    </>
  )
}

// Tile is a file's preview. A video shows its first frame and a play mark.
function Tile({ name, kind, fill }: { name: string; kind: MediaKind; fill: boolean }) {
  const url = api.mediaUrl(name)
  if (kind === "video") {
    return (
      <>
        {/* "#t=0.1" makes Safari show the first frame instead of an empty box. */}
        <video src={`${url}#t=0.1`} preload="metadata" muted playsInline className="size-full bg-black object-cover" />
        <span className="absolute inset-0 grid place-items-center">
          <span className="grid size-11 place-items-center rounded-full bg-black/60 text-white">
            <Play className="ml-0.5 size-5 fill-current" />
          </span>
        </span>
      </>
    )
  }
  return (
    <>
      <img
        src={url}
        alt={kindLabel[kind]}
        className={cn("block", fill ? "size-full object-cover" : "max-h-80 max-w-full object-contain")}
      />
      {kind === "gif" && (
        <span className="absolute bottom-1.5 left-1.5 rounded bg-black/60 px-1 text-[10px] font-semibold text-white">
          GIF
        </span>
      )}
    </>
  )
}

const viewerButton =
  "grid size-10 place-items-center rounded-full bg-white/10 text-white transition-colors hover:bg-white/20 [&_svg]:size-5"

// MediaViewer shows one file of a message at full size. Arrows and the arrow
// keys page through the message's files; Escape or a click beside the file
// closes it.
function MediaViewer({
  names,
  index,
  onIndex,
  onClose,
}: {
  names: string[]
  index: number
  onIndex: (i: number) => void
  onClose: () => void
}) {
  const name = names[index]
  const kind = mediaKind(name)
  const url = api.mediaUrl(name)
  const count = names.length
  const go = (step: number) => onIndex((index + step + count) % count)
  return (
    <Dialog.Root open onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/90" />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed inset-0 z-50 flex items-center justify-center p-4 outline-none sm:p-16"
          onClick={(e) => e.target === e.currentTarget && onClose()}
          onKeyDown={(e) => {
            // A focused video uses the arrow keys to seek.
            if (count < 2 || e.target instanceof HTMLVideoElement) return
            if (e.key === "ArrowLeft") go(-1)
            if (e.key === "ArrowRight") go(1)
          }}
        >
          <Dialog.Title className="sr-only">{`${kindLabel[kind]} ${index + 1} of ${count}`}</Dialog.Title>
          {kind === "video" ? (
            <video key={name} src={url} controls autoPlay playsInline className="max-h-full max-w-full rounded-md" />
          ) : (
            <img
              key={name}
              src={url}
              alt={kindLabel[kind]}
              className="max-h-full max-w-full rounded-md object-contain"
            />
          )}
          <div className="absolute top-3 right-3 flex items-center gap-2">
            {count > 1 && <span className="mr-1 text-sm text-white/80 tabular-nums">{`${index + 1} / ${count}`}</span>}
            <a
              href={url}
              target="_blank"
              rel="noreferrer"
              aria-label="Open the original"
              title="Open the original"
              className={viewerButton}
            >
              <ExternalLink />
            </a>
            <Dialog.Close aria-label="Close" title="Close" className={viewerButton}>
              <X />
            </Dialog.Close>
          </div>
          {count > 1 && (
            <>
              <button
                type="button"
                aria-label="Previous"
                title="Previous"
                className={cn(viewerButton, "absolute top-1/2 left-3 -translate-y-1/2")}
                onClick={() => go(-1)}
              >
                <ChevronLeft />
              </button>
              <button
                type="button"
                aria-label="Next"
                title="Next"
                className={cn(viewerButton, "absolute top-1/2 right-3 -translate-y-1/2")}
                onClick={() => go(1)}
              >
                <ChevronRight />
              </button>
            </>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
