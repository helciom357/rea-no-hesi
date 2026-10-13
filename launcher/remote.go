package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

//go:embed receiver/upload.php
var receiverTemplate string

const chunkSize = 4 << 20 // stays under the usual PHP post_max_size of 8 MB

// ReceiverPHP returns upload.php bound to key (only the key's hash is stored).
func ReceiverPHP(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return []byte(strings.Replace(receiverTemplate, "{{KEY_SHA256}}", hex.EncodeToString(sum[:]), 1))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func NewKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Remote talks to upload.php on the admin's website.
type Remote struct {
	Endpoint string // https://site/ac/upload.php
	Key      string
	client   *http.Client
}

func NewRemote(endpoint, key string) *Remote {
	return &Remote{Endpoint: strings.TrimSpace(endpoint), Key: key, client: &http.Client{Timeout: 5 * time.Minute}}
}

// FilesURL is the public folder where uploaded files are served.
func (r *Remote) FilesURL() string {
	u, err := url.Parse(r.Endpoint)
	if err != nil || u.Host == "" {
		return ""
	}
	u.Path = path.Join(path.Dir(u.Path), "files")
	u.RawQuery = ""
	return u.String()
}

func (r *Remote) HubURL() string {
	if f := r.FilesURL(); f != "" {
		return f + "/hub.json"
	}
	return ""
}

func (r *Remote) do(ctx context.Context, method, action string, q url.Values, body io.Reader, out any) error {
	if r.Endpoint == "" {
		return errors.New("conexão com o site não configurada")
	}
	u, err := url.Parse(r.Endpoint)
	if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return errors.New("o endereço do upload.php precisa começar com https://")
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("action", action)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Key", r.Key)
	req.Header.Set("User-Agent", "LauncherBrasil/"+Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("não consegui falar com o site: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var e struct {
		Error string `json:"error"`
	}
	if resp.StatusCode != 200 {
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return &remoteErr{resp.StatusCode, e.Error, b}
		}
		if resp.StatusCode == 404 {
			return errors.New("upload.php não encontrado nesse endereço")
		}
		return &remoteErr{resp.StatusCode, fmt.Sprintf("o site respondeu HTTP %d", resp.StatusCode), b}
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return errors.New("o endereço respondeu, mas não parece ser o upload.php do launcher")
		}
	}
	return nil
}

type remoteErr struct {
	code int
	msg  string
	body []byte
}

func (e *remoteErr) Error() string { return e.msg }

func (r *Remote) Ping(ctx context.Context) error {
	var v struct {
		OK bool `json:"ok"`
	}
	if err := r.do(ctx, http.MethodGet, "ping", nil, nil, &v); err != nil {
		return err
	}
	if !v.OK {
		return errors.New("resposta inesperada do upload.php")
	}
	return nil
}

func (r *Remote) RemoteSize(ctx context.Context, name string) (size, part int64, err error) {
	var v struct {
		Part int64 `json:"part"`
		Size int64 `json:"size"`
	}
	err = r.do(ctx, http.MethodGet, "status", url.Values{"file": {name}}, nil, &v)
	return v.Size, v.Part, err
}

// Upload sends a file in chunks, resuming a previous partial upload, and lets
// the server verify the sha256 before the file becomes public.
func (r *Remote) Upload(ctx context.Context, open func() (io.ReadSeekCloser, int64, error), name, sha string, progress func(done, total int64)) error {
	f, total, err := open()
	if err != nil {
		return err
	}
	defer f.Close()
	_, offset, err := r.RemoteSize(ctx, name)
	if err != nil {
		return err
	}
	if offset > total {
		offset = 0
	}
	buf := make([]byte, chunkSize)
	for retries := 0; ; {
		if offset >= total && total > 0 {
			break
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		n, rerr := io.ReadFull(f, buf)
		if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
			return rerr
		}
		var v struct {
			Part int64 `json:"part"`
		}
		err := r.do(ctx, http.MethodPost, "chunk", url.Values{"file": {name}, "offset": {fmt.Sprint(offset)}}, bytes.NewReader(buf[:n]), &v)
		if err != nil {
			var re *remoteErr
			if errors.As(err, &re) && re.code == 409 {
				var cur struct {
					Part int64 `json:"part"`
				}
				_ = json.Unmarshal(re.body, &cur)
				offset = cur.Part
				continue
			}
			if ctx.Err() != nil || retries >= 3 {
				return err
			}
			retries++
			time.Sleep(time.Duration(retries) * time.Second)
			_, offset, _ = r.RemoteSize(ctx, name)
			continue
		}
		retries = 0
		offset = v.Part
		progress(offset, total)
		if total == 0 {
			break
		}
	}
	return r.do(ctx, http.MethodPost, "finish", url.Values{"file": {name}, "sha256": {sha}}, nil, nil)
}

func fileOpener(p string) func() (io.ReadSeekCloser, int64, error) {
	return func() (io.ReadSeekCloser, int64, error) {
		f, err := os.Open(p)
		if err != nil {
			return nil, 0, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, 0, err
		}
		return f, st.Size(), nil
	}
}

type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }

func bytesOpener(b []byte) func() (io.ReadSeekCloser, int64, error) {
	return func() (io.ReadSeekCloser, int64, error) {
		return nopSeekCloser{bytes.NewReader(b)}, int64(len(b)), nil
	}
}

// ---- upload queue -------------------------------------------------------------

// Task is one entry of the admin's activity list.
type Task struct {
	ID     int64  `json:"id"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`  // import, upload, publish
	State  string `json:"state"` // waiting, running, done, error
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
	At     int64  `json:"at"`
}

type Activity struct {
	mu     sync.Mutex
	tasks  []*Task
	nextID int64
	queue  chan func()
	// PublishedAt is the last successful publication of hub.json.
	PublishedAt time.Time
	publishWant bool
}

func NewActivity() *Activity {
	a := &Activity{queue: make(chan func(), 1024)}
	go func() {
		for job := range a.queue {
			job()
		}
	}()
	return a
}

func (a *Activity) Add(kind, label string) *Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	t := &Task{ID: a.nextID, Kind: kind, Label: label, State: "waiting", At: time.Now().Unix()}
	a.tasks = append(a.tasks, t)
	if len(a.tasks) > 60 {
		a.tasks = a.tasks[len(a.tasks)-60:]
	}
	return t
}

func (a *Activity) Set(t *Task, f func(t *Task)) {
	a.mu.Lock()
	f(t)
	a.mu.Unlock()
}

func (a *Activity) Snapshot() ([]Task, time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Task, len(a.tasks))
	busy := false
	for i, t := range a.tasks {
		out[i] = *t
		if t.State == "waiting" || t.State == "running" {
			busy = true
		}
	}
	return out, a.PublishedAt, busy
}

// Enqueue runs job on the single background worker, in order.
func (a *Activity) Enqueue(job func()) { a.queue <- job }
