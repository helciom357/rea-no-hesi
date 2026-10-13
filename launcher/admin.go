package main

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// EventDraft is what the host fills in the panel. Track and cars point at
// library items by folder id; BuildEvent turns it into the published Event
// with the exact package (hash, size, file checks) of each one.
type EventDraft struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Private     bool     `json:"private"`
	Password    string   `json:"password,omitempty"` // empty keeps the current one
	HasPassword bool     `json:"hasPassword,omitempty"`
	LAN         bool     `json:"lan"`
	RadminNet   string   `json:"radminNetwork"`
	RadminPass  string   `json:"radminPassword"`
	Invite      string   `json:"invite"`
	Track       string   `json:"track"`
	Layout      string   `json:"layout"`
	Cars        []string `json:"cars"`
}

// DraftFromEvent opens a published event for editing.
func DraftFromEvent(e Event, hasPassword bool) EventDraft {
	d := EventDraft{ID: e.ID, Name: e.Name, Description: e.Description, Private: e.Private, HasPassword: hasPassword,
		LAN: e.LAN, Invite: e.Invite, Track: e.Track.ID, Layout: e.Track.Layout, Cars: []string{}}
	if e.Radmin != nil {
		d.RadminNet, d.RadminPass = e.Radmin.Network, e.Radmin.Password
	}
	for _, c := range e.Cars {
		d.Cars = append(d.Cars, c.ID)
	}
	return d
}

// Problems lists what still keeps the event from being published, in the
// order of the form.
func (d EventDraft) Problems(lib *Library) []string {
	var p []string
	if strings.TrimSpace(d.Name) == "" {
		p = append(p, "dê um nome ao evento")
	}
	if d.Private && d.Password == "" && !d.HasPassword {
		p = append(p, "defina a senha do evento privado")
	}
	if d.LAN && strings.TrimSpace(d.RadminNet) == "" {
		p = append(p, "informe o nome da rede Radmin")
	}
	if _, err := ParseInvite(d.Invite); err != nil {
		p = append(p, err.Error())
	}
	if d.Track == "" {
		p = append(p, "escolha a pista")
	} else if _, ok := lib.Get("track", d.Track); !ok {
		p = append(p, "envie a pista "+d.Track)
	}
	if len(d.Cars) == 0 {
		p = append(p, "adicione ao menos um carro")
	}
	var missing []string
	for _, c := range d.Cars {
		if _, ok := lib.Get("car", c); !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) == 1 {
		p = append(p, "envie o carro "+missing[0])
	} else if len(missing) > 1 {
		p = append(p, fmt.Sprintf("envie %d carros que faltam", len(missing)))
	}
	return p
}

// Items lists the library items the event uses.
func (d EventDraft) Items(lib *Library) []LibItem {
	var out []LibItem
	if it, ok := lib.Get("track", d.Track); ok {
		out = append(out, it)
	}
	seen := map[string]bool{}
	for _, c := range d.Cars {
		if it, ok := lib.Get("car", c); ok && !seen[c] {
			seen[c] = true
			out = append(out, it)
		}
	}
	return out
}

func entryFor(it LibItem) ContentEntry {
	img := ""
	if it.Image != "" {
		img = it.RemoteImage()
	}
	return ContentEntry{ID: it.ID, Name: it.Name, Version: it.Version, File: it.File, SHA256: it.SHA256,
		Size: it.Size, Image: img, Checks: it.Checks}
}

// eventPayload is what upload.php?action=event-save receives.
type eventPayload struct {
	Event
	Password string `json:"password,omitempty"`
}

func BuildEvent(d EventDraft, lib *Library) (eventPayload, error) {
	if p := d.Problems(lib); len(p) > 0 {
		return eventPayload{}, errors.New(p[0])
	}
	in, _ := ParseInvite(d.Invite)
	ev := Event{ID: d.ID, Name: strings.TrimSpace(d.Name), Description: strings.TrimSpace(d.Description),
		Private: d.Private, LAN: d.LAN, Invite: strings.TrimSpace(d.Invite),
		Server: ServerAddr{Host: in.Host, HTTPPort: in.HTTPPort}, Cars: []ContentEntry{}}
	if ev.ID == "" {
		ev.ID = Slug(d.Name) + "-" + NewKey()[:6]
	}
	if d.LAN {
		ev.Radmin = &Radmin{Network: strings.TrimSpace(d.RadminNet), Password: strings.TrimSpace(d.RadminPass)}
	}
	track, _ := lib.Get("track", d.Track)
	ev.Track = entryFor(track)
	ev.Track.Layout = d.Layout
	seen := map[string]bool{}
	for _, c := range d.Cars {
		if seen[c] {
			continue
		}
		seen[c] = true
		it, _ := lib.Get("car", c)
		ev.Cars = append(ev.Cars, entryFor(it))
	}
	pw := ""
	if d.Private {
		pw = d.Password
	}
	return eventPayload{Event: ev, Password: pw}, nil
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
	if len(t) > 50 {
		t = strings.Trim(t[:50], "-")
	}
	if t == "" {
		t = "evento"
	}
	return t
}

// ---- host codes ---------------------------------------------------------------

// A host code is what the owner sends to someone who will create events:
// the base address and that host's own key, in one string to paste.
const hostCodePrefix = "LB1-"

type hostCode struct {
	URL string `json:"u"`
	Key string `json:"k"`
}

func EncodeHostCode(endpoint, key string) string {
	b, _ := json.Marshal(hostCode{URL: endpoint, Key: key})
	return hostCodePrefix + base64.RawURLEncoding.EncodeToString(b)
}

func DecodeHostCode(code string) (endpoint, key string, err error) {
	code = strings.Join(strings.Fields(code), "")
	if !strings.HasPrefix(code, hostCodePrefix) {
		return "", "", errors.New("isso não parece um código de host (começa com LB1-)")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, hostCodePrefix))
	var c hostCode
	if err != nil || json.Unmarshal(b, &c) != nil || c.URL == "" || c.Key == "" {
		return "", "", errors.New("código de host incompleto — copie de novo")
	}
	return c.URL, c.Key, nil
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
