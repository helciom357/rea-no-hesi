package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

//go:embed web
var webFS embed.FS

type App struct {
	token     string
	installer Installer
	mu        sync.Mutex
	hub       *Hub // last hub loaded for the player view
	addr      string
}

func NewApp() *App {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &App{token: hex.EncodeToString(b)}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	mux.HandleFunc("GET /{$}", a.page("player.html"))
	mux.HandleFunc("GET /admin", a.page("admin.html"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	api := http.NewServeMux()
	api.HandleFunc("GET /api/state", a.getState)
	api.HandleFunc("POST /api/settings", a.postSettings)
	api.HandleFunc("GET /api/hub", a.getHub)
	api.HandleFunc("GET /api/live", a.getLive)
	api.HandleFunc("POST /api/install", a.postInstall)
	api.HandleFunc("GET /api/job", a.getJob)
	api.HandleFunc("POST /api/job/cancel", a.postCancel)
	api.HandleFunc("POST /api/join", a.postJoin)
	api.HandleFunc("GET /api/admin/base", a.getBase)
	api.HandleFunc("PUT /api/admin/base", a.putBase)
	api.HandleFunc("GET /api/admin/packages", a.getPackages)
	api.HandleFunc("GET /api/admin/local", a.getLocal)
	api.HandleFunc("POST /api/admin/package", a.postPackage)
	api.HandleFunc("POST /api/admin/export", a.postExport)
	api.HandleFunc("POST /api/admin/open", a.postOpen)
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
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
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<20)).Decode(v)
}

// ---- player -----------------------------------------------------------------

func (a *App) getState(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	writeJSON(w, map[string]any{
		"version":  Version,
		"settings": s,
		"acValid":  IsACRoot(s.ACPath),
		"detected": DetectACPath(),
	})
}

func (a *App) postSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	for k, v := range in {
		v = strings.TrimSpace(v)
		switch k {
		case "acPath":
			s.ACPath = v
		case "hubSource":
			s.HubSource = v
		case "adminBase":
			s.AdminBase = v
		case "packageDir":
			s.PackageDir = v
		case "exportDir":
			s.ExportDir = v
		}
	}
	if err := SaveSettings(s); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"settings": s, "acValid": IsACRoot(s.ACPath)})
}

type modeSummary struct {
	Mode
	Rooms     []string `json:"rooms"`
	Pending   int      `json:"pending"`
	PendingMB float64  `json:"pendingMB"`
}

func (a *App) getHub(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	h, err := LoadHub(s.HubSource)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	a.mu.Lock()
	a.hub = h
	a.mu.Unlock()
	statuses := map[string]RoomStatus{}
	for _, room := range h.Rooms {
		statuses[room.ID] = StatusForRoom(h, s.ACPath, room)
	}
	var modes []modeSummary
	for _, m := range h.Modes {
		ms := modeSummary{Mode: m, Rooms: []string{}}
		var rooms []Room
		for _, room := range h.Rooms {
			if room.ModeID == m.ID {
				ms.Rooms = append(ms.Rooms, room.ID)
				rooms = append(rooms, room)
			}
		}
		for _, it := range PendingForRooms(h, s.ACPath, rooms) {
			ms.Pending++
			ms.PendingMB += float64(it.Size) / (1 << 20)
		}
		modes = append(modes, ms)
	}
	writeJSON(w, map[string]any{
		"hub": h, "statuses": statuses, "modes": modes,
		"acValid": IsACRoot(s.ACPath), "problems": h.Validate(),
	})
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
	s := LoadSettings()
	h, err := LoadHub(s.HubSource)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var rooms []Room
	for _, room := range h.Rooms {
		if room.ModeID == in.ModeID && in.ModeID != "" {
			rooms = append(rooms, room)
			continue
		}
		for _, id := range in.RoomIDs {
			if room.ID == id {
				rooms = append(rooms, room)
			}
		}
	}
	items := PendingForRooms(h, s.ACPath, rooms)
	if len(in.Only) > 0 {
		keep := map[string]bool{}
		for _, k := range in.Only {
			keep[k] = true
		}
		var f []Item
		for _, it := range items {
			if keep[it.Key()] {
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
// connection (and its own checks) exactly as if the player had clicked Join.
func (a *App) postJoin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RoomID string `json:"roomId"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	h, err := LoadHub(s.HubSource)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	room := h.Room(in.RoomID)
	if room == nil {
		writeErr(w, 404, errors.New("sala não encontrada"))
		return
	}
	st := StatusForRoom(h, s.ACPath, *room)
	if !st.Ready {
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
	q.Set("httpPort", fmt.Sprint(s.HTTPPort))
	return "acmanager://race/online/join?" + q.Encode()
}

// ---- admin ------------------------------------------------------------------

func (a *App) getBase(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	h, err := LoadBase(s.AdminBase)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"base": h, "problems": h.Validate(), "settings": s, "saved": exists(s.AdminBase)})
}

func (a *App) putBase(w http.ResponseWriter, r *http.Request) {
	var h Hub
	if err := readJSON(r, &h); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	if err := SaveBase(s.AdminBase, &h); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"problems": h.Validate()})
}

func (a *App) getPackages(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	writeJSON(w, map[string]any{"dir": s.PackageDir, "packages": ListPackages(s.PackageDir)})
}

func (a *App) getLocal(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	kind := r.URL.Query().Get("kind")
	if kind != "track" {
		kind = "car"
	}
	writeJSON(w, map[string]any{"acValid": IsACRoot(s.ACPath), "items": ListLocalContent(s.ACPath, kind)})
}

func (a *App) postPackage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind, ID, Version string
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	info, err := BuildPackage(s.ACPath, s.PackageDir, in.Kind, in.ID, strings.TrimSpace(in.Version))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, info)
}

func (a *App) postExport(w http.ResponseWriter, r *http.Request) {
	s := LoadSettings()
	h, err := LoadBase(s.AdminBase)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	res, err := Export(h, s.ExportDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, res)
}

func (a *App) postOpen(w http.ResponseWriter, r *http.Request) {
	var in struct{ Which string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s := LoadSettings()
	dir := map[string]string{"packages": s.PackageDir, "export": s.ExportDir, "base": filepath.Dir(s.AdminBase), "ac": s.ACPath}[in.Which]
	if dir == "" {
		writeErr(w, 400, errors.New("pasta desconhecida"))
		return
	}
	if in.Which != "ac" {
		_ = os.MkdirAll(dir, 0o755)
	}
	if err := openExternal(dir); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]string{"dir": dir})
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
