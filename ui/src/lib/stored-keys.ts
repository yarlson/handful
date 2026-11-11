// Keys this page stored in the browser before the rename start with "yargpt."; they're
// "handful." now. moveOldKeys copies each old key whose new key is missing, then removes the
// old one, so an unsent draft survives the rename. It can go after 2027-11.
const OLD = "yargpt."
const NEW = "handful."

type Store = Pick<Storage, "getItem" | "setItem" | "removeItem">

// moveOldKeys moves the old keys among keys, the keys that store holds.
export function moveOldKeys(store: Store, keys: string[]): void {
  for (const key of keys.filter((k) => k.startsWith(OLD))) {
    const moved = NEW + key.slice(OLD.length)
    const value = store.getItem(key)
    if (store.getItem(moved) === null && value !== null) store.setItem(moved, value)
    store.removeItem(key)
  }
}
