package localapi

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) registerProcessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/processes", s.processes)
	mux.HandleFunc("GET /api/processes/{id}/logs", s.processLogs)
	mux.HandleFunc("POST /api/processes/{id}/restart", s.processRestart)
	mux.HandleFunc("DELETE /api/processes/{id}", s.processStop)
}

func (s *Server) processes(w http.ResponseWriter, _ *http.Request) {
	records, err := s.daemon.List()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "daemon_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) processRestart(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	record, err := s.daemon.Restart(id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_restart_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) processStop(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	killed, err := s.daemon.Kill(id, false, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_stop_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"killed": killed})
}

func (s *Server) processLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	lines := 200
	if value := r.URL.Query().Get("n"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 5000 {
			writeAPIError(w, http.StatusBadRequest, "invalid", "n must be between 0 and 5000")
			return
		}
		lines = parsed
	}
	var out strings.Builder
	if err := s.daemon.Logs(id, false, lines, "", false, func(chunk string) error {
		_, err := out.WriteString(chunk)
		return err
	}); err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": out.String()})
}

func processID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid", "invalid process id")
		return 0, false
	}
	return id, true
}
