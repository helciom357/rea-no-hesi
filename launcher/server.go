package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

const radminURL = "https://www.radmin-vpn.com/"

type App struct {
	token     string
	installer Installer
	lib       *Library
	pub       *Publisher
	addr      string

	mu  sync.Mutex
	hub *Hub // last hub loaded for the player view (with unlocked events)
}

func NewApp() *App {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	s := LoadSettings()
	lib := OpenLibrary(s.LibraryDir)
	return &App{token: hex.EncodeToString(b), lib: lib, pub: NewPublisher(lib)}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	mux.HandleFunc("GET /{$}", a.page("player.html"))
	mux.HandleFunc("GET /admin", a.page("admin.html"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	// Images and downloads are opened by <img>/<a>, which cannot send headers,
	// so they carry the token in the query instead.
	mux.Handle("GET /img/local", a.guardQuery(http.HandlerFunc(a.imgLocal)))
	mux.Handle("GET /img/lib", a.guardQuery(http.HandlerFunc(a.imgLib)))
	mux.Handle("GET /dl/upload.php", a.guardQuery(http.HandlerFunc(a.dlReceiver)))

	api := http.NewServeMux()
	api.HandleFunc("GET /api/state", a.getState)
	api.HandleFunc("POST /api/settings", a.postSettings)
	api.HandleFunc("GET /api/hub", a.getHub)
	api.HandleFunc("POST /api/unlock", a.postUnlock)
	api.HandleFunc("GET /api/live", a.getLive)
	api.HandleFunc("POST /api/install", a.postInstall)
	api.HandleFunc("GET /api/job", a.getJob)
	api.HandleFunc("POST /api/job/cancel", a.postCancel)
	api.HandleFunc("POST /api/join", a.postJoin)
	api.HandleFunc("POST /api/radmin", a.postRadmin)
	api.HandleFunc("GET /api/mods", a.getMods)
	api.HandleFunc("POST /api/mods/open", a.postOpenMod)

	api.HandleFunc("GET /api/admin/state", a.adminState)
	api.HandleFunc("POST /api/admin/host-code", a.adminHostCode)
	api.HandleFunc("POST /api/admin/owner-connect", a.adminOwnerConnect)
	api.HandleFunc("POST /api/admin/disconnect", a.adminDisconnect)
	api.HandleFunc("GET /api/admin/events", a.adminEvents)
	api.HandleFunc("POST /api/admin/probe", a.adminProbe)
	api.HandleFunc("POST /api/admin/draft", a.adminDraft)
	api.HandleFunc("POST /api/admin/publish", a.adminPublish)
	api.HandleFunc("POST /api/admin/event-delete", a.adminEventDelete)
	api.HandleFunc("POST /api/admin/drop", a.adminDrop)
	api.HandleFunc("GET /api/admin/installed", a.adminInstalled)
	api.HandleFunc("POST /api/admin/installed/add", a.adminAddInstalled)
	api.HandleFunc("GET /api/admin/hosts", a.adminHosts)
	api.HandleFunc("POST /api/admin/hosts/add", a.adminHostAdd)
	api.HandleFunc("POST /api/admin/hosts/remove", a.adminHostRemove)
	api.HandleFunc("POST /api/admin/base", a.adminBase)
	mux.Handle("/api/", a.guard(api))
	return mux
}

// guard only accepts requests from the page this process served: the right
// Host (blocks DNS rebinding) and the per-run token (blocks other websites).
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != a.addr || r.Header.Get("X-Token") != a.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) guardQuery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != a.addr || r.URL.Query().Get("t") != a.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := webFS.ReadFile("web/" + name)
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		html := strings.ReplaceAll(string(b), "{{TOKEN}}", a.token)
		html = strings.ReplaceAll(html, "{{VERSION}}", Version)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https: http:; connect-src 'self'")
		_, _ = w.Write([]byte(html))
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(v)
}

// ---- player -------------------------------------------------------------------

func (a *App) getState(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	writeJSON(w, map[string]any{
		"version":   Version,
		"acPath":    s.ACPath,
		"hubSource": s.HubSource,
		"hubUsed":   s.EffectiveHub(),
		"acValid":   IsACRoot(s.ACPath),
		"isHost":    s.UploadURL != "" && s.UploadKey != "",
	})
}

func (a *App) postSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	if v, ok := in["acPath"]; ok {
		s.ACPath = strings.Trim(strings.TrimSpace(v), `"`)
	}
	if v, ok := in["hubSource"]; ok {
		s.HubSource = strings.Trim(strings.TrimSpace(v), `"`)
	}
	if err := SaveSettings(s); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"acValid": IsACRoot(s.ACPath)})
}

// loadHub reads the base and opens the private events this player already
// has the password for.
func (a *App) loadHub(ctx context.Context) (*Hub, Settings, error) {
	s := LoadSettings()
	src := s.EffectiveHub()
	h, err := LoadHub(src)
	if err != nil {
		return nil, s, err
	}
	changed := false
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range h.Events {
		pw, ok := s.EventPasswords[h.Events[i].ID]
		if !h.Events[i].Locked || !ok {
			continue
		}
		wg.Add(1)
		go func(i int, pw string) {
			defer wg.Done()
			ev, err := Unlock(ctx, src, h.Events[i].ID, pw)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				h.Events[i] = *ev
			} else if strings.Contains(err.Error(), "senha") {
				delete(s.EventPasswords, h.Events[i].ID) // the host changed it
				changed = true
			}
		}(i, pw)
	}
	wg.Wait()
	if changed {
		_ = SaveSettings(s)
	}
	a.mu.Lock()
	a.hub = h
	a.mu.Unlock()
	return h, s, nil
}

func (a *App) getHub(w http.ResponseWriter, r *http.Request) {
	h, s, err := a.loadHub(r.Context())
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	statuses := map[string]EventStatus{}
	for _, ev := range h.Events {
		if !ev.Locked {
			statuses[ev.ID] = StatusForEvent(h, s.ACPath, ev)
		}
	}
	// Pictures of locked events still show on the cards.
	images := map[string]string{}
	for _, ev := range h.Events {
		images[ev.ID] = h.ImageURL(ev.Track)
	}
	writeJSON(w, map[string]any{"hub": h, "statuses": statuses, "images": images, "acValid": IsACRoot(s.ACPath)})
}

func (a *App) postUnlock(w http.ResponseWriter, r *http.Request) {
	var in struct{ EventID, Password string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	if _, err := Unlock(r.Context(), s.EffectiveHub(), in.EventID, in.Password); err != nil {
		writeErr(w, 403, err)
		return
	}
	if s.EventPasswords == nil {
		s.EventPasswords = map[string]string{}
	}
	s.EventPasswords[in.EventID] = in.Password
	if err := SaveSettings(s); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) getLive(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	h := a.hub
	a.mu.Unlock()
	if h == nil {
		writeJSON(w, map[string]LiveInfo{})
		return
	}
	writeJSON(w, FetchLiveAll(h.Events))
}

func (a *App) postInstall(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EventIDs []string `json:"eventIds"`
		Only     []string `json:"only"` // optional item keys (kind:id)
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	h, s, err := a.loadHub(r.Context())
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var events []Event
	for _, ev := range h.Events {
		if len(in.EventIDs) == 0 || contains(in.EventIDs, ev.ID) {
			events = append(events, ev)
		}
	}
	items := PendingForEvents(h, s.ACPath, events)
	if len(in.Only) > 0 {
		var f []Item
		for _, it := range items {
			if contains(in.Only, it.Key()) {
				f = append(f, it)
			}
		}
		items = f
	}
	if len(items) == 0 {
		writeErr(w, 400, errors.New("nada para instalar — está tudo em dia"))
		return
	}
	j, err := a.installer.Start(h, s.ACPath, items)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, j)
}

func (a *App) getJob(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.installer.Snapshot()) }

func (a *App) postCancel(w http.ResponseWriter, r *http.Request) {
	a.installer.Cancel()
	writeJSON(w, map[string]bool{"ok": true})
}

// postJoin hands the race invite to Content Manager, which performs the real
// connection exactly as if the player had clicked the invite.
func (a *App) postJoin(w http.ResponseWriter, r *http.Request) {
	var in struct{ EventID string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	h, s, err := a.loadHub(r.Context())
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	ev := h.Event(in.EventID)
	if ev == nil || ev.Locked {
		writeErr(w, 404, errors.New("evento não encontrado ou bloqueado"))
		return
	}
	if st := StatusForEvent(h, s.ACPath, *ev); !st.Ready {
		writeErr(w, 400, fmt.Errorf("ainda faltam %d arquivos para este evento", st.Pending))
		return
	}
	inv, err := ParseInvite(ev.Invite)
	if err != nil {
		inv = Invite{Host: ev.Server.Host, HTTPPort: ev.Server.HTTPPort, Query: url.Values{}}
	}
	if inv.Host == "" {
		writeErr(w, 400, errors.New("o evento não tem race invite"))
		return
	}
	if err := openExternal(inv.JoinLink()); err != nil {
		writeErr(w, 500, fmt.Errorf("não consegui abrir o Content Manager: %w", err))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) postRadmin(w http.ResponseWriter, r *http.Request) {
	if err := openExternal(radminURL); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]string{"url": radminURL})
}

func (a *App) getMods(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	a.mu.Lock()
	h := a.hub
	a.mu.Unlock()
	if h == nil {
		h, _, _ = a.loadHub(r.Context())
	}
	writeJSON(w, map[string]any{"acValid": IsACRoot(s.ACPath), "items": ListMods(s.ACPath, h)})
}

func (a *App) postOpenMod(w http.ResponseWriter, r *http.Request) {
	var in struct{ Kind, ID string }
	if err := readJSON(r, &in); err != nil || !ValidID(in.ID) {
		writeErr(w, 400, errors.New("pedido inválido"))
		return
	}
	s := LoadSettings()
	dir := carDir(s.ACPath, in.ID)
	if in.Kind == "track" {
		dir = trackDir(s.ACPath, in.ID)
	}
	if !exists(dir) {
		writeErr(w, 404, errors.New("pasta não encontrada"))
		return
	}
	if err := openExternal(dir); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) imgLocal(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := LocalPreview(LoadSettings().ACPath, q.Get("kind"), q.Get("id"))
	if p == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "max-age=300")
	http.ServeFile(w, r, p)
}

// ---- host panel ---------------------------------------------------------------

func (a *App) remote(s Settings) *Remote {
	if s.UploadURL == "" || s.UploadKey == "" {
		return nil
	}
	return NewRemote(s.UploadURL, s.UploadKey)
}

func (a *App) adminState(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	tasks, published, busy := a.pub.act.Snapshot()
	pub := ""
	if !published.IsZero() {
		pub = published.Format(time.RFC3339)
	}
	hub := ""
	if s.UploadURL != "" {
		hub = NewRemote(s.UploadURL, "").HubURL()
	}
	writeJSON(w, map[string]any{
		"connected": s.UploadURL != "" && s.UploadKey != "", "role": s.Role, "hostName": s.HostName,
		"baseName": s.BaseName, "hubUrl": hub, "uploadUrl": s.UploadURL,
		"library": a.lib.List(), "tasks": tasks, "busy": busy, "publishedAt": pub,
		"lastError": a.pub.LastError(), "acValid": IsACRoot(s.ACPath),
	})
}

// connect checks the key against the base and remembers who we are there.
func (a *App) connect(ctx context.Context, endpoint, key string) (Identity, error) {
	endpoint = strings.TrimSpace(endpoint)
	if u, err := url.Parse(endpoint); err != nil || !strings.HasSuffix(u.Path, ".php") {
		return Identity{}, errors.New("cole o endereço completo do upload.php (https://seusite/ac/upload.php)")
	}
	id, err := NewRemote(endpoint, key).Ping(ctx)
	if err != nil {
		return id, err
	}
	s := LoadSettings()
	s.UploadURL, s.UploadKey, s.Role, s.HostName, s.BaseName = endpoint, key, id.Role, id.Name, id.Base
	if s.HubSource == "" {
		s.HubSource = NewRemote(endpoint, "").HubURL()
	}
	return id, SaveSettings(s)
}

func (a *App) adminHostCode(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	endpoint, key, err := DecodeHostCode(in.Code)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	id, err := a.connect(r.Context(), endpoint, key)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, id)
}

func (a *App) dlReceiver(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	if s.OwnerKey == "" {
		s.OwnerKey = NewKey()
		_ = SaveSettings(s)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="upload.php"`)
	_, _ = w.Write(ReceiverPHP(s.OwnerKey))
}

func (a *App) adminOwnerConnect(w http.ResponseWriter, r *http.Request) {
	var in struct{ URL string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	if s.OwnerKey == "" {
		writeErr(w, 400, errors.New("baixe o upload.php primeiro (passo 1)"))
		return
	}
	id, err := a.connect(r.Context(), in.URL, s.OwnerKey)
	if err != nil {
		var re *remoteErr
		if errors.As(err, &re) && re.code == 403 {
			err = errors.New("o upload.php do site não é o que você baixou deste PC — baixe de novo e substitua")
		}
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, id)
}

func (a *App) adminDisconnect(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	s.UploadURL, s.UploadKey, s.Role, s.HostName, s.BaseName = "", "", "", "", ""
	if err := SaveSettings(s); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) adminEvents(w http.ResponseWriter, r *http.Request) {
	rm := a.remote(LoadSettings())
	if rm == nil {
		writeJSON(w, map[string]any{"events": []any{}})
		return
	}
	var res struct {
		Events []struct {
			Event
			HasPassword bool `json:"hasPassword"`
		} `json:"events"`
	}
	if err := rm.Call(r.Context(), "my-events", nil, &res); err != nil {
		writeErr(w, 400, err)
		return
	}
	type view struct {
		Event
		Draft EventDraft `json:"draft"`
	}
	out := []view{}
	for _, e := range res.Events {
		out = append(out, view{Event: e.Event, Draft: DraftFromEvent(e.Event, e.HasPassword)})
	}
	writeJSON(w, map[string]any{"events": out, "filesUrl": FilesURLFor(rm.Endpoint)})
}

// DraftItem tells the host where each track/car of the event stands: already
// prepared, available in their own Assetto, or still to be sent.
type DraftItem struct {
	Kind      string   `json:"kind"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	InLibrary bool     `json:"inLibrary"`
	Version   string   `json:"version,omitempty"`
	Size      int64    `json:"size,omitempty"`
	Image     bool     `json:"image"`
	Uploaded  bool     `json:"uploaded"`
	Installed bool     `json:"installed"` // in the host's Assetto Corsa
	Differs   bool     `json:"differs"`   // host's Assetto has other files than the prepared package
	Layouts   []string `json:"layouts,omitempty"`
}

func (a *App) draftItem(acRoot, kind, id string) DraftItem {
	di := DraftItem{Kind: kind, ID: id, Name: id}
	dir := carDir(acRoot, id)
	if kind == "track" {
		dir = trackDir(acRoot, id)
	}
	if ValidID(id) && exists(dir) {
		di.Installed = true
		ui := filepath.Join(dir, "ui", "ui_car.json")
		if kind == "track" {
			ui = filepath.Join(dir, "ui", "ui_track.json")
			if m, _ := filepath.Glob(filepath.Join(dir, "ui", "*", "ui_track.json")); !exists(ui) && len(m) > 0 {
				ui = m[0]
			}
		}
		if b, err := os.ReadFile(ui); err == nil {
			if n := uiField(b, nameRe); n != "" {
				di.Name = n
			}
		}
	}
	if it, ok := a.lib.Get(kind, id); ok {
		di.InLibrary, di.Name, di.Version, di.Size, di.Image, di.Uploaded, di.Layouts = true, it.Name, it.Version, it.Size, it.Image != "", it.Uploaded, it.Layouts
		if di.Installed && len(it.Checks) > 0 && len(CompareChecks(dir, it.Checks)) > 0 {
			di.Differs = true
		}
	}
	return di
}

func (a *App) knownTrack(acRoot string) func(id, layout string) bool {
	return func(id, layout string) bool {
		if !ValidID(id) {
			return false
		}
		if it, ok := a.lib.Get("track", id); ok {
			return layout == "" && len(it.Layouts) == 0 || contains(it.Layouts, layout)
		}
		d := trackDir(acRoot, id)
		if layout == "" {
			return exists(d)
		}
		return ValidID(layout) && exists(filepath.Join(d, layout))
	}
}

func (a *App) adminProbe(w http.ResponseWriter, r *http.Request) {
	var in struct{ Invite string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	inv, err := ParseInvite(in.Invite)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	p, err := ProbeServer(inv, a.knownTrack(s.ACPath))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, p)
}

// adminDraft validates the form as the host fills it in.
func (a *App) adminDraft(w http.ResponseWriter, r *http.Request) {
	var d EventDraft
	if err := readJSON(r, &d); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	cars := []DraftItem{}
	for _, c := range d.Cars {
		cars = append(cars, a.draftItem(s.ACPath, "car", c))
	}
	var track *DraftItem
	if d.Track != "" {
		t := a.draftItem(s.ACPath, "track", d.Track)
		track = &t
	}
	problems := d.Problems(a.lib)
	if problems == nil {
		problems = []string{}
	}
	writeJSON(w, map[string]any{"problems": problems, "track": track, "cars": cars, "acValid": IsACRoot(s.ACPath)})
}

func (a *App) adminPublish(w http.ResponseWriter, r *http.Request) {
	var d EventDraft
	if err := readJSON(r, &d); err != nil {
		writeErr(w, 400, err)
		return
	}
	t, err := a.pub.PublishEvent(d)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, t)
}

func (a *App) adminEventDelete(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	rm := a.remote(LoadSettings())
	if rm == nil {
		writeErr(w, 400, errors.New("painel não ligado a uma base"))
		return
	}
	if err := rm.Call(r.Context(), "event-delete", map[string]string{"id": in.ID}, nil); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// adminDrop receives a zip dragged into the panel as a raw body.
func (a *App) adminDrop(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.Header.Get("X-Filename"))
	if dec, err := url.QueryUnescape(name); err == nil {
		name = dec
	}
	if !strings.EqualFold(filepath.Ext(name), ".zip") {
		writeErr(w, 400, fmt.Errorf("%s: só arquivos .zip (para .7z e .rar, instale no seu Assetto e use \"Usar do meu Assetto\")", name))
		return
	}
	tmpDir := filepath.Join(a.lib.dir, "tmp")
	_ = os.MkdirAll(tmpDir, 0o755)
	f, err := os.CreateTemp(tmpDir, "drop-*.zip")
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	_, err = io.Copy(f, r.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		writeErr(w, 500, fmt.Errorf("falha ao receber %s: %w", name, err))
		return
	}
	writeJSON(w, a.pub.Import(f.Name(), name, true))
}

func (a *App) adminInstalled(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	kind := r.URL.Query().Get("kind")
	if kind != "track" {
		kind = "car"
	}
	writeJSON(w, map[string]any{"acValid": IsACRoot(s.ACPath), "items": ListLocalContent(s.ACPath, kind)})
}

// adminAddInstalled packs mods from the host's own Assetto Corsa — the same
// files their server runs — and adds them to the library.
func (a *App) adminAddInstalled(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Items []struct{ Kind, ID string } `json:"items"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	var tasks []*Task
	for _, it := range in.Items {
		it := it
		if !ValidID(it.ID) || (it.Kind != "car" && it.Kind != "track") {
			continue
		}
		t := a.pub.act.Add("import", "Preparando "+it.ID)
		tasks = append(tasks, t)
		a.pub.act.Enqueue(func() {
			a.pub.act.Set(t, func(t *Task) { t.State = "running" })
			zipPath, err := ZipInstalled(s.ACPath, it.Kind, it.ID, filepath.Join(a.lib.dir, "tmp"))
			if err == nil {
				var results []ImportResult
				results, err = a.lib.Import(zipPath)
				os.Remove(zipPath)
				a.pub.act.Set(t, func(t *Task) { t.Detail = summarize(results) })
			}
			a.pub.act.Set(t, func(t *Task) {
				if err != nil {
					t.State, t.Error = "error", err.Error()
				} else {
					t.State = "done"
				}
			})
		})
	}
	writeJSON(w, map[string]any{"tasks": tasks})
}

// ---- owner of the base ----------------------------------------------------------

func (a *App) owner(w http.ResponseWriter) *Remote {
	s := LoadSettings()
	rm := a.remote(s)
	if rm == nil || s.Role != "owner" {
		writeErr(w, 403, errors.New("só o dono da base gerencia hosts"))
		return nil
	}
	return rm
}

func (a *App) adminHosts(w http.ResponseWriter, r *http.Request) {
	rm := a.owner(w)
	if rm == nil {
		return
	}
	var res any
	if err := rm.Call(r.Context(), "host-list", nil, &res); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, res)
}

func (a *App) adminHostAdd(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	rm := a.owner(w)
	if rm == nil {
		return
	}
	var res struct{ ID, Key string }
	if err := rm.Call(r.Context(), "host-add", map[string]string{"name": in.Name}, &res); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"id": res.ID, "code": EncodeHostCode(rm.Endpoint, res.Key)})
}

func (a *App) adminHostRemove(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	rm := a.owner(w)
	if rm == nil {
		return
	}
	if err := rm.Call(r.Context(), "host-remove", map[string]string{"id": in.ID}, nil); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) adminBase(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Description string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	rm := a.owner(w)
	if rm == nil {
		return
	}
	if err := rm.Call(r.Context(), "base-save", map[string]string{"name": in.Name, "description": in.Description}, nil); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	s.BaseName = in.Name
	_ = SaveSettings(s)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) imgLib(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	it, ok := a.lib.Get(q.Get("kind"), q.Get("id"))
	if !ok || it.Image == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "max-age=60")
	http.ServeFile(w, r, a.lib.ImagePath(it))
}

// openExternal opens a URL, custom protocol link or folder with the system
// default handler.
func openExternal(target string) error {
	switch runtime.GOOS {
	case "windows":
		if !strings.Contains(target, "://") {
			return exec.Command("explorer", target).Start()
		}
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
