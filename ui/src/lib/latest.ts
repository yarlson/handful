// latestOnly returns a function that passes a request's result to onData or
// onError only if no newer request has started since, so a slow response
// never replaces a newer one.
export function latestOnly() {
  let latest: object | undefined
  return function run<T>(request: Promise<T>, onData: (data: T) => void, onError: (e: Error) => void) {
    const mine = {}
    latest = mine
    request.then(
      (data) => {
        if (mine === latest) onData(data)
      },
      (e: Error) => {
        if (mine === latest) onError(e)
      },
    )
  }
}
