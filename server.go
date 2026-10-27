package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
)

//go:embed web
var webFiles embed.FS

func newServer(store *Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, store.Sessions())
	})

	mux.HandleFunc("GET /api/sessions/current", func(w http.ResponseWriter, r *http.Request) {
		project, ok := projectParam(w, r)
		if !ok {
			return
		}
		sess, err := store.Current(project)
		respond(w, http.StatusOK, sess, err)
	})

	mux.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		project, ok := projectParam(w, r)
		if !ok {
			return
		}
		var input struct {
			Goal string `json:"goal"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		sess, err := store.StartSession(project, input.Goal)
		respond(w, http.StatusCreated, sess, err)
	})

	mux.HandleFunc("POST /api/sessions/current/steps", func(w http.ResponseWriter, r *http.Request) {
		project, ok := projectParam(w, r)
		if !ok {
			return
		}
		var input Step
		if !readJSON(w, r, &input) {
			return
		}
		sess, err := store.AddStep(project, input)
		respond(w, http.StatusCreated, sess, err)
	})

	mux.HandleFunc("POST /api/sessions/current/finish", func(w http.ResponseWriter, r *http.Request) {
		project, ok := projectParam(w, r)
		if !ok {
			return
		}
		var input struct {
			Reached bool `json:"reached"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		sess, err := store.Finish(project, input.Reached)
		respond(w, http.StatusOK, sess, err)
	})

	mux.HandleFunc("GET /api/traps", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, traps)
	})

	// Everything else is the board page, which is baked into the binary so
	// there's nothing extra to install.
	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServerFS(web))

	return mux
}

// Every request that reads or changes "the current session" has to say which
// project it means, via ?project=<path>. Each project has its own current
// session, so there's no sensible default.
func projectParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	project := r.URL.Query().Get("project")
	if project == "" {
		writeError(w, http.StatusBadRequest, errors.New("the project query parameter is required"))
		return "", false
	}
	return project, true
}

// respond writes v with the given status, or turns err into the right error
// status if it's non-nil.
func respond(w http.ResponseWriter, status int, v any, err error) {
	var verr ValidationError
	switch {
	case err == nil:
		writeJSON(w, status, v)
	case errors.As(err, &verr):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, ErrNoSession):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, ErrSessionEnded):
		writeError(w, http.StatusConflict, err)
	default:
		log.Printf("error: %v", err)
		writeError(w, http.StatusInternalServerError, errors.New("internal server error"))
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
