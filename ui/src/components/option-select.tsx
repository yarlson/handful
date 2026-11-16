import type { ReactNode } from "react"
import { cn } from "cn"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

export type SelectOption = { value: string; label: ReactNode }

// EMPTY stands for an option whose value is "": the select keeps "" for "nothing chosen".
const EMPTY = "\u0000empty"

// OptionSelect picks one of a list of options. readOnly keeps it in the tab order, marked
// aria-disabled, and ignores input; disabled takes it out.
export function OptionSelect({
  id,
  value,
  options,
  onChange,
  label,
  describedBy,
  invalid = false,
  disabled = false,
  readOnly = false,
  placeholder,
  className,
}: {
  id?: string
  value: string
  options: readonly SelectOption[]
  onChange: (value: string) => void
  // label is the accessible name when no <label> points at id.
  label?: string
  describedBy?: string
  invalid?: boolean
  disabled?: boolean
  readOnly?: boolean
  // placeholder shows while value matches no option.
  placeholder?: string
  className?: string
}) {
  const selected = options.find((o) => o.value === value)
  return (
    <Select
      value={value === "" && selected ? EMPTY : value}
      onValueChange={(v) => {
        if (!readOnly) onChange(v === EMPTY ? "" : v)
      }}
      disabled={disabled}
      open={readOnly ? false : undefined}
    >
      <SelectTrigger
        id={id}
        aria-label={label}
        aria-describedby={describedBy}
        aria-invalid={invalid || undefined}
        aria-disabled={readOnly || undefined}
        className={cn("w-full min-w-0 aria-disabled:cursor-not-allowed aria-disabled:opacity-60", className)}
      >
        <SelectValue placeholder={placeholder}>{selected?.label}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value === "" ? EMPTY : o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
