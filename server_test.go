package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSessionFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newServer(store))
	defer srv.Close()

	post := func(path, body string) int {
		t.Helper()
		resp, err := http.Post(srv.URL+path+"?project=/code/app", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	steps := []struct {
		name string
		path string
		body string
		want int
	}{
		{"step before any goal", "/api/sessions/current/steps", `{"direction":"toward","distance":1,"summary":"x"}`, 404},
		{"empty goal", "/api/sessions", `{"goal":" "}`, 400},
		{"start session", "/api/sessions", `{"goal":"ship the board"}`, 201},
		{"toward step", "/api/sessions/current/steps", `{"direction":"toward","distance":3,"summary":"wrote the store"}`, 201},
		{"stalled ignores distance", "/api/sessions/current/steps", `{"direction":"stalled","distance":9,"summary":"read docs"}`, 201},
		{"sideways with ghost", "/api/sessions/current/steps", `{"direction":"sideways","distance":2,"ghost":"yak-shaving","summary":"fixed shell config"}`, 201},
		{"unknown direction", "/api/sessions/current/steps", `{"direction":"backwards","distance":1,"summary":"x"}`, 400},
		{"distance out of range", "/api/sessions/current/steps", `{"direction":"toward","distance":6,"summary":"x"}`, 400},
		{"unknown ghost", "/api/sessions/current/steps", `{"direction":"sideways","distance":1,"ghost":"napping","summary":"x"}`, 400},
		{"missing summary", "/api/sessions/current/steps", `{"direction":"toward","distance":1}`, 400},
		{"finish", "/api/sessions/current/finish", `{"reached":true}`, 200},
		{"step after finish", "/api/sessions/current/steps", `{"direction":"toward","distance":1,"summary":"x"}`, 409},
	}
	for _, s := range steps {
		if got := post(s.path, s.body); got != s.want {
			t.Errorf("%s: got status %d, want %d", s.name, got, s.want)
		}
	}

	// Reopening the store from disk should give back exactly what was recorded.
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := reopened.Current("/code/app")
	if err != nil {
		t.Fatal(err)
	}
	if !sess.Reached || len(sess.Steps) != 3 {
		t.Fatalf("got reached=%v with %d steps, want reached=true with 3 steps", sess.Reached, len(sess.Steps))
	}
	if sess.Steps[1].Distance != 0 {
		t.Errorf("stalled step distance = %d, want 0", sess.Steps[1].Distance)
	}

	resp, err := http.Get(srv.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var all []Session
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil || len(all) != 1 {
		t.Fatalf("GET /api/sessions: got %d sessions, err %v", len(all), err)
	}
}

func TestNewGoalClosesOpenSession(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.StartSession("/code/app", "first")
	store.StartSession("/code/app", "second")

	all := store.Sessions()
	if all[0].EndedAt == nil || all[0].Reached {
		t.Errorf("first session should be ended and not reached, got %+v", all[0])
	}
	if all[1].EndedAt != nil {
		t.Errorf("second session should still be open")
	}
}

func TestProjectsAreSeparate(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.StartSession("/code/app", "ship login")
	store.StartSession("/code/blog", "write a post")

	// Starting a session in one project must not close the other one.
	if _, err := store.AddStep("/code/app", Step{Direction: "toward", Distance: 1, Summary: "form"}); err != nil {
		t.Fatalf("app step: %v", err)
	}
	if _, err := store.Finish("/code/blog", true); err != nil {
		t.Fatalf("finish blog: %v", err)
	}
	if _, err := store.AddStep("/code/app", Step{Direction: "toward", Distance: 1, Summary: "cookie"}); err != nil {
		t.Fatalf("app step after blog finished: %v", err)
	}

	app, _ := store.Current("/code/app")
	if app.Goal != "ship login" || len(app.Steps) != 2 {
		t.Errorf("app session = %q with %d steps, want \"ship login\" with 2", app.Goal, len(app.Steps))
	}
	if _, err := store.Current("/code/other"); err != ErrNoSession {
		t.Errorf("unknown project: got %v, want ErrNoSession", err)
	}
}

func TestProjectRequired(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newServer(store))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/sessions", "application/json", bytes.NewBufferString(`{"goal":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", resp.StatusCode)
	}
}
