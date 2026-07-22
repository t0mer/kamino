package server

import (
	"net/http"

	"github.com/t0mer/kamino/internal/sysinfo"
)

// handleSystem reports the host Kamino is running on, so the UI can show what
// it is about to provision and warn when the process is not root.
func (s *Server) handleSystem(w http.ResponseWriter, _ *http.Request) {
	info, err := sysinfo.Detect()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "detecting host: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}
