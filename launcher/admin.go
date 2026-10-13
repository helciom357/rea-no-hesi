package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ---- base.json --------------------------------------------------------------

func LoadBase(path string) (*Hub, error) {
	h, err := LoadHubFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ExampleBase(), nil
	}
	return h, err
}

func SaveBase(path string, h *Hub) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if exists(path) {
		_ = os.WriteFile(path+".bak", mustRead(path), 0o644)
	}
	return os.Rename(tmp, path)
}

func mustRead(p string) []byte { b, _ := os.ReadFile(p); return b }

// ExampleBase is the starting point shown the first time the admin opens the
// panel. Every value is a placeholder to be replaced with real content.
func ExampleBase() *Hub {
	return &Hub{
		Name:         "Base Brasil (exemplo)",
		Description:  "Rodovias e avenidas brasileiras com carros clássicos. Substitua estes dados pelos reais.",
		DownloadBase: "https://downloads.exemplo.com.br/ac",
		AllowedHosts: []string{"downloads.exemplo.com.br"},
		Modes: []Mode{
			{ID: "trafego", Name: "Tráfego", Description: "Rodovia com tráfego de IA"},
			{ID: "passeio", Name: "Passeio", Description: "Rolê sem pressa, sem colisão de IA"},
		},
		Rooms: []Room{
			{
				ID: "bandeirantes-trafego", Name: "Bandeirantes — Tráfego", ModeID: "trafego",
				Server: ServerAddr{Host: "127.0.0.1", TCPPort: 9600, HTTPPort: 8081},
				Track:  ContentEntry{ID: "br_bandeirantes", Name: "Rod. dos Bandeirantes", Version: "1.0", File: "br_bandeirantes.zip"},
				Cars: []ContentEntry{
					{ID: "br_opala_ss", Name: "Opala SS", Version: "1.0", File: "br_opala_ss.zip"},
					{ID: "br_maverick_gt", Name: "Maverick GT", Version: "1.0", File: "br_maverick_gt.zip"},
				},
			},
			{
				ID: "interlagos-passeio", Name: "Interlagos — Passeio", ModeID: "passeio",
				Server: ServerAddr{Host: "127.0.0.1", TCPPort: 9601, HTTPPort: 8082},
				Track:  ContentEntry{ID: "br_interlagos", Name: "Interlagos", Version: "1.0", File: "br_interlagos.zip"},
				Cars: []ContentEntry{
					{ID: "br_opala_ss", Name: "Opala SS", Version: "1.0", File: "br_opala_ss.zip"},
					{ID: "br_fusca_1300", Name: "Fusca 1300", Version: "1.0", File: "br_fusca_1300.zip"},
				},
			},
		},
	}
}

// ---- packages ---------------------------------------------------------------

// PackageInfo describes a .zip in the admin's package folder.
type PackageInfo struct {
	File    string   `json:"file"`
	Kind    string   `json:"kind"` // car, track or unknown
	ID      string   `json:"id"`
	Layouts []string `json:"layouts,omitempty"`
	Name    string   `json:"name,omitempty"`
	Version string   `json:"version,omitempty"`
	SHA256  string   `json:"sha256"`
	Size    int64    `json:"size"`
	Error   string   `json:"error,omitempty"`
}

var nameRe = regexp.MustCompile(`"name"\s*:\s*"([^"]*)"`)

// InspectPackage reads a package without extracting it: what it contains,
// its version, hash and size.
func InspectPackage(p string) PackageInfo {
	info := PackageInfo{File: filepath.Base(p), Kind: "unknown"}
	st, err := os.Stat(p)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Size = st.Size()
	if sum, err := fileSHA256(p); err == nil {
		info.SHA256 = sum
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		info.Error = "não é um .zip válido"
		return info
	}
	defer zr.Close()
	carRe := regexp.MustCompile(`(?i)^(?:content/cars/)?([^/]+)/ui/ui_car\.json$`)
	trackRe := regexp.MustCompile(`(?i)^(?:content/tracks/)?([^/]+)/ui/(?:([^/]+)/)?ui_track\.json$`)
	for _, f := range zr.File {
		n := strings.ReplaceAll(f.Name, `\`, "/")
		if m := carRe.FindStringSubmatch(n); m != nil {
			info.Kind, info.ID = "car", m[1]
			info.Name, info.Version = readUIFields(f)
			break
		}
		if m := trackRe.FindStringSubmatch(n); m != nil {
			info.Kind, info.ID = "track", m[1]
			if m[2] != "" {
				info.Layouts = append(info.Layouts, m[2])
			}
			if info.Version == "" {
				info.Name, info.Version = readUIFields(f)
			}
		}
	}
	if info.Kind == "unknown" {
		info.Error = "não encontrei ui/ui_car.json nem ui/ui_track.json dentro de uma pasta com o id"
	}
	return info
}

func readUIFields(f *zip.File) (name, version string) {
	rc, err := f.Open()
	if err != nil {
		return
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
	if m := nameRe.FindSubmatch(b); m != nil {
		name = string(m[1])
	}
	if m := versionRe.FindSubmatch(b); m != nil {
		version = string(m[1])
	}
	return
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

func ListPackages(dir string) []PackageInfo {
	var out []PackageInfo
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".zip") {
			out = append(out, InspectPackage(filepath.Join(dir, e.Name())))
		}
	}
	return out
}

// LocalContent lists cars or tracks installed on this PC, so the admin can
// package them without leaving the launcher.
type LocalContent struct {
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Version string   `json:"version,omitempty"`
	Layouts []string `json:"layouts,omitempty"`
}

func ListLocalContent(acRoot, kind string) []LocalContent {
	sub := "cars"
	if kind == "track" {
		sub = "tracks"
	}
	var out []LocalContent
	entries, _ := os.ReadDir(filepath.Join(acRoot, "content", sub))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		lc := LocalContent{Kind: kind, ID: e.Name()}
		dir := filepath.Join(acRoot, "content", sub, e.Name())
		ui := filepath.Join(dir, "ui", "ui_car.json")
		if kind == "track" {
			ui = filepath.Join(dir, "ui", "ui_track.json")
			if subs, err := os.ReadDir(filepath.Join(dir, "ui")); err == nil {
				for _, s := range subs {
					if s.IsDir() && exists(filepath.Join(dir, "ui", s.Name(), "ui_track.json")) {
						lc.Layouts = append(lc.Layouts, s.Name())
						if !exists(ui) {
							ui = filepath.Join(dir, "ui", s.Name(), "ui_track.json")
						}
					}
				}
			}
		}
		if b, err := os.ReadFile(ui); err == nil {
			if m := nameRe.FindSubmatch(b); m != nil {
				lc.Name = string(m[1])
			}
			if m := versionRe.FindSubmatch(b); m != nil {
				lc.Version = string(m[1])
			}
		}
		out = append(out, lc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// BuildPackage zips content/<cars|tracks>/<id> into <pkgDir>/<id>.zip with
// the layout the launcher expects (content/cars/<id>/...). When version is
// given it is written into the ui json files inside the package only; the
// local install is not modified.
func BuildPackage(acRoot, pkgDir, kind, id, version string) (PackageInfo, error) {
	if !ValidID(id) {
		return PackageInfo{}, fmt.Errorf("id inválido: %q", id)
	}
	sub := "cars"
	if kind == "track" {
		sub = "tracks"
	}
	src := filepath.Join(acRoot, "content", sub, id)
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return PackageInfo{}, fmt.Errorf("não encontrei %s em content/%s", id, sub)
	}
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return PackageInfo{}, err
	}
	dst := filepath.Join(pkgDir, id+".zip")
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return PackageInfo{}, err
	}
	zw := zip.NewWriter(out)
	prefix := "content/" + sub + "/" + id + "/"
	walkErr := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		name := prefix + filepath.ToSlash(rel)
		if d.IsDir() {
			_, err := zw.Create(name + "/")
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, _ := d.Info()
		hdr, _ := zip.FileInfoHeader(info)
		hdr.Name = name
		hdr.Method = zip.Deflate
		hdr.Modified = time.Now()
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		base := strings.ToLower(d.Name())
		if version != "" && (base == "ui_car.json" || base == "ui_track.json") {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			_, err = w.Write(SetVersionInUI(b, version))
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err := zw.Close(); walkErr == nil {
		walkErr = err
	}
	if err := out.Close(); walkErr == nil {
		walkErr = err
	}
	if walkErr != nil {
		os.Remove(tmp)
		return PackageInfo{}, walkErr
	}
	if err := os.Rename(tmp, dst); err != nil {
		return PackageInfo{}, err
	}
	return InspectPackage(dst), nil
}

// ---- export -----------------------------------------------------------------

type ExportResult struct {
	Dir      string   `json:"dir"`
	Files    []string `json:"files"`
	Upload   []string `json:"upload"`   // packages that must go to DownloadBase
	Problems []string `json:"problems"` // validation warnings (export still happens)
}

// Export writes hub.json (for the launcher) and one cm_content/content.json
// per room (for AssettoServer, which hands it to Content Manager).
func Export(h *Hub, dir string) (ExportResult, error) {
	res := ExportResult{Dir: dir, Problems: h.Validate()}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	b, _ := json.MarshalIndent(h, "", "  ")
	hubPath := filepath.Join(dir, "hub.json")
	if err := os.WriteFile(hubPath, b, 0o644); err != nil {
		return res, err
	}
	res.Files = append(res.Files, hubPath)
	uploads := map[string]bool{}
	for _, r := range h.Rooms {
		c := h.CMContentFor(r)
		cb, _ := json.MarshalIndent(c, "", "  ")
		p := filepath.Join(dir, "servidores", r.ID, "cfg", "cm_content", "content.json")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(p, cb, 0o644); err != nil {
			return res, err
		}
		res.Files = append(res.Files, p)
		for _, e := range append([]ContentEntry{r.Track}, r.Cars...) {
			if e.File != "" && e.URL == "" {
				uploads[e.File] = true
			}
		}
	}
	for f := range uploads {
		res.Upload = append(res.Upload, f)
	}
	sort.Strings(res.Upload)
	return res, nil
}
