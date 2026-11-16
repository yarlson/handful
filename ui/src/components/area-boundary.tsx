import { Component, type ErrorInfo, type ReactNode } from "react"
import { RotateCw, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

type Props = {
  // area names the view in the console message.
  area: string
  // resetKey clears a shown error when it changes, for example when another chat opens.
  resetKey?: string
  // menu and close keep the area's way out when its own header is gone with the error.
  menu?: ReactNode
  close?: { label: string; onClose: () => void }
  // onReload loads the area's data again when the data lives outside the area.
  onReload?: () => void
  className?: string
  children: ReactNode
}

type State = { failed: boolean; resetKey?: string }

// AreaBoundary keeps a render error inside one area of the app: the area shows the error
// with Reload, which renders the area again, and the rest of the app keeps working.
export class AreaBoundary extends Component<Props, State> {
  state: State = { failed: false, resetKey: this.props.resetKey }

  static getDerivedStateFromError(): Partial<State> {
    return { failed: true }
  }

  static getDerivedStateFromProps(props: Props, state: State): Partial<State> | null {
    if (props.resetKey === state.resetKey) return null
    return { failed: false, resetKey: props.resetKey }
  }

  reload = () => {
    this.props.onReload?.()
    this.setState({ failed: false })
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`The ${this.props.area} view failed to render.`, error, info.componentStack)
  }

  render() {
    if (!this.state.failed) return this.props.children
    const { menu, close, className } = this.props
    return (
      <div className={cn("flex min-h-0 min-w-0 flex-1 flex-col", className)}>
        {(menu || close) && (
          <div className="flex h-12 shrink-0 items-center gap-2 px-4 max-md:h-14 max-md:px-3">
            {menu}
            <span className="flex-1" />
            {close && (
              <Button
                size="icon"
                variant="ghost"
                className="shrink-0 max-md:size-11"
                aria-label={close.label}
                title={close.label}
                onClick={close.onClose}
              >
                <X />
              </Button>
            )}
          </div>
        )}
        <div role="alert" className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center text-sm">
          <p>Something went wrong in this view.</p>
          <Button size="sm" variant="outline" onClick={this.reload}>
            <RotateCw /> Reload
          </Button>
        </div>
      </div>
    )
  }
}
