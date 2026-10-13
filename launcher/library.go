package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LibItem is one car or track the admin has added to the base. Each item is
// stored as its own normalized package: content/<cars|tracks>/<id>/...
type LibItem struct {
	Kind        string      `json:"kind"`
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	File        string      `json:"file"`
	SHA256      string      `json:"sha256"`
	Size        int64       `json:"size"`
	Layouts     []string    `json:"layouts,omitempty"`
	Image       string      `json:"image,omitempty"`  // file name under img/
	Fingerprint string      `json:"fingerprint"`      // content hash ignoring the version field
	Checks      []FileCheck `json:"checks,omitempty"` // what decides "same files as the server"
	Uploaded    bool        `json:"uploaded"`         // package + image are on the remote base
	UpdatedAt   time.Time   `json:"updatedAt"`
}

func (i LibItem) Key() string { return i.Kind + ":" + i.ID }

// RemoteImage is the name the preview image gets on the remote base.
func (i LibItem) RemoteImage() string {
	if i.Image == "" {
		return ""
	}
	return "img-" + i.Image
}

// ImportResult tells the admin what happened to each item of a dropped zip.
type ImportResult struct {
	Item   LibItem `json:"item"`
	Status string  `json:"status"` // new, updated, unchanged
}

type Library struct {
	dir   string
	mu    sync.Mutex
	Items []LibItem `json:"items"`
}

func OpenLibrary(dir string) *Library {
	l := &Library{dir: dir}
	_ = os.MkdirAll(filepath.Join(dir, "img"), 0o755)
	if b, err := os.ReadFile(filepath.Join(dir, "library.json")); err == nil {
		_ = json.Unmarshal(b, l)
	}
	return l
}

func (l *Library) saveLocked() error {
	b, _ := json.MarshalIndent(l, "", "  ")
	tmp := filepath.Join(l.dir, "library.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(l.dir, "library.json"))
}

func (l *Library) List() []LibItem {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]LibItem{}, l.Items...)
	sort.Slice(out, func(a, b int) bool {
		if out[a].Kind != out[b].Kind {
			return out[a].Kind == "track"
		}
		return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name)
	})
	return out
}

func (l *Library) Get(kind, id string) (LibItem, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, it := range l.Items {
		if it.Kind == kind && it.ID == id {
			return it, true
		}
	}
	return LibItem{}, false
}

func (l *Library) Path(it LibItem) string      { return filepath.Join(l.dir, it.File) }
func (l *Library) ImagePath(it LibItem) string { return filepath.Join(l.dir, "img", it.Image) }

func (l *Library) MarkUploaded(kind, id, sha string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.Items {
		if l.Items[i].Kind == kind && l.Items[i].ID == id && l.Items[i].SHA256 == sha {
			l.Items[i].Uploaded = true
		}
	}
	_ = l.saveLocked()
}

func (l *Library) Remove(kind, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, it := range l.Items {
		if it.Kind == kind && it.ID == id {
			_ = os.Remove(filepath.Join(l.dir, it.File))
			if it.Image != "" {
				_ = os.Remove(filepath.Join(l.dir, "img", it.Image))
			}
			l.Items = append(l.Items[:i], l.Items[i+1:]...)
			return l.saveLocked()
		}
	}
	return errors.New("item não encontrado")
}

// found is one car or track located inside a dropped archive.
type found struct {
	kind, id, root string // root ends with "/"
	layouts        []string
}

var (
	carUIRe   = regexp.MustCompile(`(?i)^(.*/)?([^/]+)/ui/ui_car\.json$`)
	trackUIRe = regexp.MustCompile(`(?i)^(.*/)?([^/]+)/ui/(?:([^/]+)/)?ui_track\.json$`)
)

// scanArchive finds every car and track in a zip, so a pack of several mods
// can be dropped at once.
func scanArchive(names []string) []found {
	byRoot := map[string]*found{}
	var order []string
	for _, n := range names {
		if m := carUIRe.FindStringSubmatch(n); m != nil {
			root := m[1] + m[2] + "/"
			if byRoot[root] == nil {
				byRoot[root] = &found{kind: "car", id: m[2], root: root}
				order = append(order, root)
			}
			continue
		}
		if m := trackUIRe.FindStringSubmatch(n); m != nil {
			root := m[1] + m[2] + "/"
			f := byRoot[root]
			if f == nil {
				f = &found{kind: "track", id: m[2], root: root}
				byRoot[root] = f
				order = append(order, root)
			}
			if m[3] != "" {
				f.layouts = append(f.layouts, m[3])
			}
		}
	}
	// Ignore items nested inside another item (backups inside a car folder…).
	var out []found
	seenID := map[string]bool{}
	for _, r := range order {
		if seenID[byRoot[r].kind+":"+byRoot[r].id] {
			continue // the same mod twice in one zip: keep the first
		}
		nested := false
		for _, o := range order {
			if o != r && strings.HasPrefix(r, o) {
				nested = true
				break
			}
		}
		if !nested {
			seenID[byRoot[r].kind+":"+byRoot[r].id] = true
			f := *byRoot[r]
			sort.Strings(f.layouts)
			out = append(out, f)
		}
	}
	return out
}

// Import adds every car and track found in archive to the library. A changed
// item gets its version bumped automatically; an identical one is left as is.
func (l *Library) Import(archive string) ([]ImportResult, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return nil, errors.New("o arquivo não é um .zip válido (.7z e .rar: instale no seu Assetto e use \"Adicionar do meu Assetto\")")
	}
	defer zr.Close()
	names := make([]string, len(zr.File))
	for i, f := range zr.File {
		names[i] = strings.ReplaceAll(f.Name, `\`, "/")
	}
	items := scanArchive(names)
	if len(items) == 0 {
		return nil, errors.New("não encontrei carro nem pista neste zip — ele precisa ter a pasta do mod com ui/ui_car.json ou ui/ui_track.json dentro")
	}
	var results []ImportResult
	for _, f := range items {
		if !ValidID(f.id) {
			return results, fmt.Errorf("o nome da pasta %q não pode ser usado (use letras, números, _ - .)", f.id)
		}
		r, err := l.importOne(zr, names, f)
		if err != nil {
			return results, fmt.Errorf("%s: %w", f.id, err)
		}
		results = append(results, r)
	}
	return results, nil
}

func (l *Library) importOne(zr *zip.ReadCloser, names []string, f found) (ImportResult, error) {
	uiNames := map[string]bool{"ui_car.json": true, "ui_track.json": true}
	var files []*zip.File
	var rels []string
	var fpLines []string
	var pkgFiles []PkgFile
	zipVersion, name := "", ""
	for i, zf := range zr.File {
		n := names[i]
		if !strings.HasPrefix(n, f.root) || zf.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(n, f.root)
		clean := path.Clean(rel)
		if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.Contains(clean, ":") {
			return ImportResult{}, fmt.Errorf("caminho inseguro no zip: %q", n)
		}
		files = append(files, zf)
		rels = append(rels, clean)
		pkgFiles = append(pkgFiles, PkgFile{Rel: clean, Size: int64(zf.UncompressedSize64), CRC32: zf.CRC32})
		base := strings.ToLower(path.Base(clean))
		if uiNames[base] {
			b := readZipFile(zf, 1<<20)
			v, nm := uiField(b, versionRe), uiField(b, nameRe)
			// Prefer the main ui file (ui/ui_car.json, ui/ui_track.json).
			if path.Dir(clean) == "ui" || name == "" {
				if nm != "" {
					name = nm
				}
				if v != "" {
					zipVersion = v
				}
			}
			continue // version line excluded from the fingerprint
		}
		fpLines = append(fpLines, fmt.Sprintf("%s|%08x|%d", strings.ToLower(clean), zf.CRC32, zf.UncompressedSize64))
	}
	// Order-independent, so re-zipping the same mod is not a new version.
	sort.Strings(fpLines)
	fingerprint := sha256Hex([]byte(strings.Join(fpLines, "\n")))

	l.mu.Lock()
	prev, had := LibItem{}, false
	for _, it := range l.Items {
		if it.Kind == f.kind && it.ID == f.id {
			prev, had = it, true
		}
	}
	l.mu.Unlock()
	if had && prev.Fingerprint == fingerprint && len(prev.Checks) > 0 && exists(filepath.Join(l.dir, prev.File)) {
		return ImportResult{Item: prev, Status: "unchanged"}, nil
	}
	version := zipVersion
	if version == "" {
		version = "1.0"
	}
	if had && !NewerVersion(version, prev.Version) {
		version = BumpVersion(prev.Version)
	}
	if name == "" {
		name = f.id
	}

	sub := "cars"
	if f.kind == "track" {
		sub = "tracks"
	}
	// Packages are named by their hash: two hosts with different versions of
	// the same mod never overwrite each other on the base.
	tmp := filepath.Join(l.dir, f.kind+"-"+f.id+".zip.tmp")
	if err := writePackage(tmp, files, rels, "content/"+sub+"/"+f.id+"/", version, uiNames); err != nil {
		os.Remove(tmp)
		return ImportResult{}, err
	}
	sum, err := fileSHA256(tmp)
	if err != nil {
		os.Remove(tmp)
		return ImportResult{}, err
	}
	stem := f.kind + "-" + f.id + "-" + sum[:12]
	file := stem + ".zip"
	if err := os.Rename(tmp, filepath.Join(l.dir, file)); err != nil {
		return ImportResult{}, err
	}
	st, _ := os.Stat(filepath.Join(l.dir, file))
	item := LibItem{Kind: f.kind, ID: f.id, Name: name, Version: version, File: file, SHA256: sum,
		Size: st.Size(), Layouts: f.layouts, Fingerprint: fingerprint, UpdatedAt: time.Now(),
		Checks: ChecksFromList(f.kind, pkgFiles)}
	item.Image = l.saveImage(stem, files, rels, f.kind, f.layouts)
	if had && prev.File != "" && prev.File != item.File {
		_ = os.Remove(filepath.Join(l.dir, prev.File))
		if prev.Image != "" && prev.Image != item.Image {
			_ = os.Remove(filepath.Join(l.dir, "img", prev.Image))
		}
	}

	l.mu.Lock()
	replaced := false
	for i := range l.Items {
		if l.Items[i].Kind == item.Kind && l.Items[i].ID == item.ID {
			l.Items[i] = item
			replaced = true
		}
	}
	if !replaced {
		l.Items = append(l.Items, item)
	}
	err = l.saveLocked()
	l.mu.Unlock()
	status := "new"
	if had {
		status = "updated"
	}
	return ImportResult{Item: item, Status: status}, err
}

// writePackage copies the item's files under prefix without recompressing
// them; only the ui json files are rewritten to carry the version.
func writePackage(dst string, files []*zip.File, rels []string, prefix, version string, uiNames map[string]bool) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	for i, zf := range files {
		hdr := zf.FileHeader
		hdr.Name = prefix + rels[i]
		hdr.Extra = stripExtra(hdr.Extra)
		if uiNames[strings.ToLower(path.Base(rels[i]))] {
			hdr.Method = zip.Deflate
			hdr.CompressedSize64, hdr.UncompressedSize64, hdr.CRC32 = 0, 0, 0
			w, err := zw.CreateHeader(&hdr)
			if err != nil {
				return closeAll(zw, out, err)
			}
			if _, err := w.Write(SetVersionInUI(readZipFile(zf, 1<<20), version)); err != nil {
				return closeAll(zw, out, err)
			}
			continue
		}
		raw, err := zf.OpenRaw()
		if err != nil {
			return closeAll(zw, out, err)
		}
		w, err := zw.CreateRaw(&hdr)
		if err != nil {
			return closeAll(zw, out, err)
		}
		if _, err := io.Copy(w, raw); err != nil {
			return closeAll(zw, out, err)
		}
	}
	return closeAll(zw, out, nil)
}

// stripExtra drops the zip64 (0x0001) and timestamp (0x5455) extra fields,
// which the writer regenerates; keeping the originals would duplicate them.
func stripExtra(b []byte) []byte {
	var out []byte
	for len(b) >= 4 {
		tag := uint16(b[0]) | uint16(b[1])<<8
		size := int(uint16(b[2]) | uint16(b[3])<<8)
		if 4+size > len(b) {
			break
		}
		if tag != 0x0001 && tag != 0x5455 {
			out = append(out, b[:4+size]...)
		}
		b = b[4+size:]
	}
	return out
}

func closeAll(zw *zip.Writer, f *os.File, err error) error {
	if e := zw.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}

func readZipFile(zf *zip.File, limit int64) []byte {
	rc, err := zf.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, limit))
	return b
}

func uiField(b []byte, re *regexp.Regexp) string {
	if m := re.FindSubmatch(b); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// saveImage keeps a preview for the cards: the first skin preview of a car,
// or the track preview.
func (l *Library) saveImage(stem string, files []*zip.File, rels []string, kind string, layouts []string) string {
	var cands []string
	if kind == "car" {
		var skins []string
		for _, r := range rels {
			lr := strings.ToLower(r)
			if strings.HasPrefix(lr, "skins/") && strings.HasSuffix(lr, "/preview.jpg") && strings.Count(lr, "/") == 2 {
				skins = append(skins, r)
			}
		}
		sort.Strings(skins)
		cands = skins
	} else {
		cands = append(cands, "ui/preview.png")
		for _, ly := range layouts {
			cands = append(cands, "ui/"+ly+"/preview.png")
		}
	}
	for _, c := range cands {
		for i, r := range rels {
			if !strings.EqualFold(r, c) || files[i].UncompressedSize64 > 8<<20 {
				continue
			}
			b := readZipFile(files[i], 8<<20)
			if len(b) == 0 {
				continue
			}
			name := stem + strings.ToLower(path.Ext(r))
			if os.WriteFile(filepath.Join(l.dir, "img", name), b, 0o644) == nil {
				return name
			}
		}
	}
	return ""
}

// BumpVersion increments the last number: 1.0 -> 1.1, 1.9 -> 1.10.
func BumpVersion(v string) string {
	if v == "" {
		return "1.0"
	}
	parts := strings.Split(v, ".")
	last := len(parts) - 1
	n, err := strconv.Atoi(strings.TrimLeft(parts[last], "vV"))
	if err != nil {
		return v + ".1"
	}
	parts[last] = strconv.Itoa(n + 1)
	return strings.Join(parts, ".")
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
