package server

import "net/http"

// Placeholder handlers for routes owned by Task 9.
//
// Task 5 only establishes the router, the JSON error shape, and the auth
// middleware; it does not implement any handler logic. This method exists
// solely so the route table in server.go compiles and this task's auth
// tests can run against a real router. It is replaced with a real
// implementation in Task 9 and removed from this file at that point:
//   - handleRunEvents -> Task 9
//
// When Task 9 lands its handler, delete the stub here rather than leaving a
// shadowed/dead method behind.

func (s *Server) handleRunEvents(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}
