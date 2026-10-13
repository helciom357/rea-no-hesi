package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Item is one car or track an event needs, with its state on this PC.
type Item struct {
	Kind     string   `json:"kind"` // "car" or "track"
	ID       string   `json:"id"`
	Layout   string   `json:"layout,omitempty"`
	Name     string   `json:"name,omitempty"`
	State    string   `json:"state"` // ok, missing, broken, outdated (files differ from the server's)
	Version  string   `json:"version,omitempty"`
	URL      string   `json:"url,omitempty"`
	Image    string   `json:"image,omitempty"`
	SHA256   string   `json:"sha256,omitempty"`
	Size     int64    `json:"size,omitempty"`
	Problems []string `json:"problems,omitempty"`
}

func (i Item) Key() string { return i.Kind + ":" + i.ID }

type EventStatus struct {
	EventID string `json:"eventId"`
	Ready   bool   `json:"ready"`
	Pending int    `json:"pending"`
	Bytes   int64  `json:"bytes"`
	Items   []Item `json:"items"`
}

// classify compares the installed mod with the one the event pins. When the
// event carries file checks those decide; the version number is only a
// fallback for entries without them.
func classify(acRoot, kind string, e ContentEntry) (string, []string) {
	var local LocalState
	dir := carDir(acRoot, e.ID)
	if kind == "track" {
		local = CheckTrack(acRoot, e.ID, e.Layout)
		dir = trackDir(acRoot, e.ID)
	} else {
		local = CheckCar(acRoot, e.ID)
	}
	switch {
	case !local.Found:
		return "missing", nil
	case !local.Healthy:
		return "broken", local.Problems
	}
	if len(e.Checks) > 0 {
		if diff := CompareChecks(dir, e.Checks); len(diff) > 0 {
			return "outdated", diff
		}
		return "ok", nil
	}
	if NewerVersion(e.Version, local.Version) {
		return "outdated", nil
	}
	return "ok", nil
}

func StatusForEvent(h *Hub, acRoot string, ev Event) EventStatus {
	st := EventStatus{EventID: ev.ID, Ready: true}
	add := func(kind string, e ContentEntry) {
		state, problems := classify(acRoot, kind, e)
		it := Item{Kind: kind, ID: e.ID, Layout: e.Layout, Name: e.Name, State: state, Version: e.Version,
			URL: h.DownloadURL(e), Image: h.ImageURL(e), SHA256: e.SHA256, Size: e.Size, Problems: problems}
		if state != "ok" {
			st.Ready = false
			st.Pending++
			st.Bytes += e.Size
		}
		st.Items = append(st.Items, it)
	}
	if ValidID(ev.Track.ID) {
		add("track", ev.Track)
	}
	seen := map[string]bool{}
	for _, c := range ev.Cars {
		if seen[c.ID] || !ValidID(c.ID) {
			continue
		}
		seen[c.ID] = true
		add("car", c)
	}
	return st
}

// PendingForEvents merges what several events need, so shared content is
// downloaded once. When two events pin different files for the same mod,
// the first event wins; the other one will show it as different afterwards.
func PendingForEvents(h *Hub, acRoot string, events []Event) []Item {
	var out []Item
	seen := map[string]bool{}
	for _, ev := range events {
		if ev.Locked {
			continue
		}
		for _, it := range StatusForEvent(h, acRoot, ev).Items {
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

func FetchLiveAll(events []Event) map[string]LiveInfo {
	out := map[string]LiveInfo{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ev := range events {
		if ev.Locked {
			continue
		}
		wg.Add(1)
		go func(ev Event) {
			defer wg.Done()
			li := FetchLive(ev.Server)
			mu.Lock()
			out[ev.ID] = li
			mu.Unlock()
		}(ev)
	}
	wg.Wait()
	return out
}
