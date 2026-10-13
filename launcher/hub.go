package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Hub is the published description of the whole base: game modes, rooms and
// the exact content (with version and download link) each room requires.
// The admin panel edits it as base.json and exports it as hub.json.
type Hub struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	DownloadBase string   `json:"downloadBase,omitempty"` // URL prefix used for packages without an explicit URL
	AllowedHosts []string `json:"allowedHosts"`           // hosts the launcher is allowed to download from
	Modes        []Mode   `json:"modes"`
	Rooms        []Room   `json:"rooms"`
}

type Mode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Room struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ModeID      string         `json:"modeId"`
	Description string         `json:"description,omitempty"`
	Server      ServerAddr     `json:"server"`
	Track       ContentEntry   `json:"track"`
	Cars        []ContentEntry `json:"cars"`
}

type ServerAddr struct {
	Host     string `json:"host"`
	TCPPort  int    `json:"tcpPort"`
	HTTPPort int    `json:"httpPort"`
	Password bool   `json:"password,omitempty"`
}

// ContentEntry is one car or track. ID is the folder name under content/cars
// or content/tracks. Layout is only used for tracks with several layouts.
type ContentEntry struct {
	ID      string `json:"id"`
	Layout  string `json:"layout,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	URL     string `json:"url,omitempty"`
	File    string `json:"file,omitempty"` // package file name, combined with DownloadBase when URL is empty
	SHA256  string `json:"sha256,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,127}$`)

// ValidID reports whether s can be used as a content folder name without
// escaping content/cars or content/tracks.
func ValidID(s string) bool {
	return safeID.MatchString(s) && !strings.Contains(s, "..")
}

// DownloadURL returns the URL a package is fetched from.
func (h *Hub) DownloadURL(e ContentEntry) string {
	if e.URL != "" {
		return e.URL
	}
	if e.File != "" && h.DownloadBase != "" {
		return strings.TrimRight(h.DownloadBase, "/") + "/" + url.PathEscape(e.File)
	}
	return ""
}

func (h *Hub) Mode(id string) *Mode {
	for i := range h.Modes {
		if h.Modes[i].ID == id {
			return &h.Modes[i]
		}
	}
	return nil
}

func (h *Hub) Room(id string) *Room {
	for i := range h.Rooms {
		if h.Rooms[i].ID == id {
			return &h.Rooms[i]
		}
	}
	return nil
}

// Validate returns every problem found, so the admin can fix them in one pass.
func (h *Hub) Validate() []string {
	p := []string{}
	if strings.TrimSpace(h.Name) == "" {
		p = append(p, "A base precisa de um nome.")
	}
	modes := map[string]bool{}
	for _, m := range h.Modes {
		if !ValidID(m.ID) {
			p = append(p, fmt.Sprintf("Modo %q: id inválido (use letras, números, _ - .).", m.Name))
		}
		if modes[m.ID] {
			p = append(p, fmt.Sprintf("Modo %q repetido.", m.ID))
		}
		modes[m.ID] = true
	}
	rooms := map[string]bool{}
	for _, r := range h.Rooms {
		label := r.Name
		if label == "" {
			label = r.ID
		}
		if !ValidID(r.ID) {
			p = append(p, fmt.Sprintf("Sala %q: id inválido.", label))
		}
		if rooms[r.ID] {
			p = append(p, fmt.Sprintf("Sala %q repetida.", r.ID))
		}
		rooms[r.ID] = true
		if !modes[r.ModeID] {
			p = append(p, fmt.Sprintf("Sala %q: modo %q não existe.", label, r.ModeID))
		}
		if r.Server.Host == "" || r.Server.HTTPPort == 0 {
			p = append(p, fmt.Sprintf("Sala %q: falta o endereço ou a porta HTTP do servidor.", label))
		}
		if !ValidID(r.Track.ID) {
			p = append(p, fmt.Sprintf("Sala %q: pista sem id válido.", label))
		} else if r.Track.Layout != "" && !ValidID(r.Track.Layout) {
			p = append(p, fmt.Sprintf("Sala %q: layout da pista inválido.", label))
		}
		if len(r.Cars) == 0 {
			p = append(p, fmt.Sprintf("Sala %q: nenhum carro.", label))
		}
		entries := append([]ContentEntry{r.Track}, r.Cars...)
		for i, e := range entries {
			kind := "Carro"
			if i == 0 {
				kind = "Pista"
			}
			if i > 0 && !ValidID(e.ID) {
				p = append(p, fmt.Sprintf("Sala %q: carro com id inválido %q.", label, e.ID))
			}
			if e.Version == "" {
				p = append(p, fmt.Sprintf("Sala %q: %s %q sem versão — sem versão o launcher não detecta atualização.", label, strings.ToLower(kind), e.ID))
			}
			if h.DownloadURL(e) == "" {
				p = append(p, fmt.Sprintf("Sala %q: %s %q sem link de download.", label, strings.ToLower(kind), e.ID))
			}
		}
	}
	return p
}

func LoadHubFile(path string) (*Hub, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseHub(b)
}

func ParseHub(b []byte) (*Hub, error) {
	var h Hub
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("hub inválido: %w", err)
	}
	return &h, nil
}

// LoadHub reads the hub from a local path or an http(s) URL.
func LoadHub(src string) (*Hub, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, errors.New("nenhuma base configurada")
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		c := &http.Client{Timeout: 20 * time.Second}
		resp, err := c.Get(src)
		if err != nil {
			return nil, fmt.Errorf("não foi possível baixar a base: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("a base respondeu HTTP %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return nil, err
		}
		return ParseHub(b)
	}
	return LoadHubFile(src)
}

// CMContent is the cm_content/content.json format read by AssettoServer and
// served to Content Manager through /api/details.
type CMContent struct {
	Cars  map[string]CMEntry `json:"cars,omitempty"`
	Track *CMEntry           `json:"track,omitempty"`
}

type CMEntry struct {
	URL     string `json:"url,omitempty"`
	Version string `json:"version,omitempty"`
}

func (h *Hub) CMContentFor(r Room) CMContent {
	c := CMContent{Cars: map[string]CMEntry{}}
	for _, car := range r.Cars {
		c.Cars[car.ID] = CMEntry{URL: h.DownloadURL(car), Version: car.Version}
	}
	c.Track = &CMEntry{URL: h.DownloadURL(r.Track), Version: r.Track.Version}
	return c
}
