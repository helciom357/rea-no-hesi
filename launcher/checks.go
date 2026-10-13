package main

import (
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// FileCheck pins one file that decides whether a player's copy of a mod is
// the same one the server runs: the files Assetto Corsa's handshake compares
// (data.acd, surfaces.ini) and the models (.kn5). Other files (skins, ui,
// CSP extras a player added) are ignored, so a local tweak there does not
// force a re-download.
type FileCheck struct {
	Path  string `json:"p"`           // relative to the mod folder, lower case, "/"
	Size  int64  `json:"s"`           // bytes
	CRC32 string `json:"c,omitempty"` // only for small files where content matters
}

// checkedFile tells whether rel (lower case, relative to the mod folder)
// takes part in the comparison, and whether its content is hashed.
func checkedFile(kind, rel string) (checked, withCRC bool) {
	dir, base := path.Split(rel)
	switch kind {
	case "car":
		switch {
		case rel == "data.acd":
			return true, true
		case dir == "" && strings.HasSuffix(base, ".kn5"):
			return true, false
		case dir == "data/" && strings.HasSuffix(base, ".ini"):
			return true, true // unpacked data folder
		}
	case "track":
		switch {
		case dir == "" && strings.HasSuffix(base, ".kn5"):
			return true, false
		case dir == "" && strings.HasPrefix(base, "models") && strings.HasSuffix(base, ".ini"):
			return true, true
		case base == "surfaces.ini" && (dir == "data/" || strings.Count(dir, "/") == 2 && strings.HasSuffix(dir, "/data/")):
			return true, true
		}
	}
	return false, false
}

// PkgFile is one file of a package as recorded in the zip directory.
type PkgFile struct {
	Rel   string
	Size  int64
	CRC32 uint32
}

// ChecksFromList builds the checks from the files of a package.
func ChecksFromList(kind string, files []PkgFile) []FileCheck {
	var out []FileCheck
	for _, f := range files {
		rel := strings.ToLower(f.Rel)
		ok, crc := checkedFile(kind, rel)
		if !ok {
			continue
		}
		c := FileCheck{Path: rel, Size: f.Size}
		if crc {
			c.CRC32 = fmt.Sprintf("%08x", f.CRC32)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// CompareChecks reports which pinned files differ in the installed mod. An
// empty result means the player has the same files as the server.
func CompareChecks(dir string, checks []FileCheck) []string {
	// Folder names are case-insensitive on Windows; match them that way here too.
	local := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		local[strings.ToLower(filepath.ToSlash(rel))] = p
		return nil
	})
	var diff []string
	for _, c := range checks {
		p, ok := local[c.Path]
		if !ok {
			diff = append(diff, "falta "+c.Path)
			continue
		}
		st, err := os.Stat(p)
		if err != nil || st.Size() != c.Size {
			diff = append(diff, c.Path+" diferente")
			continue
		}
		if c.CRC32 != "" && fileCRC32(p) != c.CRC32 {
			diff = append(diff, c.Path+" diferente")
		}
	}
	return diff
}

func fileCRC32(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return fmt.Sprintf("%08x", h.Sum32())
}
