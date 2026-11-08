// The server starts a 400 error with "invalid input: ", the text of its
// sentinel error. The status already says that the input was wrong, and the
// rest reads as a sentence for the user.
const invalidPrefix = "invalid input: "

// errorMessage returns a server error as the UI shows it: without the
// "invalid input: " prefix. Other errors keep their text.
export function errorMessage(serverError: string): string {
  return serverError.startsWith(invalidPrefix) ? serverError.slice(invalidPrefix.length) : serverError
}
