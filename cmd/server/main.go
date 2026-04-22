package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"ocean/internal/desk"
	"ocean/managedagent"
)

var (
	desktop *desk.Desk
	once    sync.Once
	initErr error
)

func getDesk(ctx context.Context) (*desk.Desk, error) {
	once.Do(func() {
		desktop, initErr = desk.New(ctx)
	})
	return desktop, initErr
}

func main() {
	addr := os.Getenv("OCEAN_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/init", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		d, err := getDesk(ctx)
		if err != nil {
			log.Printf("init failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("init ok session=%s remote=%s", d.SessionID(), r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"sessionId": d.SessionID()})
	})

	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Type  string `json:"type"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if body.Type == "" || body.Input == "" {
			http.Error(w, "type and input required", http.StatusBadRequest)
			return
		}
		log.Printf("chat in type=%s len=%d remote=%s", body.Type, len(body.Input), r.RemoteAddr)

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()

		d, err := getDesk(ctx)
		if err != nil {
			log.Printf("chat getDesk failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		write := func(ev managedagent.StreamEvent) error {
			b, err := json.Marshal(ev)
			if err != nil {
				return err
			}
			_, err = w.Write([]byte("data: "))
			if err != nil {
				return err
			}
			_, err = w.Write(b)
			if err != nil {
				return err
			}
			_, err = w.Write([]byte("\n\n"))
			if err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}

		err = d.Chat(ctx, body.Type, body.Input, func(ev managedagent.StreamEvent) error {
			typ, err := ev.Type()
			if err != nil {
				return err
			}
			if err := write(ev); err != nil {
				return err
			}
			if typ == "session.status_idle" {
				return managedagent.ErrStopStream
			}
			return nil
		})
		if err != nil {
			log.Printf("chat failed type=%s: %v", body.Type, err)
			return
		}
		log.Printf("chat done type=%s", body.Type)
		_, _ = w.Write([]byte("data: {\"type\":\"ocean.done\"}\n\n"))
		flusher.Flush()
	})

	log.Printf("Ocean desk API listening on %s (set OCEAN_HTTP_ADDR to change)", addr)
	log.Fatal(http.ListenAndServe(addr, withCORS(mux)))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
