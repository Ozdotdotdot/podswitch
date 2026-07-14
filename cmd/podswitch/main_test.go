package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostActionRoutesGrabAndToggle(t *testing.T) {
	playing := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]string
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["host"] != "pi" {
			t.Fatalf("host = %q", request["host"])
		}
		switch r.URL.Path {
		case "/api/grab":
			_, _ = w.Write([]byte(`{"holder":"pi"}`))
		case "/api/toggle":
			_ = json.NewEncoder(w).Encode(hostActionResult{Playing: &playing})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	grab, err := hostAction(server.URL, "pi", "grab")
	if err != nil || grab.Holder != "pi" {
		t.Fatalf("grab = %#v, %v", grab, err)
	}
	toggle, err := hostAction(server.URL, "pi", "toggle")
	if err != nil || toggle.Playing == nil || !*toggle.Playing {
		t.Fatalf("toggle = %#v, %v", toggle, err)
	}
}

func TestHostActionReturnsCoordinatorError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"host offline"}`))
	}))
	defer server.Close()

	if _, err := hostAction(server.URL, "pi", "grab"); err == nil || err.Error() != "host offline" {
		t.Fatalf("error = %v, want host offline", err)
	}
}
