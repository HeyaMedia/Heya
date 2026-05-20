package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/logbuf"
)

func handleGetLogs(buf *logbuf.RingBuffer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		if n <= 0 || n > 1000 {
			n = 200
		}
		level := r.URL.Query().Get("level")

		entries := buf.Recent(n)
		if level != "" {
			filtered := make([]logbuf.Entry, 0, len(entries))
			for _, e := range entries {
				if e.Level == level {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

func handleLogStream(buf *logbuf.RingBuffer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher.Flush()

		ch := buf.Subscribe()
		defer buf.Unsubscribe(ch)

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(entry)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
