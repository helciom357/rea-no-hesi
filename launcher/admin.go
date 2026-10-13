package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// AdminBase is what the admin edits: rooms that point at library items by id.
// It is turned into the published Hub (with versions, links and hashes) by
// BuildHub.
type AdminBase struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Rooms       []AdminRoom `json:"rooms"`
}

type AdminRoom struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Mode        string   `json:"mode"` // free text: "Tráfego pesado", "Racha"…
	Description string   `json:"description,omitempty"`
	Host        string   `json:"host"`
	HTTPPort    int      `json:"httpPort"`
	Track       string   `json:"track"`
	Layout      string   `json:"layout,omitempty"`
	Cars        []string `json:"cars"`
}

func LoadAdminBase(p string) (*AdminBase, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return &AdminBase{Name: "Minha base", Rooms: []AdminRoom{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var a AdminBase
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("base.json inválido: %w", err)
	}
	if a.Rooms == nil {
		a.Rooms = []AdminRoom{}
	}
	return &a, nil
}

func SaveAdminBase(p string, a *AdminBase) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	a.normalize()
	b, _ := json.MarshalIndent(a, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// normalize gives every room a stable, unique id derived from its name.
func (a *AdminBase) normalize() {
	taken := map[string]bool{}
	for i := range a.Rooms {
		r := &a.Rooms[i]
		r.Name = strings.TrimSpace(r.Name)
		r.Mode = strings.TrimSpace(r.Mode)
		r.Host = strings.TrimSpace(r.Host)
		if r.Cars == nil {
			r.Cars = []string{}
		}
		if r.ID == "" || !ValidID(r.ID) || taken[r.ID] {
			base := Slug(r.Name)
			id := base
			for n := 2; taken[id]; n++ {
				id = fmt.Sprintf("%s-%d", base, n)
			}
			r.ID = id
		}
		taken[r.ID] = true
	}
}

var (
	nonSlug = regexp.MustCompile(`[^a-z0-9]+`)
	accents = strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "é", "e", "ê", "e", "è", "e", "ë", "e",
		"í", "i", "î", "i", "ì", "i", "ï", "i", "ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o",
		"ú", "u", "û", "u", "ù", "u", "ü", "u", "ç", "c", "ñ", "n")
)

func Slug(s string) string {
	t := accents.Replace(strings.ToLower(s))
	t = strings.Trim(nonSlug.ReplaceAllString(t, "-"), "-")
	if len(t) > 60 {
		t = strings.Trim(t[:60], "-")
	}
	if t == "" {
		t = "sala"
	}
	return t
}

// RoomProblems explains why a room cannot be published yet.
func RoomProblems(r AdminRoom, lib *Library) []string {
	var p []string
	if r.Name == "" {
		p = append(p, "dê um nome à sala")
	}
	if r.Mode == "" {
		p = append(p, "escolha o modo de jogo")
	}
	if r.Host == "" || r.HTTPPort == 0 {
		p = append(p, "informe o endereço do servidor (IP:porta HTTP)")
	}
	if r.Track == "" {
		p = append(p, "escolha a pista")
	} else if it, ok := lib.Get("track", r.Track); !ok {
		p = append(p, "a pista "+r.Track+" não está mais no conteúdo")
	} else if len(it.Layouts) > 1 && r.Layout == "" {
		p = append(p, "escolha o traçado da pista")
	}
	if len(r.Cars) == 0 {
		p = append(p, "adicione ao menos um carro")
	}
	for _, c := range r.Cars {
		if _, ok := lib.Get("car", c); !ok {
			p = append(p, "o carro "+c+" não está mais no conteúdo")
		}
	}
	return p
}

// BuildHub turns the admin base into the published hub. Rooms with problems
// are left out so players never see a room they cannot join.
func BuildHub(a *AdminBase, lib *Library, filesURL string) *Hub {
	h := &Hub{Name: a.Name, Description: a.Description, DownloadBase: strings.TrimRight(filesURL, "/"),
		AllowedHosts: []string{}, Modes: []Mode{}, Rooms: []Room{}}
	if u, err := url.Parse(filesURL); err == nil && u.Hostname() != "" {
		h.AllowedHosts = []string{u.Hostname()}
	}
	image := func(it LibItem) string {
		if it.Image == "" || h.DownloadBase == "" {
			return ""
		}
		return h.DownloadBase + "/" + url.PathEscape(it.RemoteImage())
	}
	entry := func(it LibItem) ContentEntry {
		return ContentEntry{ID: it.ID, Name: it.Name, Version: it.Version, File: it.File,
			SHA256: it.SHA256, Size: it.Size, Image: image(it)}
	}
	modes := map[string]bool{}
	for _, r := range a.Rooms {
		if len(RoomProblems(r, lib)) > 0 {
			continue
		}
		modeID := Slug(r.Mode)
		if !modes[modeID] {
			modes[modeID] = true
			h.Modes = append(h.Modes, Mode{ID: modeID, Name: r.Mode})
		}
		track, _ := lib.Get("track", r.Track)
		te := entry(track)
		te.Layout = r.Layout
		room := Room{ID: r.ID, Name: r.Name, ModeID: modeID, Description: r.Description,
			Server: ServerAddr{Host: r.Host, HTTPPort: r.HTTPPort}, Track: te, Image: te.Image}
		seen := map[string]bool{}
		for _, c := range r.Cars {
			if seen[c] {
				continue
			}
			seen[c] = true
			it, _ := lib.Get("car", c)
			room.Cars = append(room.Cars, entry(it))
		}
		h.Rooms = append(h.Rooms, room)
	}
	return h
}

// ReferencedItems lists the library items used by publishable rooms.
func ReferencedItems(a *AdminBase, lib *Library) []LibItem {
	var out []LibItem
	seen := map[string]bool{}
	add := func(kind, id string) {
		if it, ok := lib.Get(kind, id); ok && !seen[it.Key()] {
			seen[it.Key()] = true
			out = append(out, it)
		}
	}
	for _, r := range a.Rooms {
		if len(RoomProblems(r, lib)) > 0 {
			continue
		}
		add("track", r.Track)
		for _, c := range r.Cars {
			add("car", c)
		}
	}
	return out
}

// CMContentJSON is the cfg/cm_content/content.json for one room's
// AssettoServer, so Content Manager users get the same download links.
func CMContentJSON(h *Hub, roomID string) ([]byte, error) {
	r := h.Room(roomID)
	if r == nil {
		return nil, errors.New("a sala ainda não está completa")
	}
	return json.MarshalIndent(h.CMContentFor(*r), "", "  ")
}

// ---- content already installed on the admin's PC -----------------------------

type LocalContent struct {
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Version string   `json:"version,omitempty"`
	Layouts []string `json:"layouts,omitempty"`
}

var nameRe = regexp.MustCompile(`"name"\s*:\s*"([^"]*)"`)

func ListLocalContent(acRoot, kind string) []LocalContent {
	sub := "cars"
	if kind == "track" {
		sub = "tracks"
	}
	var out []LocalContent
	entries, _ := os.ReadDir(filepath.Join(acRoot, "content", sub))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		lc := LocalContent{Kind: kind, ID: e.Name()}
		dir := filepath.Join(acRoot, "content", sub, e.Name())
		ui := filepath.Join(dir, "ui", "ui_car.json")
		if kind == "track" {
			ui = filepath.Join(dir, "ui", "ui_track.json")
			if subs, err := os.ReadDir(filepath.Join(dir, "ui")); err == nil {
				for _, s := range subs {
					if s.IsDir() && exists(filepath.Join(dir, "ui", s.Name(), "ui_track.json")) {
						lc.Layouts = append(lc.Layouts, s.Name())
						if !exists(ui) {
							ui = filepath.Join(dir, "ui", s.Name(), "ui_track.json")
						}
					}
				}
			}
		}
		if b, err := os.ReadFile(ui); err == nil {
			lc.Name, lc.Version = uiField(b, nameRe), uiField(b, versionRe)
		}
		out = append(out, lc)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID) })
	return out
}

// ZipInstalled packs content/<cars|tracks>/<id> from the admin's install into
// a temporary zip that then goes through Library.Import like a dropped file.
func ZipInstalled(acRoot, kind, id, tmpDir string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("id inválido: %q", id)
	}
	sub := "cars"
	if kind == "track" {
		sub = "tracks"
	}
	src := filepath.Join(acRoot, "content", sub, id)
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return "", fmt.Errorf("não encontrei %s em content/%s", id, sub)
	}
	_ = os.MkdirAll(tmpDir, 0o755)
	out, err := os.CreateTemp(tmpDir, "installed-*.zip")
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(out)
	prefix := "content/" + sub + "/" + id + "/"
	walkErr := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		info, _ := d.Info()
		hdr, _ := zip.FileInfoHeader(info)
		hdr.Name = prefix + filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		hdr.Modified = time.Now()
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err := closeAll(zw, out, walkErr); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}
