package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxExtractBytes = 20 << 30 // refuse archives that expand past 20 GB
	parallelJobs    = 3
)

// ItemProgress is what the UI shows for each item being installed.
type ItemProgress struct {
	Item
	Phase      string `json:"phase"` // waiting, downloading, extracting, done, error
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Error      string `json:"error,omitempty"`
}

type Job struct {
	ID       string          `json:"id"`
	Status   string          `json:"status"` // running, done, error, cancelled
	Items    []*ItemProgress `json:"items"`
	Started  time.Time       `json:"started"`
	Finished time.Time       `json:"finished,omitempty"`
	cancel   context.CancelFunc
}

type Installer struct {
	mu  sync.Mutex
	job *Job
}

func (in *Installer) Snapshot() *Job {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.job == nil {
		return nil
	}
	c := *in.job
	c.Items = make([]*ItemProgress, len(in.job.Items))
	for i, p := range in.job.Items {
		cp := *p
		c.Items[i] = &cp
	}
	return &c
}

func (in *Installer) Cancel() {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.job != nil && in.job.cancel != nil {
		in.job.cancel()
	}
}

// Start installs items in the background. Only one job runs at a time.
func (in *Installer) Start(h *Hub, acRoot string, items []Item) (*Job, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.job != nil && in.job.Status == "running" {
		return nil, errors.New("já existe uma instalação em andamento")
	}
	if !IsACRoot(acRoot) {
		return nil, errors.New("pasta do Assetto Corsa não encontrada — configure em Ajustes")
	}
	for _, it := range items {
		if !ValidID(it.ID) {
			return nil, fmt.Errorf("id inválido: %q", it.ID)
		}
		if err := checkAllowedURL(h, it.URL); err != nil {
			return nil, fmt.Errorf("%s: %w", it.ID, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{ID: fmt.Sprint(time.Now().UnixNano()), Status: "running", Started: time.Now(), cancel: cancel}
	for _, it := range items {
		j.Items = append(j.Items, &ItemProgress{Item: it, Phase: "waiting", Total: it.Size})
	}
	in.job = j
	go in.run(ctx, j, acRoot)
	return j, nil
}

func (in *Installer) update(f func()) {
	in.mu.Lock()
	f()
	in.mu.Unlock()
}

func (in *Installer) run(ctx context.Context, j *Job, acRoot string) {
	staging := filepath.Join(acRoot, "content", ".launcher-staging")
	_ = os.MkdirAll(staging, 0o755)
	sem := make(chan struct{}, parallelJobs)
	var wg sync.WaitGroup
	for _, p := range j.Items {
		wg.Add(1)
		go func(p *ItemProgress) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			err := in.installOne(ctx, p, acRoot, staging)
			in.update(func() {
				if err != nil {
					p.Phase, p.Error = "error", err.Error()
				} else {
					p.Phase = "done"
				}
			})
		}(p)
	}
	wg.Wait()
	_ = os.RemoveAll(staging)
	in.update(func() {
		j.Finished = time.Now()
		j.Status = "done"
		if ctx.Err() != nil {
			j.Status = "cancelled"
		}
		for _, p := range j.Items {
			if p.Phase == "error" && j.Status == "done" {
				j.Status = "error"
			}
		}
	})
}

func (in *Installer) installOne(ctx context.Context, p *ItemProgress, acRoot, staging string) error {
	if ctx.Err() != nil {
		return errors.New("cancelado")
	}
	in.update(func() { p.Phase = "downloading" })
	archive := filepath.Join(staging, p.Kind+"-"+p.ID+".zip")
	if err := download(ctx, p.URL, archive, p.SHA256, func(done, total int64) {
		in.update(func() { p.Downloaded, p.Total = done, total })
	}); err != nil {
		return err
	}
	in.update(func() { p.Phase = "extracting" })
	defer os.Remove(archive)
	return InstallArchive(archive, acRoot, staging, p.Kind, p.ID)
}

func checkAllowedURL(h *Hub, raw string) error {
	if raw == "" {
		return errors.New("sem link de download")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("link de download inválido")
	}
	host := u.Hostname()
	local := host == "localhost" || host == "127.0.0.1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return errors.New("o download precisa ser HTTPS")
	}
	if local {
		return nil
	}
	for _, a := range h.AllowedHosts {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if strings.EqualFold(host, a) || (strings.HasPrefix(a, "*.") && strings.HasSuffix(strings.ToLower(host), a[1:])) {
			return nil
		}
	}
	return fmt.Errorf("o host %s não está na lista de hosts permitidos da base", host)
}

var downloadClient = &http.Client{Timeout: 0}

func download(ctx context.Context, src, dst, wantSHA string, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "LauncherBrasil/"+Version)
	resp, err := downloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha no download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("o download respondeu HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	h := sha256.New()
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if time.Since(last) > 200*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return fmt.Errorf("download interrompido: %w", rerr)
		}
	}
	progress(done, done)
	if err := f.Close(); err != nil {
		return err
	}
	if wantSHA != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantSHA) {
		return errors.New("o arquivo baixado não confere com o sha256 da base (corrompido ou trocado)")
	}
	return nil
}

// findRoot locates the folder inside the archive that holds the item, so a
// package can be zipped as content/cars/<id>/..., <id>/... or flat.
func findRoot(names []string, kind, id string) (string, error) {
	sub := "cars"
	marker := "ui/ui_car.json"
	if kind == "track" {
		sub = "tracks"
		marker = "ui/"
	}
	candidates := []string{"content/" + sub + "/" + id + "/", id + "/"}
	for _, c := range candidates {
		for _, n := range names {
			if strings.HasPrefix(strings.ToLower(n), strings.ToLower(c)) {
				return n[:len(c)], nil
			}
		}
	}
	// Flat archive: the item's files sit at the top level.
	for _, n := range names {
		if strings.HasPrefix(n, marker) || (kind == "track" && strings.HasSuffix(n, ".kn5") && !strings.Contains(n, "/")) {
			return "", nil
		}
	}
	return "", fmt.Errorf("o pacote não contém a pasta %q", id)
}

// InstallArchive extracts the item into a staging folder, then swaps it into
// content/cars or content/tracks so a failed install never leaves half a mod.
func InstallArchive(archive, acRoot, staging, kind, id string) error {
	if !ValidID(id) {
		return fmt.Errorf("id inválido: %q", id)
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("pacote não é um .zip válido: %w", err)
	}
	defer zr.Close()
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, strings.ReplaceAll(f.Name, `\`, "/"))
	}
	root, err := findRoot(names, kind, id)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(staging, kind+"-"+id+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var written int64
	for i, f := range zr.File {
		name := names[i]
		if !strings.HasPrefix(name, root) {
			continue
		}
		rel := strings.TrimPrefix(name, root)
		if rel == "" {
			continue
		}
		clean := path.Clean(rel)
		if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || path.IsAbs(clean) || strings.Contains(clean, ":") {
			return fmt.Errorf("o pacote tem um caminho inseguro: %q", name)
		}
		dst := filepath.Join(tmp, filepath.FromSlash(clean))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue // ignore links and special files
		}
		written += int64(f.UncompressedSize64)
		if written > maxExtractBytes {
			return errors.New("o pacote é grande demais")
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := extractFile(f, dst); err != nil {
			return err
		}
	}
	sub := "cars"
	if kind == "track" {
		sub = "tracks"
	}
	target := filepath.Join(acRoot, "content", sub, id)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	backup := ""
	if exists(target) {
		backup = filepath.Join(staging, fmt.Sprintf("old-%s-%s-%d", kind, id, time.Now().UnixNano()))
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("não foi possível substituir %s (o jogo está aberto?): %w", id, err)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		if backup != "" {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("não foi possível instalar %s: %w", id, err)
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func extractFile(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
