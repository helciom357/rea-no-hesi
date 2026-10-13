package main

import (
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
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

type App struct {
	token     string
	installer Installer
	lib       *Library
	pub       *Publisher
	addr      string

	mu  sync.Mutex
	hub *Hub // last hub loaded for the player view
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
	mux.Handle("GET /dl/content.json", a.guardQuery(http.HandlerFunc(a.dlContentJSON)))

	api := http.NewServeMux()
	api.HandleFunc("GET /api/state", a.getState)
	api.HandleFunc("POST /api/settings", a.postSettings)
	api.HandleFunc("GET /api/hub", a.getHub)
	api.HandleFunc("GET /api/live", a.getLive)
	api.HandleFunc("POST /api/install", a.postInstall)
	api.HandleFunc("GET /api/job", a.getJob)
	api.HandleFunc("POST /api/job/cancel", a.postCancel)
	api.HandleFunc("POST /api/join", a.postJoin)
	api.HandleFunc("GET /api/mods", a.getMods)
	api.HandleFunc("POST /api/mods/open", a.postOpenMod)

	api.HandleFunc("GET /api/admin/state", a.adminState)
	api.HandleFunc("PUT /api/admin/base", a.adminPutBase)
	api.HandleFunc("POST /api/admin/drop", a.adminDrop)
	api.HandleFunc("POST /api/admin/library/remove", a.adminRemove)
	api.HandleFunc("GET /api/admin/installed", a.adminInstalled)
	api.HandleFunc("POST /api/admin/installed/add", a.adminAddInstalled)
	api.HandleFunc("POST /api/admin/connection", a.adminConnection)
	api.HandleFunc("POST /api/admin/publish", a.adminPublish)
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
		"isAdmin":   s.UploadURL != "",
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

func (a *App) loadHub() (*Hub, Settings, error) {
	s := LoadSettings()
	h, err := LoadHub(s.EffectiveHub())
	if err != nil {
		return nil, s, err
	}
	a.mu.Lock()
	a.hub = h
	a.mu.Unlock()
	return h, s, nil
}

type modeSummary struct {
	Mode
	Pending int `json:"pending"`
}

func (a *App) getHub(w http.ResponseWriter, r *http.Request) {
	h, s, err := a.loadHub()
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	statuses := map[string]RoomStatus{}
	for _, room := range h.Rooms {
		statuses[room.ID] = StatusForRoom(h, s.ACPath, room)
	}
	modes := []modeSummary{}
	for _, m := range h.Modes {
		var rooms []Room
		for _, room := range h.Rooms {
			if room.ModeID == m.ID {
				rooms = append(rooms, room)
			}
		}
		modes = append(modes, modeSummary{Mode: m, Pending: len(PendingForRooms(h, s.ACPath, rooms))})
	}
	writeJSON(w, map[string]any{"hub": h, "statuses": statuses, "modes": modes, "acValid": IsACRoot(s.ACPath)})
}

func (a *App) getLive(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	h := a.hub
	a.mu.Unlock()
	if h == nil {
		writeJSON(w, map[string]LiveInfo{})
		return
	}
	writeJSON(w, FetchLiveAll(h.Rooms))
}

func (a *App) postInstall(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RoomIDs []string `json:"roomIds"`
		ModeID  string   `json:"modeId"`
		Only    []string `json:"only"` // optional item keys (kind:id)
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	h, s, err := a.loadHub()
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var rooms []Room
	all := len(in.RoomIDs) == 0 && in.ModeID == ""
	for _, room := range h.Rooms {
		if all || (in.ModeID != "" && room.ModeID == in.ModeID) || contains(in.RoomIDs, room.ID) {
			rooms = append(rooms, room)
		}
	}
	items := PendingForRooms(h, s.ACPath, rooms)
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

// postJoin hands the server to Content Manager, which performs the real
// connection exactly as if the player had clicked Join there.
func (a *App) postJoin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RoomID string `json:"roomId"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	h, s, err := a.loadHub()
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	room := h.Room(in.RoomID)
	if room == nil {
		writeErr(w, 404, errors.New("sala não encontrada"))
		return
	}
	if st := StatusForRoom(h, s.ACPath, *room); !st.Ready {
		writeErr(w, 400, fmt.Errorf("ainda faltam %d itens para esta sala", st.Pending))
		return
	}
	link := JoinLink(room.Server)
	if err := openExternal(link); err != nil {
		writeErr(w, 500, fmt.Errorf("não consegui abrir o Content Manager: %w", err))
		return
	}
	writeJSON(w, map[string]string{"link": link})
}

func JoinLink(s ServerAddr) string {
	q := url.Values{}
	q.Set("ip", s.Host)
	q.Set("httpPort", strconv.Itoa(s.HTTPPort))
	return "acmanager://race/online/join?" + q.Encode()
}

func (a *App) getMods(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	a.mu.Lock()
	h := a.hub
	a.mu.Unlock()
	if h == nil {
		h, _, _ = a.loadHub()
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

// ---- admin --------------------------------------------------------------------

type adminRoomView struct {
	AdminRoom
	Problems []string `json:"problems"`
}

func (a *App) adminState(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	base, err := LoadAdminBase(s.AdminBase)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	rooms := []adminRoomView{}
	for _, room := range base.Rooms {
		p := RoomProblems(room, a.lib)
		if p == nil {
			p = []string{}
		}
		rooms = append(rooms, adminRoomView{room, p})
	}
	tasks, published, busy := a.pub.act.Snapshot()
	conn := map[string]any{"url": s.UploadURL, "configured": s.UploadURL != "" && s.UploadKey != ""}
	if s.UploadURL != "" {
		rm := NewRemote(s.UploadURL, "")
		conn["hubUrl"] = rm.HubURL()
		conn["filesUrl"] = rm.FilesURL()
	}
	pub := ""
	if !published.IsZero() {
		pub = published.Format(time.RFC3339)
	}
	writeJSON(w, map[string]any{
		"name": base.Name, "description": base.Description, "rooms": rooms,
		"library": a.lib.List(), "connection": conn,
		"tasks": tasks, "busy": busy, "publishedAt": pub, "publishError": a.pub.LastError(),
		"acValid": IsACRoot(s.ACPath),
	})
}

func (a *App) adminPutBase(w http.ResponseWriter, r *http.Request) {
	var in AdminBase
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	if err := SaveAdminBase(s.AdminBase, &in); err != nil {
		writeErr(w, 500, err)
		return
	}
	a.pub.SchedulePublish()
	writeJSON(w, map[string]any{"rooms": in.Rooms})
}

// adminDrop receives a zip dragged into the panel as a raw body.
func (a *App) adminDrop(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.Header.Get("X-Filename"))
	if dec, err := url.QueryUnescape(name); err == nil {
		name = dec
	}
	if !strings.EqualFold(filepath.Ext(name), ".zip") {
		writeErr(w, 400, fmt.Errorf("%s: só arquivos .zip (para .7z e .rar, instale no Assetto e use \"Adicionar do meu Assetto\")", name))
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
	t := a.pub.Import(f.Name(), name, true)
	writeJSON(w, t)
}

func (a *App) adminRemove(w http.ResponseWriter, r *http.Request) {
	var in struct{ Kind, ID string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := a.lib.Remove(in.Kind, in.ID); err != nil {
		writeErr(w, 400, err)
		return
	}
	a.pub.SchedulePublish()
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) adminInstalled(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	kind := r.URL.Query().Get("kind")
	if kind != "track" {
		kind = "car"
	}
	writeJSON(w, map[string]any{"acValid": IsACRoot(s.ACPath), "items": ListLocalContent(s.ACPath, kind)})
}

func (a *App) adminAddInstalled(w http.ResponseWriter, r *http.Request) {
	var in struct{ Kind, ID string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	t := a.pub.act.Add("import", "Copiando "+in.ID+" do seu Assetto")
	a.pub.act.Enqueue(func() {
		a.pub.act.Set(t, func(t *Task) { t.State = "running" })
		zipPath, err := ZipInstalled(s.ACPath, in.Kind, in.ID, filepath.Join(a.lib.dir, "tmp"))
		if err != nil {
			a.pub.act.Set(t, func(t *Task) { t.State, t.Error = "error", err.Error() })
			return
		}
		a.pub.act.Set(t, func(t *Task) { t.State = "done" })
		a.pub.Import(zipPath, in.ID, true)
	})
	writeJSON(w, t)
}

func (a *App) adminConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL      string `json:"url"`
		NewKey   bool   `json:"newKey"`
		TestOnly bool   `json:"testOnly"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	if in.NewKey || s.UploadKey == "" {
		s.UploadKey = NewKey()
	}
	if !in.TestOnly {
		s.UploadURL = strings.TrimSpace(in.URL)
	}
	if err := SaveSettings(s); err != nil {
		writeErr(w, 500, err)
		return
	}
	if s.UploadURL == "" {
		writeJSON(w, map[string]any{"ok": false})
		return
	}
	rm := NewRemote(s.UploadURL, s.UploadKey)
	if err := rm.Ping(r.Context()); err != nil {
		writeErr(w, 400, err)
		return
	}
	a.pub.SyncAll()
	writeJSON(w, map[string]any{"ok": true, "hubUrl": rm.HubURL()})
}

func (a *App) adminPublish(w http.ResponseWriter, r *http.Request) {
	a.pub.SyncAll()
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

func (a *App) dlReceiver(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	if s.UploadKey == "" {
		s.UploadKey = NewKey()
		_ = SaveSettings(s)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="upload.php"`)
	_, _ = w.Write(ReceiverPHP(s.UploadKey))
}

func (a *App) dlContentJSON(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	base, err := LoadAdminBase(s.AdminBase)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	files := ""
	if s.UploadURL != "" {
		files = NewRemote(s.UploadURL, "").FilesURL()
	}
	b, err := CMContentJSON(BuildHub(base, a.lib, files), r.URL.Query().Get("room"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="content.json"`)
	_, _ = w.Write(b)
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
