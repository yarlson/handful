import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"

// KeyPage asks a browser on the computer for Handful's key, which writes to the API need. The
// desktop app gives its window the key itself; a browser gets it once, from the key file.
// onSaved runs once the browser has the key.
export function KeyPage({ keyFile, onSaved }: { keyFile: string; onSaved: () => void }) {
  const [key, setKey] = useState("")
  const [error, setError] = useState<string | null>(null)
  const save = async () => {
    setError(null)
    try {
      await api.saveKey(key)
      onSaved()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <div className="bg-background text-foreground flex min-h-dvh items-start justify-center px-4 pt-[max(3rem,env(safe-area-inset-top))]">
      <form
        className="flex w-full max-w-md flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
      >
        <img src="/logo.svg" alt="Handful" className="size-10" />
        <p className="text-sm">
          Handful needs its key on this browser. Run{" "}
          <code className="bg-muted rounded px-1 font-mono text-[0.85em]">cat {keyFile}</code> in a terminal on this
          computer, then paste the key here.
        </p>
        <div className="flex flex-col gap-1">
          <label htmlFor="handful-key" className="text-sm font-medium">
            Key
          </label>
          <Input
            id="handful-key"
            value={key}
            autoComplete="off"
            spellCheck={false}
            className="font-mono"
            onChange={(e) => setKey(e.target.value)}
          />
        </div>
        {error && (
          <p role="alert" className="text-destructive text-sm">
            {error}
          </p>
        )}
        <Button type="submit" className="self-start" disabled={!key.trim()}>
          Save key
        </Button>
      </form>
    </div>
  )
}
