package server

import (
	"crypto/subtle"
	"net/http"
)

// TokenHeader is the header carrying the API token.
const TokenHeader = "X-API-Token"

// RequireToken rejects any request whose X-API-Token header does not match.
//
// This API installs software as root, so there is no unauthenticated mode: a
// caller that reaches a handler has already proved it holds the token. The
// comparison is constant-time so a caller cannot learn the token a byte at a
// time by measuring responses, and neither the expected nor the supplied token
// is ever echoed in the rejection.
//
// An empty configured token always fails closed. subtle.ConstantTimeCompare
// on two empty byte slices returns equal (both length zero), so without this
// guard a caller-side misconfiguration that leaves the expected token empty
// would authenticate every request that also omits the header — turning the
// "no bootstrap mode" guarantee into exactly the open-by-default window it
// exists to prevent.
func RequireToken(token string) func(http.Handler) http.Handler {
	want := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Exactly one header, or nothing. Header.Get would silently take
			// the first of several, so a reverse proxy appending its own token
			// alongside a caller's would authenticate on whichever landed
			// first. An ambiguous credential is not a credential.
			values := r.Header.Values(TokenHeader)
			if len(values) != 1 {
				writeError(w, http.StatusUnauthorized, "invalid or missing API token")
				return
			}

			got := []byte(values[0])
			if len(want) == 0 || subtle.ConstantTimeCompare(got, want) != 1 {
				writeError(w, http.StatusUnauthorized, "invalid or missing API token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
