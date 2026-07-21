package remote

import "net/http"

// RedirectAuthGuardForTest exposes the package-private redirectAuthGuard to
// the remote_test (external) test package. It exists solely so tests can
// exercise the guard's scheme/host decision logic directly against
// constructed *http.Request values — in particular an https -> http
// same-host scheme downgrade, which two real network listeners cannot
// reproduce (a single TCP port cannot serve both TLS and plaintext HTTP), so
// it cannot be observed end-to-end through a live redirect chain.
func RedirectAuthGuardForTest(repo *Repo, next func(req *http.Request, via []*http.Request) error) func(req *http.Request, via []*http.Request) error {
	return redirectAuthGuard(repo, next)
}
