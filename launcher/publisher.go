package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Publisher runs the host's background work in order: import dropped zips
// into the local library, and publish events (uploading the packages they
// use first).
type Publisher struct {
	lib *Library
	act *Activity

	mu        sync.Mutex
	lastError string
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
	})
	return t
}

// PublishEvent uploads the packages the event uses that are not on the base
// yet, then saves the event. The task reports progress to the panel.
func (p *Publisher) PublishEvent(d EventDraft) (*Task, error) {
	r := p.remote()
	if r == nil {
		return nil, fmt.Errorf("ligue o painel a uma base primeiro")
	}
	if _, err := BuildEvent(d, p.lib); err != nil {
		return nil, err
	}
	t := p.act.Add("publish", "Publicando "+strings.TrimSpace(d.Name))
	p.act.Enqueue(func() {
		p.act.Set(t, func(t *Task) { t.State = "running" })
		fail := func(err error) {
			p.mu.Lock()
			p.lastError = err.Error()
			p.mu.Unlock()
			p.act.Set(t, func(t *Task) { t.State, t.Error = "error", err.Error() })
		}
		items := d.Items(p.lib)
		var total int64
		for _, it := range items {
			if !it.Uploaded {
				total += it.Size
			}
		}
		var done int64
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
		defer cancel()
		for _, it := range items {
			if it.Uploaded {
				continue
			}
			p.act.Set(t, func(t *Task) { t.Detail, t.Total = "Enviando "+it.Name, total })
			base := done
			err := r.Upload(ctx, fileOpener(p.lib.Path(it)), it.File, it.SHA256, func(n, _ int64) {
				p.act.Set(t, func(t *Task) { t.Done = base + n })
			})
			if err == nil && it.Image != "" {
				sum, _ := fileSHA256(p.lib.ImagePath(it))
				err = r.Upload(ctx, fileOpener(p.lib.ImagePath(it)), it.RemoteImage(), sum, func(int64, int64) {})
			}
			if err != nil {
				fail(fmt.Errorf("%s: %w", it.Name, err))
				return
			}
			p.lib.MarkUploaded(it.Kind, it.ID, it.SHA256)
			done += it.Size
		}
		// Build again: the library may have changed while uploading.
		payload, err := BuildEvent(d, p.lib)
		if err != nil {
			fail(err)
			return
		}
		var res struct {
			Event Event `json:"event"`
		}
		p.act.Set(t, func(t *Task) { t.Detail = "Salvando o evento" })
		if err := r.Call(ctx, "event-save", payload, &res); err != nil {
			fail(err)
			return
		}
		p.mu.Lock()
		p.lastError = ""
		p.mu.Unlock()
		p.act.mu.Lock()
		p.act.PublishedAt = time.Now()
		p.act.mu.Unlock()
		p.act.Set(t, func(t *Task) { t.State, t.Done, t.Detail = "done", t.Total, res.Event.ID })
	})
	return t, nil
}

func (p *Publisher) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastError
}
