package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnlyExplicitLoopbackRootIsAccepted(t *testing.T) {
	for _, raw := range []string{"https://example.com", "http://localhost:4492/", "http://127.0.0.1/", "http://127.0.0.1:0/", "http://user@127.0.0.1:4492/", "http://127.0.0.1:4492/api/"} {
		if _, err := validateURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := validateURL("http://127.0.0.1:4492/?project=demo"); err != nil {
		t.Fatal(err)
	}
}

func TestHealthRejectsAnotherAppAndRedirects(t *testing.T) {
	for _, test := range []struct {
		body   string
		status int
		ok     bool
	}{
		{`{"app":"Storyboard Desk"}`, 200, true},
		{`{"app":"Another app"}`, 200, false},
		{`{"app":"Storyboard Desk"}`, 302, false},
		{`no json`, 200, false},
	} {
		listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if test.status == 302 {
				w.Header().Set("Location", "https://example.com")
			}
			w.WriteHeader(test.status)
			fmt.Fprint(w, test.body)
		}))
		err := checkService(listener.URL)
		listener.Close()
		if (err == nil) != test.ok {
			t.Fatalf("status %d, body %s: %v", test.status, test.body, err)
		}
	}
}
