package server

import "net/http"

// Placeholder handlers for routes owned by Tasks 8-9.
//
// Task 5 only establishes the router, the JSON error shape, and the auth
// middleware; it does not implement any handler logic. Every method below
// exists solely so the route table in server.go compiles and this task's
// auth tests can run against a real router. Each one is replaced with a real
// implementation in its owning task and removed from this file at that
// point:
//   - handleListRuns, handleCreateRun, handleGetRun,
//     handleCancelRun                                   -> Task 8
//   - handleRunEvents                                   -> Task 9
//
// When a task lands its handlers, delete the corresponding stub(s) here
// rather than leaving a shadowed/dead method behind.

func (s *Server) handleListRuns(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleCreateRun(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleGetRun(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleRunEvents(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleCancelRun(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}
