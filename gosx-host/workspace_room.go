package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/hub"
)

const roomEvents = "/room/events"

type storyboardRoom struct {
	workspace *workspaceHandler
	hub       *hub.Hub
	projectID string
	origin    *url.URL
	socket    string
	invite    string
	key       []byte

	mu       sync.Mutex
	revision int
}

var shareHost = regexp.MustCompile(`^[a-zA-Z0-9.-]+(?::[0-9]{1,5})?$`)

func parseShareOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !shareHost.MatchString(u.Host) || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("-share-origin must be an HTTPS origin such as https://board.example.com")
	}
	if port := u.Port(); port != "" {
		n, _ := strconv.Atoi(port)
		if n < 1 || n > 65535 {
			return nil, fmt.Errorf("-share-origin has an invalid port")
		}
		u.Host = strings.ToLower(u.Hostname())
		if n != 443 {
			u.Host += ":" + strconv.Itoa(n)
		}
	}
	return &url.URL{Scheme: "https", Host: strings.ToLower(u.Host)}, nil
}

func newStoryboardRoom(store *ProjectStore, projectID string, origin *url.URL) (*storyboardRoom, error) {
	p, err := store.Load(projectID)
	if err != nil {
		return nil, err
	}
	socketScheme := "ws"
	if origin.Scheme == "https" {
		socketScheme = "wss"
	}
	room := &storyboardRoom{projectID: projectID, origin: origin, invite: rand.Text(), key: []byte(rand.Text()),
		socket: socketScheme + "://" + origin.Host, revision: documentRevision(p), hub: hub.New("storyboard")}
	room.workspace = &workspaceHandler{store: store, onSave: room.publish}
	room.hub.RequireOrigin = true
	room.hub.MaxClients = 32
	room.hub.MaxMessagesPerSecond = 4
	presence := func(*hub.Context) { room.publish(0) }
	room.hub.On("join", presence)
	room.hub.On("leave", presence)
	return room, nil
}

func (room *storyboardRoom) publish(revision int) {
	room.mu.Lock()
	defer room.mu.Unlock()
	room.revision = max(room.revision, revision)
	room.hub.Broadcast("board", map[string]int{"revision": room.revision, "connected": room.hub.Presence().Count()})
}

func (room *storyboardRoom) cookieName() string {
	if room.origin.Scheme == "https" {
		return "__Host-storyboard_room"
	}
	return "storyboard_room_" + room.origin.Port()
}

func (room *storyboardRoom) sign(purpose, nonce string) string {
	mac := hmac.New(sha256.New, room.key)
	mac.Write([]byte(purpose + ":" + nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (room *storyboardRoom) sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(room.cookieName())
	if err != nil || len(cookie.Value) > 128 {
		return ""
	}
	nonce, signature, found := strings.Cut(cookie.Value, ".")
	if !found || nonce == "" || !hmac.Equal([]byte(signature), []byte(room.sign("session", nonce))) {
		return ""
	}
	return room.sign("csrf", nonce)
}

func (room *storyboardRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() || r.Host != room.origin.Host {
		http.Error(w, "Use the configured room address", http.StatusForbidden)
		return
	}
	workspaceHeaders(w, "'self' "+room.socket)
	if (r.Method == http.MethodPost || r.URL.Path == roomEvents) && r.Header.Get("Origin") != room.origin.String() {
		http.Error(w, "Invalid room origin", http.StatusForbidden)
		return
	}
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasPrefix(r.URL.Path, "/client/assets/") {
		room.workspace.clientAsset(w, r)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/workspace.css" {
		data, _ := workspaceAssets.ReadFile("workspace.css")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(data)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/join" {
		room.join(w, r)
		return
	}
	csrf := room.sessionToken(r)
	if r.Method == http.MethodGet && r.URL.Path == "/" {
		if csrf == "" {
			room.joinPage(w, http.StatusOK, "")
		} else {
			room.render(w, r, http.StatusOK, action.View{})
		}
		return
	}
	if csrf == "" {
		http.Error(w, "Join the storyboard room first", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/room/board" {
		room.render(w, r, http.StatusOK, action.View{})
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == roomEvents {
		room.hub.ServeHTTP(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/media/") {
		room.preview(w, r)
		return
	}
	if r.Method != http.MethodPost || !containsField([]string{"/save", "/insert", "/move"}, r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(csrf), []byte(r.PostForm.Get("csrf"))) != 1 || r.PostForm.Get("project") != room.projectID {
		http.Error(w, "Invalid room form", http.StatusForbidden)
		return
	}
	r.PostForm.Set("view", "board")
	handler := room.workspace.coreAction(r.URL.Path)
	if r.URL.Path == "/save" {
		handler = func(ctx *action.Context) error { return room.workspace.saveShot(ctx, true) }
	}
	serveFormAction(w, r, handler, room.render)
}

func (room *storyboardRoom) join(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil || subtle.ConstantTimeCompare([]byte(room.invite), []byte(strings.TrimSpace(r.PostForm.Get("invite")))) != 1 {
		room.joinPage(w, http.StatusForbidden, "That room code is invalid.")
		return
	}
	nonce := rand.Text()
	http.SetCookie(w, &http.Cookie{Name: room.cookieName(), Value: nonce + "." + room.sign("session", nonce),
		Path: "/", HttpOnly: true, Secure: room.origin.Scheme == "https", SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (room *storyboardRoom) joinPage(w http.ResponseWriter, status int, message string) {
	children := []any{el("h1", txt("Join a storyboard room")), el("p", txt("Enter the room code from your host."))}
	if message != "" {
		children = append(children, cls("p", "error", attrs("role", "alert"), txt(message)))
	}
	children = append(children, el("form", attrs("method", "post", "action", "/join"),
		el("label", txt("Room code"), el("input", attrs("type", "password", "name", "invite", "required", "required", "maxlength", "128"))), button("Join room")))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprint(w, "<!doctype html>", gosx.RenderHTML(el("html", attrs("lang", "en"),
		el("head", el("meta", attrs("charset", "utf-8")), el("meta", attrs("name", "viewport", "content", "width=device-width, initial-scale=1")),
			el("title", txt("Join a storyboard room")), el("link", attrs("rel", "stylesheet", "href", "/workspace.css"))),
		el("body", cls("main", "workspace room-workspace", cls("section", "paper", children...))))))
}

func (room *storyboardRoom) render(w http.ResponseWriter, r *http.Request, status int, submission action.View) {
	project, err := room.workspace.store.snapshot(room.projectID)
	if err != nil {
		http.Error(w, "Shared project unavailable", http.StatusServiceUnavailable)
		return
	}
	if id := r.URL.Query().Get("project"); id != "" && id != room.projectID {
		http.NotFound(w, r)
		return
	}
	p := project.document
	page := workspacePage{project: p, shots: project.shots, csrf: room.sessionToken(r), view: "board", scene: r.URL.Query().Get("scene"), submission: submission, shared: true}
	page.selectShot(r.URL.Query().Get("shot"))
	board := cls("div", "room-board", attrs("data-room-board", "true",
		"data-revision", strconv.Itoa(documentRevision(p)), "data-scene", page.scene,
		"data-selected-shot", shotText(page.shot, "id"), "data-shot-count", strconv.Itoa(len(project.shots))), page.board())
	if r.URL.Path == "/room/board" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		fmt.Fprint(w, gosx.RenderHTML(board))
		return
	}
	renderer, err := editorRuntime()
	if err != nil {
		http.Error(w, "Editor unavailable", http.StatusServiceUnavailable)
		return
	}
	saveStatus := "Saved to project"
	if submission.Submitted() {
		saveStatus = "Unsaved changes"
	}
	children := []any{cls("header", "room-heading",
		cls("div", "room-heading-copy", cls("p", "eyebrow", txt("PRODUCTION BOARD")),
			el("h1", txt(shotText(p, "title"))), cls("p", "room-brief", txt(shotText(p, "brief")))),
		cls("p", "room-shot-count", attrs("id", "room-shot-count"), txt(fmt.Sprintf("%d shots", len(project.shots))))),
		cls("div", "room-status", attrs("data-room-events", roomEvents, "data-revision", strconv.Itoa(documentRevision(p))),
			el("span", attrs("id", "room-presence", "role", "status"), txt("Connecting to room…")),
			el("span", attrs("id", "save-status", "role", "status"), txt(saveStatus)),
			el("span", attrs("id", "room-sync-error", "role", "status", "hidden", "hidden"), txt("Board updates paused. Reload to try again.")),
			link("Shot brief", "#shot-editor", "button secondary"),
			link("Reload board", page.href("board", shotText(page.shot, "id")), "button secondary"))}
	if submission.Submitted() {
		children = append(children, cls("p", "error", attrs("role", "alert"), txt(submission.Message()+". Your submitted values remain below.")))
	}
	children = append(children, board)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprint(w, gosx.RenderHTML(workspaceDocument(renderer, r, status, cls("main", "workspace room-workspace", children...), attrs())))
}

func shareablePreview(rel string) bool {
	return strings.HasPrefix(rel, "media/") && containsField([]string{".png", ".jpg", ".jpeg", ".webp"}, strings.ToLower(path.Ext(rel)))
}

func (room *storyboardRoom) preview(w http.ResponseWriter, r *http.Request) {
	prefix := "/media/" + room.projectID + "/"
	rel, ok := strings.CutPrefix(r.URL.Path, prefix)
	if ok && shareablePreview(rel) {
		project, err := room.workspace.store.snapshot(room.projectID)
		if err == nil {
			for _, shot := range project.shots {
				if selected, ok := boardPreview(shot); ok && rel == selected {
					room.workspace.media(w, r)
					return
				}
			}
		}
	}
	http.NotFound(w, r)
}
