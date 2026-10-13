package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

// Hub is what upload.php?action=hub publishes: the base and every event
// (race invite) its hosts created. Private events come locked: no invite,
// no network data and no download links until the player gives the password.
type Hub struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	FilesURL    string  `json:"filesUrl,omitempty"` // public folder with the packages
	Events      []Event `json:"events"`
}

type Event struct {
	ID          string         `json:"id"`
	Host        string         `json:"host,omitempty"` // host display name
	HostID      string         `json:"hostId,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Private     bool           `json:"private,omitempty"`
	Locked      bool           `json:"locked,omitempty"` // private and not unlocked yet
	LAN         bool           `json:"lan,omitempty"`
	Radmin      *Radmin        `json:"radmin,omitempty"`
	Invite      string         `json:"invite,omitempty"`
	Server      ServerAddr     `json:"server"`
	Track       ContentEntry   `json:"track"`
	Cars        []ContentEntry `json:"cars"`
	UpdatedAt   string         `json:"updatedAt,omitempty"`
}

// Radmin is the virtual LAN players join before connecting to a LAN event.
type Radmin struct {
	Network  string `json:"network"`
	Password string `json:"password"`
}

type ServerAddr struct {
	Host     string `json:"host,omitempty"`
	HTTPPort int    `json:"httpPort,omitempty"`
}

// ContentEntry is one car or track of an event. ID is the folder name under
// content/cars or content/tracks; File is the package on the base, named by
// its hash so two hosts never overwrite each other's version.
type ContentEntry struct {
	ID      string      `json:"id"`
	Layout  string      `json:"layout,omitempty"`
	Name    string      `json:"name,omitempty"`
	Version string      `json:"version,omitempty"`
	File    string      `json:"file,omitempty"`
	SHA256  string      `json:"sha256,omitempty"`
	Size    int64       `json:"size,omitempty"`
	Image   string      `json:"image,omitempty"` // file name on the base
	Checks  []FileCheck `json:"checks,omitempty"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,127}$`)

// ValidID reports whether s can be used as a content folder name without
// escaping content/cars or content/tracks.
func ValidID(s string) bool {
	return safeID.MatchString(s) && !strings.Contains(s, "..")
}

func (h *Hub) fileURL(name string) string {
	if name == "" || h.FilesURL == "" {
		return ""
	}
	return strings.TrimRight(h.FilesURL, "/") + "/" + url.PathEscape(name)
}

// DownloadURL returns where a package is fetched from.
func (h *Hub) DownloadURL(e ContentEntry) string { return h.fileURL(e.File) }

// ImageURL returns the preview picture of an entry.
func (h *Hub) ImageURL(e ContentEntry) string { return h.fileURL(e.Image) }

func (h *Hub) Event(id string) *Event {
	for i := range h.Events {
		if h.Events[i].ID == id {
			return &h.Events[i]
		}
	}
	return nil
}

// AllowedHost is the only host packages may come from: the base's own site.
func (h *Hub) AllowedHost() string {
	u, err := url.Parse(h.FilesURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func ParseHub(b []byte) (*Hub, error) {
	var h Hub
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("a base respondeu algo inválido: %w", err)
	}
	if h.Events == nil {
		h.Events = []Event{}
	}
	return &h, nil
}

var hubClient = &http.Client{Timeout: 20 * time.Second}

// LoadHub reads the hub from the base link (upload.php?action=hub or a
// hub.json URL) or from a local file.
func LoadHub(src string) (*Hub, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, errors.New("nenhuma base configurada")
	}
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		b, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		return ParseHub(b)
	}
	src = NormalizeHubLink(src)
	resp, err := hubClient.Get(src)
	if err != nil {
		return nil, fmt.Errorf("não foi possível acessar a base: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("a base respondeu HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	h, err := ParseHub(b)
	if err != nil {
		return nil, err
	}
	// The packages live in files/ next to upload.php; trust the link the
	// player configured over what the response says.
	if f := FilesURLFor(src); f != "" {
		h.FilesURL = f
	}
	return h, nil
}

// NormalizeHubLink accepts the link the way people paste it: the upload.php
// address, with or without ?action=hub.
func NormalizeHubLink(src string) string {
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil {
		return src
	}
	if strings.HasSuffix(u.Path, ".php") {
		q := u.Query()
		q.Set("action", "hub")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// FilesURLFor derives https://site/ac/files from https://site/ac/upload.php.
func FilesURLFor(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" || !strings.HasSuffix(u.Path, ".php") {
		return ""
	}
	u.Path = path.Join(path.Dir(u.Path), "files")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
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

func (h *Hub) CMContentFor(e Event) CMContent {
	c := CMContent{Cars: map[string]CMEntry{}}
	for _, car := range e.Cars {
		c.Cars[car.ID] = CMEntry{URL: h.DownloadURL(car), Version: car.Version}
	}
	c.Track = &CMEntry{URL: h.DownloadURL(e.Track), Version: e.Track.Version}
	return c
}
