package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// zipOf builds a zip with the given files (name -> content).
func zipOf(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "drop.zip")
	makeZip(t, p, files)
	return p
}

func carIn(prefix, name, version string) map[string]string {
	v := ""
	if version != "" {
		v = `"version": "` + version + `", `
	}
	return map[string]string{
		prefix + "data.acd":                  "data-" + name,
		prefix + "body.kn5":                  "body",
		prefix + "collider.kn5":              "col",
		prefix + "ui/ui_car.json":            `{"name": "` + name + `", ` + v + `"brand": "BR"}`,
		prefix + "skins/branco/preview.jpg":  "JPEGDATA",
		prefix + "skins/branco/ui_skin.json": "{}",
	}
}

func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func TestScanArchivePacks(t *testing.T) {
	names := []string{
		"Pack Clássicos/opala/ui/ui_car.json", "Pack Clássicos/opala/data.acd",
		"Pack Clássicos/maverick/ui/ui_car.json",
		"content/tracks/band/ui/trafego/ui_track.json", "content/tracks/band/ui/livre/ui_track.json",
		"content/tracks/band/band.kn5",
		"opala/backup/old/ui/ui_car.json", // nested inside another item: ignored
		"opala/ui/ui_car.json",
	}
	got := scanArchive(names)
	var ids []string
	for _, f := range got {
		ids = append(ids, f.kind+":"+f.id)
	}
	want := "car:opala car:maverick track:band"
	if strings.Join(ids, " ") != want {
		t.Fatalf("scan = %v", ids)
	}
	if strings.Join(got[2].layouts, ",") != "livre,trafego" {
		t.Fatalf("layouts = %v", got[2].layouts)
	}
}

func TestImportPackVersionsAndInstall(t *testing.T) {
	lib := OpenLibrary(t.TempDir())
	pack := zipOf(t, merge(carIn("Pack/br_opala/", "Opala SS", ""), carIn("Pack/br_maverick/", "Maverick GT", "2.3")))
	res, err := lib.Import(pack)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Status != "new" {
		t.Fatalf("import = %+v", res)
	}
	op, _ := lib.Get("car", "br_opala")
	mv, _ := lib.Get("car", "br_maverick")
	if op.Version != "1.0" || mv.Version != "2.3" || op.Name != "Opala SS" || op.Image == "" {
		t.Fatalf("items: %+v %+v", op, mv)
	}
	// Same content again: nothing changes.
	res, _ = lib.Import(zipOf(t, carIn("br_opala/", "Opala SS", "")))
	if res[0].Status != "unchanged" || res[0].Item.Version != "1.0" {
		t.Fatalf("re-import = %+v", res[0])
	}
	// Changed content: version bumps automatically.
	changed := carIn("br_opala/", "Opala SS", "")
	changed["br_opala/data.acd"] = "new physics"
	res, _ = lib.Import(zipOf(t, changed))
	if res[0].Status != "updated" || res[0].Item.Version != "1.1" {
		t.Fatalf("update = %+v", res[0])
	}
	// The normalized package installs and carries the version.
	ac := t.TempDir()
	staging := filepath.Join(ac, "content", ".s")
	os.MkdirAll(staging, 0o755)
	op, _ = lib.Get("car", "br_opala")
	if err := InstallArchive(lib.Path(op), ac, staging, "car", "br_opala"); err != nil {
		t.Fatal(err)
	}
	if s := CheckCar(ac, "br_opala"); !s.Healthy || s.Version != "1.1" {
		t.Fatalf("installed: %+v", s)
	}
	if b, _ := os.ReadFile(filepath.Join(carDir(ac, "br_opala"), "data.acd")); string(b) != "new physics" {
		t.Fatalf("raw copy changed data: %q", b)
	}
}

func TestImportRejectsNonMods(t *testing.T) {
	lib := OpenLibrary(t.TempDir())
	if _, err := lib.Import(zipOf(t, map[string]string{"leia-me.txt": "x"})); err == nil {
		t.Fatal("zip without mods accepted")
	}
	bad := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(bad, []byte("not a zip"), 0o644)
	if _, err := lib.Import(bad); err == nil {
		t.Fatal("non-zip accepted")
	}
}

func TestBumpVersionAndSlug(t *testing.T) {
	for in, want := range map[string]string{"": "1.0", "1.0": "1.1", "1.9": "1.10", "v2": "3", "beta": "beta.1"} {
		if got := BumpVersion(in); got != want {
			t.Errorf("BumpVersion(%q)=%q want %q", in, got, want)
		}
	}
	if s := Slug("Av. Brasil — Tráfego Pesado!"); s != "av-brasil-trafego-pesado" {
		t.Errorf("slug = %q", s)
	}
}

func baseFixture(t *testing.T) (*AdminBase, *Library) {
	lib := OpenLibrary(t.TempDir())
	files := merge(carIn("br_opala/", "Opala SS", ""), carIn("br_fusca/", "Fusca", ""), map[string]string{
		"br_band/band.kn5": "k", "br_band/models.ini": "m", "br_band/data/surfaces.ini": "s",
		"br_band/ui/ui_track.json": `{"name": "Bandeirantes"}`, "br_band/ui/preview.png": "PNG",
	})
	if _, err := lib.Import(zipOf(t, files)); err != nil {
		t.Fatal(err)
	}
	a := &AdminBase{Name: "Base BR", Rooms: []AdminRoom{
		{Name: "Bandeirantes", Mode: "Tráfego pesado", Host: "1.2.3.4", HTTPPort: 8081, Track: "br_band", Cars: []string{"br_opala", "br_fusca"}},
		{Name: "Racha", Mode: "Racha", Host: "1.2.3.4", HTTPPort: 8082, Track: "br_band", Cars: []string{"br_opala"}},
		{Name: "Incompleta", Mode: "Racha", Track: "br_band"},
	}}
	a.normalize()
	return a, lib
}

func TestBuildHubSkipsIncompleteRooms(t *testing.T) {
	a, lib := baseFixture(t)
	h := BuildHub(a, lib, "https://site.com.br/ac/files")
	if len(h.Rooms) != 2 || len(h.Modes) != 2 || h.Modes[0].ID != "trafego-pesado" {
		t.Fatalf("hub rooms=%d modes=%+v", len(h.Rooms), h.Modes)
	}
	if p := RoomProblems(a.Rooms[2], lib); len(p) < 2 {
		t.Fatalf("problems = %v", p)
	}
	r := h.Rooms[0]
	if r.ID != "bandeirantes" || r.Image != "https://site.com.br/ac/files/img-track-br_band.png" {
		t.Fatalf("room = %+v", r)
	}
	if h.DownloadURL(r.Cars[0]) != "https://site.com.br/ac/files/car-br_opala.zip" || h.AllowedHosts[0] != "site.com.br" {
		t.Fatalf("urls: %s %v", h.DownloadURL(r.Cars[0]), h.AllowedHosts)
	}
	if len(h.Validate()) != 0 {
		t.Fatalf("published hub invalid: %v", h.Validate())
	}
	ac := t.TempDir()
	if n := len(PendingForRooms(h, ac, h.Rooms)); n != 3 {
		t.Fatalf("pending across rooms = %d, want 3 (shared items once)", n)
	}
	b, err := CMContentJSON(h, "racha")
	if err != nil || !strings.Contains(string(b), `"br_opala"`) {
		t.Fatalf("content.json: %s %v", b, err)
	}
}

// startPHP serves receiver/upload.php with the real PHP binary.
func startPHP(t *testing.T, key string) string {
	t.Helper()
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php não instalado")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "upload.php"), ReceiverPHP(key), 0o644)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	cmd := exec.Command(php, "-S", addr, "-t", dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "http://" + addr + "/upload.php"
}

func TestRemoteUploadWithRealPHP(t *testing.T) {
	key := NewKey()
	endpoint := startPHP(t, key)
	ctx := context.Background()
	if err := NewRemote(endpoint, "errada").Ping(ctx); err == nil {
		t.Fatal("wrong key accepted")
	}
	r := NewRemote(endpoint, key)
	if err := r.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	// 10 MB file: three chunks, then resume after a simulated interruption.
	data := make([]byte, 10<<20+123)
	for i := range data {
		data[i] = byte(i * 7)
	}
	sum := sha256Hex(data)
	if err := r.Upload(ctx, bytesOpener(data), "car-x.zip", sum, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	size, _, _ := r.RemoteSize(ctx, "car-x.zip")
	if size != int64(len(data)) {
		t.Fatalf("remote size %d", size)
	}
	if err := r.Upload(ctx, bytesOpener(data), "car-y.zip", strings.Repeat("0", 64), func(int64, int64) {}); err == nil {
		t.Fatal("wrong sha accepted")
	}
	if err := r.Upload(ctx, bytesOpener([]byte("x")), "../evil.php", "", func(int64, int64) {}); err == nil {
		t.Fatal("bad name accepted")
	}
	if r.FilesURL() != strings.TrimSuffix(endpoint, "/upload.php")+"/files" {
		t.Fatalf("files url %s", r.FilesURL())
	}
	// Served publicly from files/.
	resp, err := http.Get(r.FilesURL() + "/car-x.zip")
	if err != nil || resp.StatusCode != 200 || resp.ContentLength != int64(len(data)) {
		t.Fatalf("public download: %v %v", resp, err)
	}
}

func TestPublisherEndToEndWithPHP(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("APPDATA", cfg)
	key := NewKey()
	endpoint := startPHP(t, key)
	s := LoadSettings()
	s.UploadURL, s.UploadKey = endpoint, key
	SaveSettings(s)

	a, lib := baseFixture(t)
	SaveAdminBase(LoadSettings().AdminBase, a)
	pub := NewPublisher(lib)
	pub.SyncAll()
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, at, busy := pub.act.Snapshot()
		if !busy && !at.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			tasks, _, _ := pub.act.Snapshot()
			t.Fatalf("publish did not finish: %+v", tasks)
		}
		time.Sleep(50 * time.Millisecond)
	}
	hub, err := LoadHub(NewRemote(endpoint, "").HubURL())
	if err != nil {
		t.Fatal(err)
	}
	if len(hub.Rooms) != 2 {
		t.Fatalf("published rooms = %d", len(hub.Rooms))
	}
	for _, it := range lib.List() {
		if !it.Uploaded {
			t.Fatalf("%s not uploaded", it.ID)
		}
	}
	// A player installs straight from the published base.
	ac := t.TempDir()
	os.MkdirAll(filepath.Join(ac, "content", "cars"), 0o755)
	items := PendingForRooms(hub, ac, hub.Rooms)
	var in Installer
	if _, err := in.Start(hub, ac, items); err != nil {
		t.Fatal(err)
	}
	for in.Snapshot().Status == "running" {
		time.Sleep(20 * time.Millisecond)
	}
	if j := in.Snapshot(); j.Status != "done" {
		t.Fatalf("install: %+v", j.Items)
	}
	for _, r := range hub.Rooms {
		if st := StatusForRoom(hub, ac, r); !st.Ready {
			t.Fatalf("room %s not ready: %+v", r.ID, st.Items)
		}
	}
}

func TestLiveSlots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/INFO" {
			fmt.Fprint(w, `{"clients":2,"maxclients":3}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"Cars": []map[string]any{
			{"Model": "br_opala", "IsConnected": true}, {"Model": "br_opala", "IsConnected": false}, {"Model": "br_fusca", "IsConnected": true},
		}})
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	var p int
	fmt.Sscan(port, &p)
	li := FetchLive(ServerAddr{Host: host, HTTPPort: p})
	if !li.Online || li.Clients != 2 || li.Slots["br_opala"] != (SlotInfo{2, 1}) || li.Slots["br_fusca"].Free != 0 {
		t.Fatalf("live = %+v", li)
	}
}

func TestModsList(t *testing.T) {
	a, lib := baseFixture(t)
	h := BuildHub(a, lib, "https://site/files")
	ac := t.TempDir()
	fakeCar(t, ac, "br_opala", "1.0")
	fakeCar(t, ac, "outro_mod", "")
	mods := ListMods(ac, h)
	got := map[string]ModEntry{}
	for _, m := range mods {
		got[m.ID] = m
	}
	if got["br_fusca"].State != "missing" || got["br_fusca"].Installed || !got["br_fusca"].Required {
		t.Fatalf("fusca: %+v", got["br_fusca"])
	}
	if got["br_opala"].State != "ok" || !got["br_opala"].Installed || len(got["br_opala"].Rooms) != 2 {
		t.Fatalf("opala: %+v", got["br_opala"])
	}
	if got["outro_mod"].Required || !got["outro_mod"].Installed || got["outro_mod"].Size == 0 {
		t.Fatalf("other: %+v", got["outro_mod"])
	}
	if mods[0].State != "missing" {
		t.Fatalf("missing items should come first: %+v", mods[0])
	}
	var _ = zip.Store
}
