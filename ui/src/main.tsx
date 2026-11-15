import { LucideProvider } from "lucide-react"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import "./index.css"
import { App } from "./App"
import { followAppearance } from "./lib/appearance"
import { moveOldKeys } from "./lib/stored-keys"

try {
  moveOldKeys(localStorage, Object.keys(localStorage))
} catch {
  // Storage that the browser blocks holds no old keys.
}

followAppearance(document.documentElement, window.matchMedia("(prefers-color-scheme: dark)"))

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <LucideProvider strokeWidth={1.5}>
      <App />
    </LucideProvider>
  </StrictMode>,
)
