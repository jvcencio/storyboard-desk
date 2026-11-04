package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestStoryboardRoomAccessScope(t *testing.T) {
	root, id := legacyFixtureCopy(t)
	store, err := OpenProjectStore(root, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	other := createTestProject(t, store)
	origin, err := parseShareOrigin("https://Board.example.test:443/")
	if err != nil || origin.String() != "https://board.example.test" {
		t.Fatalf("invalid origin normalization: %v %v", origin, err)
	}
	room, err := newStoryboardRoom(store, id, origin)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, values url.Values, cookie *http.Cookie, source string) *http.Request {
		r := httptest.NewRequest(method, origin.String()+path, strings.NewReader(values.Encode()))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Origin", source)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		return r
	}
	send := func(r *http.Request, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		room.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s returned %d, want %d", r.Method, r.URL.Path, w.Code, status)
		}
		return w
	}
	invite := url.Values{"invite": {room.invite}}
	send(request("POST", "/join", invite, nil, "https://other.example.test"), 403)
	send(request("POST", "/join", url.Values{"invite": {"wrong"}}, nil, origin.String()), 403)
	if got := send(request("GET", "/", nil, nil, ""), 200).Body.String(); !strings.Contains(got, "Join a storyboard room") || strings.Contains(got, id) {
		t.Fatal("anonymous board request exposed project data")
	}
	send(request("GET", roomEvents, nil, nil, origin.String()), 401)
	cookies := send(request("POST", "/join", invite, nil, origin.String()), 303).Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("HTTPS room did not issue a protected session cookie")
	}
	cookie := cookies[0]
	tampered := *cookie
	tampered.Value += "x"
	send(request("GET", roomEvents, nil, &tampered, origin.String()), 401)
	send(request("GET", roomEvents, nil, cookie, ""), 403)
	send(request("GET", roomEvents, nil, cookie, "https://other.example.test"), 403)
	for _, path := range []string{"/?project=" + shotText(other, "id"), "/api/health", "/download/plan", "/media/" + id + "/exports/private.png", "/media/" + id + "/media/unselected.png"} {
		send(request("GET", path, nil, cookie, ""), 404)
	}
	foreignHost := request("GET", "/", nil, cookie, "")
	foreignHost.Host = "other.example.test"
	foreignHost.Header.Set("X-Forwarded-Host", origin.Host)
	send(foreignHost, 403)
	foreignPeer := request("GET", "/", nil, cookie, "")
	foreignPeer.RemoteAddr = "192.0.2.1:1234"
	send(foreignPeer, 403)
	p, err := store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	first := list(p, "shots")[0]
	_, rel := boardPreview(first)
	if rel == "" {
		t.Fatal("legacy fixture lacks a board preview")
	}
	send(request("GET", mediaURL(id, rel), nil, cookie, ""), 200)
	form := url.Values{"csrf": {room.sessionToken(request("GET", "/", nil, cookie, ""))}, "project": {id}, "shot": {shotText(first, "id")},
		"revision": {strconv.Itoa(documentRevision(p))}, "shot_base": {shotBriefVersion(first)}, "title": {"Should not save"}}
	send(request("POST", "/project", form, cookie, origin.String()), 404)
	send(request("POST", "/cloud-submit", form, cookie, origin.String()), 404)
	send(request("POST", "/save", form, cookie, ""), 403)
	otherSession := send(request("POST", "/join", invite, nil, origin.String()), 303).Result().Cookies()[0]
	send(request("POST", "/save", form, otherSession, origin.String()), 403)
	form.Set("project", shotText(other, "id"))
	send(request("POST", "/save", form, cookie, origin.String()), 403)
	saved, err := store.Load(id)
	if err != nil || documentRevision(saved) != documentRevision(p) {
		t.Fatal("a denied request changed the shared project", err)
	}
}
