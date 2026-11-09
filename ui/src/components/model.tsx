import { useEffect, useState, type ReactNode } from "react"
import { AlertTriangle } from "lucide-react"
import { Tooltip } from "radix-ui"
import { api } from "@/lib/api"
import {
  chipLabel,
  chipText,
  CUSTOM,
  defaultEffortOption,
  defaultModelOption,
  effortName,
  modelName,
  selectValue,
  type Defaults,
  type Effective,
  type Inherited,
} from "@/lib/model"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { cn } from "@/lib/utils"

// FALLBACK_ALIASES name models before GET /api/defaults has answered.
export const FALLBACK_ALIASES = ["fable", "opus", "sonnet", "haiku"]

// OPEN_SETTINGS is the event that asks the app to open a bot's settings at its model.
export const OPEN_SETTINGS = "yargpt:open-settings"
export function openSettings(botId: string) {
  window.dispatchEvent(new CustomEvent(OPEN_SETTINGS, { detail: botId }))
}

let loading: Promise<Defaults> | null = null

// useDefaults loads the global model and effort and the choices once per page.
export function useDefaults(): Defaults | null {
  const [d, setD] = useState<Defaults | null>(null)
  useEffect(() => {
    loading ??= api.defaults().catch((e: unknown) => {
      loading = null
      throw e
    })
    let live = true
    loading.then(
      (v) => live && setD(v),
      () => {},
    )
    return () => {
      live = false
    }
  }, [])
  return d
}

const selectClass = "border-input h-9 rounded-md border bg-transparent px-2 text-sm"

// ModelEffortFields edits a model and an effort. "" inherits; the first option of each
// select names what it would inherit and from where.
export function ModelEffortFields({
  id,
  modelLabel = "Model",
  effortLabel = "Effort",
  model,
  effort,
  onModel,
  onEffort,
  inherited,
  defaults,
  invalid,
}: {
  id: string
  modelLabel?: string
  effortLabel?: string
  model: string
  effort: string
  onModel: (v: string) => void
  onEffort: (v: string) => void
  inherited: { model: Inherited; effort: Inherited }
  defaults: Defaults | null
  invalid?: string | null
}) {
  const aliases = defaults?.modelAliases ?? FALLBACK_ALIASES
  const levels = defaults?.effortLevels ?? ["low", "medium", "high", "xhigh", "max"]
  // custom keeps the custom field open while its text is empty or happens to be an alias.
  const [custom, setCustom] = useState(selectValue(model, aliases) === CUSTOM)
  const shown = custom ? CUSTOM : selectValue(model, aliases)
  const errorId = `${id}-model-error`
  return (
    <>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`${id}-model`}>{modelLabel}</Label>
        <select
          id={`${id}-model`}
          className={selectClass}
          value={shown}
          onChange={(e) => {
            const v = e.target.value
            setCustom(v === CUSTOM)
            onModel(v === CUSTOM ? (selectValue(model, aliases) === CUSTOM ? model : "") : v)
          }}
        >
          <option value="">{defaultModelOption(inherited.model, aliases)}</option>
          {aliases.map((a) => (
            <option key={a} value={a}>
              {modelName(a, aliases)}
            </option>
          ))}
          <option value={CUSTOM}>Custom model ID…</option>
        </select>
        {custom && (
          <>
            <Label htmlFor={`${id}-model-custom`} className="text-muted-foreground text-xs">
              Custom model ID
            </Label>
            <Input
              id={`${id}-model-custom`}
              className="font-mono"
              placeholder="claude-opus-5-5"
              value={model}
              aria-invalid={invalid ? true : undefined}
              aria-describedby={invalid ? errorId : undefined}
              onChange={(e) => onModel(e.target.value.trim())}
            />
            {invalid && (
              <p id={errorId} className="text-destructive text-xs">
                {invalid}
              </p>
            )}
          </>
        )}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`${id}-effort`}>{effortLabel}</Label>
        <select id={`${id}-effort`} className={selectClass} value={effort} onChange={(e) => onEffort(e.target.value)}>
          <option value="">{defaultEffortOption(inherited.effort)}</option>
          {levels.map((l) => (
            <option key={l} value={l}>
              {effortName(l)}
            </option>
          ))}
        </select>
        {(effort === "xhigh" || effort === "max") && (
          <p className="text-muted-foreground text-xs">
            Models that don't support a level run at the nearest lower one.
          </p>
        )}
      </div>
    </>
  )
}

// ModelChip shows a bot's model and effort. It turns amber with a warning icon when Claude
// Code ran another model than the one asked for.
export function ModelChip({
  effective,
  aliases,
  onClick,
  prefix = "",
  suffix = "",
  who = "",
  action = "Open settings.",
}: {
  effective: Effective
  aliases: string[]
  onClick?: () => void
  prefix?: string
  suffix?: string
  // who names the bot in the label only, where the chip sits next to its name.
  who?: string
  action?: string
}) {
  const warn = Boolean(effective.modelMismatch)
  const label = `${who ? `${who}: ` : ""}${prefix}${chipLabel(effective, aliases)}${suffix ? ` ${suffix}` : ""}${onClick ? ` ${action}` : ""}`
  return (
    <button
      type="button"
      className={cn(
        "inline-flex max-w-full min-w-0 shrink items-center gap-1 rounded-full border px-2 py-0.5 text-xs",
        warn ? "border-amber-500/60 bg-amber-500/10 text-amber-700 dark:text-amber-400" : "text-muted-foreground",
        onClick ? "hover:bg-muted" : "cursor-default",
      )}
      aria-label={label}
      title={label}
      onClick={onClick}
    >
      {warn && <AlertTriangle className="size-3 shrink-0" aria-hidden />}
      <span className="truncate">
        {prefix && <span>{prefix}</span>}
        {chipText(effective, aliases)}
        {suffix && <span> {suffix}</span>}
      </span>
    </button>
  )
}

// Hint shows text on hover and on keyboard focus of its child.
export function Hint({ text, children }: { text: string | null; children: ReactNode }) {
  if (!text) return <>{children}</>
  return (
    <Tooltip.Provider delayDuration={300}>
      <Tooltip.Root>
        <Tooltip.Trigger asChild>{children}</Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            sideOffset={4}
            className="bg-popover text-popover-foreground z-50 rounded-md border px-2 py-1 text-xs shadow-md"
          >
            {text}
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  )
}
