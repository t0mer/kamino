package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/t0mer/kamino/internal/events"
	"github.com/t0mer/kamino/internal/state"
)

// heartbeatInterval keeps an idle stream alive. Proxies routinely drop a
// connection that has been silent for a minute, and an install can easily be
// silent for longer while apt downloads.
const heartbeatInterval = 20 * time.Second

// handleRunEvents streams a run's progress as Server-Sent Events.
//
// The handler replays what is already persisted before attaching to the live
// bus, so a browser refreshed midway through an install sees the whole run
// rather than a blank pane — which is precisely when an operator refreshes,
// because something looks wrong. A run that has already finished replays in
// full and the stream closes, so this endpoint doubles as "show me what
// happened" with no special case.
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// Subscribe BEFORE reading the replay snapshot from sqlite: an event
	// published between the read and the subscription would otherwise fall in
	// the gap and be lost entirely.
	ch, unsub := s.d.Bus.Subscribe(id)
	defer unsub()

	run, steps, err := s.d.DB.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "run "+id+" not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(e events.Event) bool {
		payload, err := json.Marshal(e)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	replayed := replay(s.d.DB, run, steps)

	// Watermark, captured the instant the replay snapshot is complete. Because
	// the engine's MultiSink persists a step before it publishes it, any live
	// event already reflected in the snapshot was published before this moment
	// and carries a bus timestamp at or before it. Dropping those live events
	// stops the replay and the live stream from double-delivering the same log
	// line — a visible double-print in the UI. Events published after the
	// watermark are genuinely new and always pass.
	//
	// The window this leaves open is the sub-microsecond gap between the last
	// snapshot read and this call: an event whose whole persist-then-publish
	// cycle lands there could be dropped though it was not in the snapshot. On
	// real hardware that is vanishingly unlikely, and the failure is a single
	// missing line, never a stall.
	cutoff := time.Now().UTC()

	for _, e := range replayed {
		if !send(e) {
			return
		}
	}

	if isTerminal(run.Status) {
		return
	}

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-ch:
			if !open {
				return
			}
			// Drop anything already covered by the replay snapshot.
			if !e.TS.After(cutoff) {
				continue
			}
			if !send(e) {
				return
			}
			if e.Type == events.EventRun && isTerminal(state.Status(e.Status)) {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// replay renders everything already persisted for a run as events, in the
// order they happened.
func replay(db *state.Store, run state.Run, steps []state.Step) []events.Event {
	out := make([]events.Event, 0, len(steps)*2)

	for _, st := range steps {
		out = append(out, events.Event{
			Type:   events.EventStep,
			RunID:  run.ID,
			StepID: st.ItemRef,
			Status: string(st.Status),
			TS:     run.StartedAt,
		})

		lines, err := db.StepLogs(st.ID)
		if err != nil {
			continue
		}
		for _, line := range lines {
			out = append(out, events.Event{
				Type:   events.EventLog,
				RunID:  run.ID,
				StepID: st.ItemRef,
				Stream: "stdout",
				Line:   line,
				TS:     run.StartedAt,
			})
		}
	}

	if isTerminal(run.Status) {
		out = append(out, events.Event{
			Type:   events.EventRun,
			RunID:  run.ID,
			Status: string(run.Status),
			TS:     run.StartedAt,
		})
	}
	return out
}

// isTerminal reports whether a status means the run is over.
func isTerminal(s state.Status) bool {
	switch s {
	case state.StatusSuccess, state.StatusFailed, state.StatusCancelled:
		return true
	}
	return false
}
