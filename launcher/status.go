package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Item is one car or track a room needs, with its state on this PC.
type Item struct {
	Kind         string   `json:"kind"` // "car" or "track"
	ID           string   `json:"id"`
	Layout       string   `json:"layout,omitempty"`
	Name         string   `json:"name,omitempty"`
	State        string   `json:"state"` // ok, missing, broken, outdated
	LocalVersion string   `json:"localVersion,omitempty"`
	Version      string   `json:"version,omitempty"`
	URL          string   `json:"url,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	Size         int64    `json:"size,omitempty"`
	Problems     []string `json:"problems,omitempty"`
}

func (i Item) Key() string { return i.Kind + ":" + i.ID }

type RoomStatus struct {
	RoomID    string  `json:"roomId"`
	Ready     bool    `json:"ready"`
	Pending   int     `json:"pending"`
	PendingMB float64 `json:"pendingMB"`
	Items     []Item  `json:"items"`
}

func classify(local LocalState, e ContentEntry) (string, LocalState) {
	switch {
	case !local.Found:
		return "missing", local
	case !local.Healthy:
		return "broken", local
	case NewerVersion(e.Version, local.Version):
		return "outdated", local
	default:
		return "ok", local
	}
}

func StatusForRoom(h *Hub, acRoot string, r Room) RoomStatus {
	rs := RoomStatus{RoomID: r.ID, Ready: true}
	add := func(kind string, e ContentEntry, local LocalState) {
		state, l := classify(local, e)
		it := Item{Kind: kind, ID: e.ID, Layout: e.Layout, Name: e.Name, State: state,
			LocalVersion: l.Version, Version: e.Version, URL: h.DownloadURL(e),
			SHA256: e.SHA256, Size: e.Size, Problems: l.Problems}
		if state != "ok" {
			rs.Ready = false
			rs.Pending++
			rs.PendingMB += float64(e.Size) / (1 << 20)
		}
		rs.Items = append(rs.Items, it)
	}
	add("track", r.Track, CheckTrack(acRoot, r.Track.ID, r.Track.Layout))
	seen := map[string]bool{}
	for _, c := range r.Cars {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		add("car", c, CheckCar(acRoot, c.ID))
	}
	return rs
}

// PendingForRooms merges the missing items of several rooms, so a whole mode
// can be installed at once without downloading shared content twice.
func PendingForRooms(h *Hub, acRoot string, rooms []Room) []Item {
	var out []Item
	seen := map[string]bool{}
	for _, r := range rooms {
		for _, it := range StatusForRoom(h, acRoot, r).Items {
			if it.State == "ok" || seen[it.Key()] {
				continue
			}
			seen[it.Key()] = true
			out = append(out, it)
		}
	}
	return out
}

// LiveInfo is what the AC/AssettoServer HTTP port reports right now.
type LiveInfo struct {
	Online     bool                `json:"online"`
	Clients    int                 `json:"clients"`
	MaxClients int                 `json:"maxClients"`
	PingMs     int                 `json:"pingMs"`
	Slots      map[string]SlotInfo `json:"slots,omitempty"` // per car model
	Error      string              `json:"error,omitempty"`
}

type SlotInfo struct {
	Total int `json:"total"`
	Free  int `json:"free"`
}

var liveClient = &http.Client{Timeout: 4 * time.Second}

func FetchLive(s ServerAddr) LiveInfo {
	if s.Host == "" || s.HTTPPort == 0 {
		return LiveInfo{Error: "sem endereço"}
	}
	base := fmt.Sprintf("http://%s:%d", s.Host, s.HTTPPort)
	start := time.Now()
	resp, err := liveClient.Get(base + "/INFO")
	if err != nil {
		return LiveInfo{Error: "offline"}
	}
	li := LiveInfo{Online: true, PingMs: int(time.Since(start).Milliseconds())}
	var v struct {
		Clients    int `json:"clients"`
		MaxClients int `json:"maxclients"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v) == nil {
		li.Clients, li.MaxClients = v.Clients, v.MaxClients
	}
	resp.Body.Close()
	// The entry list says how many slots each car has and which are taken.
	if r2, err := liveClient.Get(base + "/JSON%7C0"); err == nil {
		var el struct {
			Cars []struct {
				Model       string `json:"Model"`
				IsConnected bool   `json:"IsConnected"`
			} `json:"Cars"`
		}
		if json.NewDecoder(io.LimitReader(r2.Body, 4<<20)).Decode(&el) == nil && len(el.Cars) > 0 {
			li.Slots = map[string]SlotInfo{}
			for _, c := range el.Cars {
				si := li.Slots[c.Model]
				si.Total++
				if !c.IsConnected {
					si.Free++
				}
				li.Slots[c.Model] = si
			}
		}
		r2.Body.Close()
	}
	return li
}

func FetchLiveAll(rooms []Room) map[string]LiveInfo {
	out := map[string]LiveInfo{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range rooms {
		wg.Add(1)
		go func(r Room) {
			defer wg.Done()
			li := FetchLive(r.Server)
			mu.Lock()
			out[r.ID] = li
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	return out
}
