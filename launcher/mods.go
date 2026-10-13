package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ModEntry is one card in "Meus mods": something installed, something the
// base requires, or both.
type ModEntry struct {
	Kind      string   `json:"kind"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Installed bool     `json:"installed"`
	Size      int64    `json:"size"`
	Version   string   `json:"version,omitempty"`
	Required  bool     `json:"required"`
	State     string   `json:"state,omitempty"` // ok, missing, broken, outdated (only when required)
	Rooms     []string `json:"rooms,omitempty"` // rooms that need it
	Image     string   `json:"image,omitempty"` // remote image from the base
	HasLocal  bool     `json:"hasLocalImage"`
	Problems  []string `json:"problems,omitempty"`
}

type sizeCacheEntry struct {
	mod  time.Time
	size int64
}

var (
	sizeMu    sync.Mutex
	sizeCache = map[string]sizeCacheEntry{}
)

// dirSize walks a mod folder; results are cached until the folder changes.
func dirSize(dir string) int64 {
	st, err := os.Stat(dir)
	if err != nil {
		return 0
	}
	sizeMu.Lock()
	c, ok := sizeCache[dir]
	sizeMu.Unlock()
	if ok && c.mod.Equal(st.ModTime()) {
		return c.size
	}
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	sizeMu.Lock()
	sizeCache[dir] = sizeCacheEntry{st.ModTime(), total}
	sizeMu.Unlock()
	return total
}

// LocalPreview finds a picture for an installed car or track.
func LocalPreview(acRoot, kind, id string) string {
	if !ValidID(id) {
		return ""
	}
	if kind == "car" {
		skins, _ := os.ReadDir(filepath.Join(carDir(acRoot, id), "skins"))
		for _, s := range skins {
			p := filepath.Join(carDir(acRoot, id), "skins", s.Name(), "preview.jpg")
			if s.IsDir() && exists(p) {
				return p
			}
		}
		return ""
	}
	p := filepath.Join(trackDir(acRoot, id), "ui", "preview.png")
	if exists(p) {
		return p
	}
	m, _ := filepath.Glob(filepath.Join(trackDir(acRoot, id), "ui", "*", "preview.png"))
	if len(m) > 0 {
		return m[0]
	}
	return ""
}

func ListMods(acRoot string, h *Hub) []ModEntry {
	byKey := map[string]*ModEntry{}
	var order []string
	get := func(kind, id string) *ModEntry {
		k := kind + ":" + id
		if e := byKey[k]; e != nil {
			return e
		}
		e := &ModEntry{Kind: kind, ID: id, Name: id}
		byKey[k] = e
		order = append(order, k)
		return e
	}
	// Installed content, sized in parallel.
	var installed []*ModEntry
	for _, kind := range []string{"car", "track"} {
		for _, lc := range ListLocalContent(acRoot, kind) {
			e := get(kind, lc.ID)
			e.Installed = true
			e.Version = lc.Version
			if lc.Name != "" {
				e.Name = lc.Name
			}
			installed = append(installed, e)
		}
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, e := range installed {
		wg.Add(1)
		go func(e *ModEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dir := carDir(acRoot, e.ID)
			if e.Kind == "track" {
				dir = trackDir(acRoot, e.ID)
			}
			e.Size = dirSize(dir)
			e.HasLocal = LocalPreview(acRoot, e.Kind, e.ID) != ""
		}(e)
	}
	wg.Wait()
	// Everything the base needs, with its state on this PC.
	if h != nil {
		for _, r := range h.Rooms {
			st := StatusForRoom(h, acRoot, r)
			for _, it := range st.Items {
				e := get(it.Kind, it.ID)
				e.Required = true
				e.State = it.State
				e.Problems = it.Problems
				if it.Name != "" {
					e.Name = it.Name
				}
				if e.Image == "" {
					e.Image = imageFor(r, it)
				}
				if !e.Installed {
					e.Size = it.Size
				}
				if !contains(e.Rooms, r.Name) {
					e.Rooms = append(e.Rooms, r.Name)
				}
			}
		}
	}
	out := make([]ModEntry, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	// Problems first, then required, then the rest by name.
	rank := func(e ModEntry) int {
		switch {
		case e.Required && e.State != "ok":
			return 0
		case e.Required:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if ra, rb := rank(out[a]), rank(out[b]); ra != rb {
			return ra < rb
		}
		return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name)
	})
	return out
}

func imageFor(r Room, it Item) string {
	if it.Kind == "track" {
		return r.Track.Image
	}
	for _, c := range r.Cars {
		if c.ID == it.ID {
			return c.Image
		}
	}
	return ""
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
