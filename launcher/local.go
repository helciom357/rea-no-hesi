package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// LocalState describes one car or track as found in the player's install.
type LocalState struct {
	Found    bool     `json:"found"`
	Healthy  bool     `json:"healthy"`
	Version  string   `json:"version"`
	Problems []string `json:"problems,omitempty"`
}

func carDir(acRoot, id string) string   { return filepath.Join(acRoot, "content", "cars", id) }
func trackDir(acRoot, id string) string { return filepath.Join(acRoot, "content", "tracks", id) }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func hasGlob(dir, pattern string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, pattern))
	return len(m) > 0
}

// CheckCar mirrors what the game needs to load a car: packed or unpacked data,
// the UI description, a body model and a collider.
func CheckCar(acRoot, id string) LocalState {
	dir := carDir(acRoot, id)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return LocalState{}
	}
	s := LocalState{Found: true}
	if !exists(filepath.Join(dir, "data.acd")) && !exists(filepath.Join(dir, "data")) {
		s.Problems = append(s.Problems, "falta data.acd")
	}
	ui := filepath.Join(dir, "ui", "ui_car.json")
	if !exists(ui) {
		s.Problems = append(s.Problems, "falta ui/ui_car.json")
	} else {
		s.Version = readVersion(ui)
	}
	if !hasGlob(dir, "*.kn5") {
		s.Problems = append(s.Problems, "falta modelo .kn5")
	} else if !exists(filepath.Join(dir, "collider.kn5")) {
		s.Problems = append(s.Problems, "falta collider.kn5")
	}
	s.Healthy = len(s.Problems) == 0
	return s
}

// CheckTrack handles both single-layout tracks (models.ini, data/, ui/) and
// multi-layout tracks (models_<layout>.ini, <layout>/data/, ui/<layout>/).
func CheckTrack(acRoot, id, layout string) LocalState {
	dir := trackDir(acRoot, id)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return LocalState{}
	}
	s := LocalState{Found: true}
	if !hasGlob(dir, "*.kn5") {
		s.Problems = append(s.Problems, "falta modelo .kn5")
	}
	var models, surfaces, ui string
	if layout == "" {
		models = filepath.Join(dir, "models.ini")
		surfaces = filepath.Join(dir, "data", "surfaces.ini")
		ui = filepath.Join(dir, "ui", "ui_track.json")
	} else {
		models = filepath.Join(dir, "models_"+layout+".ini")
		if !exists(models) {
			models = filepath.Join(dir, "models.ini")
		}
		surfaces = filepath.Join(dir, layout, "data", "surfaces.ini")
		ui = filepath.Join(dir, "ui", layout, "ui_track.json")
		if !exists(filepath.Join(dir, layout)) {
			s.Problems = append(s.Problems, "falta o layout "+layout)
		}
	}
	// Tracks that ship a single .kn5 without models.ini are valid too.
	if !exists(models) && !hasGlob(dir, "models*.ini") && !hasGlob(dir, "*.kn5") {
		s.Problems = append(s.Problems, "falta models.ini")
	}
	if !exists(surfaces) {
		s.Problems = append(s.Problems, "falta data/surfaces.ini")
	}
	if exists(ui) {
		s.Version = readVersion(ui)
	} else {
		s.Problems = append(s.Problems, "falta ui_track.json")
	}
	s.Healthy = len(s.Problems) == 0
	return s
}

// ui_car.json files are often not strict JSON (comments, raw tabs), so the
// version is read with a tolerant pattern instead of a JSON decoder.
var versionRe = regexp.MustCompile(`"version"\s*:\s*"([^"]*)"`)

func readVersion(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := versionRe.FindSubmatch(b); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// SetVersionInUI returns ui json bytes with "version" set to v, keeping the
// rest of the file as it is.
func SetVersionInUI(b []byte, v string) []byte {
	repl := []byte(`"version": "` + v + `"`)
	if versionRe.Match(b) {
		return versionRe.ReplaceAllLiteral(b, repl)
	}
	i := strings.IndexByte(string(b), '{')
	if i < 0 {
		return b
	}
	out := append([]byte{}, b[:i+1]...)
	out = append(out, []byte("\n  "+string(repl)+",")...)
	return append(out, b[i+1:]...)
}

// NewerVersion reports whether server is newer than local, comparing dotted
// numbers ("1.10" > "1.9"). A missing local version counts as older.
func NewerVersion(server, local string) bool {
	if server == "" {
		return false
	}
	if local == "" {
		return true
	}
	a, b := strings.Split(server, "."), strings.Split(local, ".")
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(a) {
			x, _ = strconv.Atoi(strings.TrimLeft(a[i], "vV"))
		}
		if i < len(b) {
			y, _ = strconv.Atoi(strings.TrimLeft(b[i], "vV"))
		}
		if x != y {
			return x > y
		}
	}
	return false
}
