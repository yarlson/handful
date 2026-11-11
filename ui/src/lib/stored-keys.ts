// Keys this page stored in the browser before the rename start with "yargpt."; they're
// "handful." now. moveOldKeys copies each old key whose new key is missing, then removes the
// old one, so an unsent draft survives the rename. It can go after 2027-11.
const OLD = "yargpt."
const NEW = "handful."

type Store = Pick<Storage, "length" | "key" | "getItem" | "setItem" | "removeItem">

export function moveOldKeys(store: Store): void {
  const old: string[] = []
  for (let i = 0; i < store.length; i++) {
    const key = store.key(i)
    if (key?.startsWith(OLD)) old.push(key)
  }
  for (const key of old) {
    const moved = NEW + key.slice(OLD.length)
    const value = store.getItem(key)
    if (store.getItem(moved) === null && value !== null) store.setItem(moved, value)
    store.removeItem(key)
  }
}
