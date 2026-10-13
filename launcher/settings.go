package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// Settings are the player's local preferences, stored in the user config dir.
type Settings struct {
	ACPath    string `json:"acPath"`
	HubSource string `json:"hubSource"` // URL or local path of hub.json
	// Admin side.
	AdminBase  string `json:"adminBase"`  // path of the admin's base.json
	LibraryDir string `json:"libraryDir"` // packages added to the base
	UploadURL  string `json:"uploadUrl"`  // https://site/ac/upload.php
	UploadKey  string `json:"uploadKey"`
}

var settingsMu sync.Mutex

func appDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	d = filepath.Join(d, "LauncherBrasil")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(p)
}

func settingsPath() string { return filepath.Join(appDir(), "settings.json") }

func LoadSettings() Settings {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	var s Settings
	if b, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.ACPath == "" {
		s.ACPath = DetectACPath()
	}
	if s.HubSource == "" {
		// A hub.json shipped next to the .exe is used automatically.
		if p := filepath.Join(exeDir(), "hub.json"); exists(p) {
			s.HubSource = p
		}
	}
	if s.AdminBase == "" {
		s.AdminBase = filepath.Join(appDir(), "base.json")
	}
	if s.LibraryDir == "" {
		s.LibraryDir = filepath.Join(appDir(), "conteudo")
	}
	return s
}

func SaveSettings(s Settings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(settingsPath(), b, 0o600) // holds the upload key
}

// EffectiveHub is where the player view reads the base from: the configured
// link, or the admin's own published base on this PC.
func (s Settings) EffectiveHub() string {
	if s.HubSource != "" {
		return s.HubSource
	}
	if s.UploadURL != "" {
		return NewRemote(s.UploadURL, "").HubURL()
	}
	return ""
}

// IsACRoot reports whether dir looks like an Assetto Corsa install.
func IsACRoot(dir string) bool {
	return dir != "" && (exists(filepath.Join(dir, "acs.exe")) || exists(filepath.Join(dir, "content", "cars")))
}

// DetectACPath finds Assetto Corsa through Steam's library folders.
func DetectACPath() string {
	var steamRoots []string
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("reg", "query", `HKCU\Software\Valve\Steam`, "/v", "SteamPath").Output(); err == nil {
			if m := regexp.MustCompile(`SteamPath\s+REG_SZ\s+(.+)`).FindSubmatch(out); m != nil {
				steamRoots = append(steamRoots, filepath.FromSlash(strings.TrimSpace(string(m[1]))))
			}
		}
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if v := os.Getenv(env); v != "" {
				steamRoots = append(steamRoots, filepath.Join(v, "Steam"))
			}
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		steamRoots = append(steamRoots, filepath.Join(home, ".steam", "steam"), filepath.Join(home, ".local", "share", "Steam"))
	}
	libs := map[string]bool{}
	var order []string
	add := func(p string) {
		if p != "" && !libs[p] {
			libs[p] = true
			order = append(order, p)
		}
	}
	pathRe := regexp.MustCompile(`"path"\s+"([^"]+)"`)
	for _, root := range steamRoots {
		add(root)
		f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if m := pathRe.FindStringSubmatch(sc.Text()); m != nil {
				add(filepath.FromSlash(strings.ReplaceAll(m[1], `\\`, `\`)))
			}
		}
		f.Close()
	}
	for _, lib := range order {
		p := filepath.Join(lib, "steamapps", "common", "assettocorsa")
		if IsACRoot(p) {
			return p
		}
	}
	return ""
}
