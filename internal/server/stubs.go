package server

import "net/http"

// Placeholder handlers for routes owned by Tasks 6-9.
//
// Task 5 only establishes the router, the JSON error shape, and the auth
// middleware; it does not implement any handler logic. Every method below
// exists solely so the route table in server.go compiles and this task's
// auth tests can run against a real router. Each one is replaced with a real
// implementation in its owning task and removed from this file at that
// point:
//   - handleSystem                                    -> Task 6
//   - handleGetSettings, handlePutSettings,
//     handleTestSettings                               -> Task 6
//   - handleGetConfig, handleRefreshConfig, handlePlan  -> Task 7
//   - handleListRuns, handleCreateRun, handleGetRun,
//     handleCancelRun                                   -> Task 8
//   - handleRunEvents                                   -> Task 9
//
// When a task lands its handlers, delete the corresponding stub(s) here
// rather than leaving a shadowed/dead method behind.

func (s *Server) handleSystem(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handlePutSettings(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleTestSettings(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleRefreshConfig(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handlePlan(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

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
