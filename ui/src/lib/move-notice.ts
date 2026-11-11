// handfulName is the name that the welcome notice after the move offers a team named after
// YarGPT, the app's old name: "YarGPT Dev" becomes "Handful Dev". It returns null for any
// other name, which keeps its name. The notice only offers it; nothing renames a team itself.
export function handfulName(name: string): string | null {
  const m = /^YarGPT(\s.*)?$/.exec(name)
  return m ? "Handful" + (m[1] ?? "") : null
}
