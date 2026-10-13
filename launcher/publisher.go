package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Publisher runs the admin's background work in order: import dropped zips,
// upload packages and publish hub.json. Everything the admin does goes live
// without a separate "export" step.
type Publisher struct {
	lib *Library
	act *Activity

	mu             sync.Mutex
	publishPending bool
	lastError      string
}

func NewPublisher(lib *Library) *Publisher {
	return &Publisher{lib: lib, act: NewActivity()}
}

func (p *Publisher) remote() *Remote {
	s := LoadSettings()
	if s.UploadURL == "" || s.UploadKey == "" {
		return nil
	}
	return NewRemote(s.UploadURL, s.UploadKey)
}

func summarize(results []ImportResult) string {
	var parts []string
	count := map[string]int{}
	for _, r := range results {
		count[r.Item.Kind+":"+r.Status]++
	}
	label := map[string]string{"car": "carro", "track": "pista"}
	for _, kind := range []string{"car", "track"} {
		for _, st := range []string{"new", "updated", "unchanged"} {
			n := count[kind+":"+st]
			if n == 0 {
				continue
			}
			noun := label[kind]
			if n > 1 {
				noun += "s"
			}
			verb := map[string]string{"new": "adicionad", "updated": "atualizad", "unchanged": "sem mudança"}[st]
			if st != "unchanged" {
				if kind == "track" {
					verb += "a"
				} else {
					verb += "o"
				}
				if n > 1 {
					verb += "s"
				}
			}
			parts = append(parts, fmt.Sprintf("%d %s %s", n, noun, verb))
		}
	}
	return strings.Join(parts, " · ")
}

// Import queues a dropped (or freshly zipped) archive. The temp file is
// removed once imported.
func (p *Publisher) Import(tmpPath, label string, removeAfter bool) *Task {
	t := p.act.Add("import", label)
	p.act.Enqueue(func() {
		p.act.Set(t, func(t *Task) { t.State = "running" })
		results, err := p.lib.Import(tmpPath)
		if removeAfter {
			os.Remove(tmpPath)
		}
		p.act.Set(t, func(t *Task) {
			t.Detail = summarize(results)
			if err != nil {
				t.State, t.Error = "error", err.Error()
			} else {
				t.State = "done"
			}
		})
		for _, r := range results {
			if !r.Item.Uploaded {
				p.queueUpload(r.Item)
			}
		}
		if len(results) > 0 {
			p.SchedulePublish()
		}
	})
	return t
}

func (p *Publisher) queueUpload(it LibItem) {
	if p.remote() == nil {
		return // stays local until the connection is set
	}
	t := p.act.Add("upload", "Enviando "+it.Name)
	p.act.Enqueue(func() { p.upload(t, it) })
}

func (p *Publisher) upload(t *Task, it LibItem) {
	r := p.remote()
	cur, ok := p.lib.Get(it.Kind, it.ID)
	if r == nil || !ok || cur.SHA256 != it.SHA256 {
		p.act.Set(t, func(t *Task) { t.State, t.Detail = "done", "substituído por uma versão mais nova" })
		return
	}
	p.act.Set(t, func(t *Task) { t.State, t.Total = "running", cur.Size })
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	err := r.Upload(ctx, fileOpener(p.lib.Path(cur)), cur.File, cur.SHA256, func(done, total int64) {
		p.act.Set(t, func(t *Task) { t.Done, t.Total = done, total })
	})
	if err == nil && cur.Image != "" {
		sum, _ := fileSHA256(p.lib.ImagePath(cur))
		err = r.Upload(ctx, fileOpener(p.lib.ImagePath(cur)), cur.RemoteImage(), sum, func(int64, int64) {})
	}
	p.act.Set(t, func(t *Task) {
		if err != nil {
			t.State, t.Error = "error", err.Error()
		} else {
			t.State, t.Done = "done", t.Total
		}
	})
	if err == nil {
		p.lib.MarkUploaded(cur.Kind, cur.ID, cur.SHA256)
	}
}

// SyncAll uploads everything not yet on the site and republishes; used right
// after the connection is configured.
func (p *Publisher) SyncAll() {
	for _, it := range p.lib.List() {
		if !it.Uploaded {
			p.queueUpload(it)
		}
	}
	p.SchedulePublish()
}

// SchedulePublish coalesces publish requests: many quick edits produce one
// upload of hub.json after the pending uploads.
func (p *Publisher) SchedulePublish() {
	p.mu.Lock()
	if p.publishPending {
		p.mu.Unlock()
		return
	}
	p.publishPending = true
	p.mu.Unlock()
	if p.remote() == nil {
		p.mu.Lock()
		p.publishPending = false
		p.mu.Unlock()
		return
	}
	t := p.act.Add("publish", "Publicando a base")
	p.act.Enqueue(func() {
		p.mu.Lock()
		p.publishPending = false
		p.mu.Unlock()
		p.publish(t)
	})
}

func (p *Publisher) publish(t *Task) {
	p.act.Set(t, func(t *Task) { t.State = "running" })
	fail := func(err error) {
		p.mu.Lock()
		p.lastError = err.Error()
		p.mu.Unlock()
		p.act.Set(t, func(t *Task) { t.State, t.Error = "error", err.Error() })
	}
	r := p.remote()
	if r == nil {
		fail(fmt.Errorf("conexão com o site não configurada"))
		return
	}
	s := LoadSettings()
	base, err := LoadAdminBase(s.AdminBase)
	if err != nil {
		fail(err)
		return
	}
	// Content used by rooms must be online before the hub points at it.
	for _, it := range ReferencedItems(base, p.lib) {
		if !it.Uploaded {
			sub := p.act.Add("upload", "Enviando "+it.Name)
			p.upload(sub, it)
			if cur, _ := p.lib.Get(it.Kind, it.ID); !cur.Uploaded {
				fail(fmt.Errorf("não foi possível enviar %s", it.Name))
				return
			}
		}
	}
	hub := BuildHub(base, p.lib, r.FilesURL())
	b, _ := json.MarshalIndent(hub, "", "  ")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sum := sha256Hex(b)
	if err := r.Upload(ctx, bytesOpener(b), "hub.json", sum, func(int64, int64) {}); err != nil {
		fail(err)
		return
	}
	// Keep a local copy next to the base for reference and offline testing.
	_ = os.WriteFile(filepath.Join(filepath.Dir(s.AdminBase), "hub.json"), b, 0o644)
	p.mu.Lock()
	p.lastError = ""
	p.mu.Unlock()
	p.act.mu.Lock()
	p.act.PublishedAt = time.Now()
	p.act.mu.Unlock()
	p.act.Set(t, func(t *Task) {
		t.State = "done"
		t.Detail = fmt.Sprintf("%d sala(s) no ar", len(hub.Rooms))
	})
}

func (p *Publisher) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastError
}
