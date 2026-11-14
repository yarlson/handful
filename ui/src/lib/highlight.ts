// highlight marks up code in the 12 languages that chats show most. The UI loads this module
// with a dynamic import the first time a code block renders, so the JS loaded before then does
// not grow.
import { createLowlight } from "lowlight"
import bash from "highlight.js/lib/languages/bash"
import css from "highlight.js/lib/languages/css"
import diff from "highlight.js/lib/languages/diff"
import go from "highlight.js/lib/languages/go"
import javascript from "highlight.js/lib/languages/javascript"
import json from "highlight.js/lib/languages/json"
import markdown from "highlight.js/lib/languages/markdown"
import python from "highlight.js/lib/languages/python"
import sql from "highlight.js/lib/languages/sql"
import typescript from "highlight.js/lib/languages/typescript"
import xml from "highlight.js/lib/languages/xml"
import yaml from "highlight.js/lib/languages/yaml"
import type { RootContent } from "hast"

const lowlight = createLowlight({ bash, css, diff, go, javascript, json, markdown, python, sql, typescript, xml, yaml })

// highlight returns code as highlighted hast nodes, or null for a language it does not know
// (aliases such as ts, sh, html and yml count). Only span elements and text come out.
export function highlight(code: string, language: string): RootContent[] | null {
  if (!language || !lowlight.registered(language)) return null
  return lowlight.highlight(language, code).children
}
