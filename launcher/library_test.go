package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func eventFixture(t *testing.T) (*Library, *Hub) {
	lib := OpenLibrary(t.TempDir())
	files := merge(carIn("br_opala/", "Opala SS", ""), carIn("br_fusca/", "Fusca", ""), map[string]string{
		"br_band/band.kn5": "k", "br_band/models_trafego.ini": "m", "br_band/trafego/data/surfaces.ini": "s",
		"br_band/ui/trafego/ui_track.json": `{"name": "Bandeirantes"}`, "br_band/ui/trafego/preview.png": "PNG",
	})
	if _, err := lib.Import(zipOf(t, files)); err != nil {
		t.Fatal(err)
	}
	d := EventDraft{Name: "Racha de Opala", Invite: "acmanager://race/online/join?ip=1.2.3.4&httpPort=8081",
		Track: "br_band", Layout: "trafego", Cars: []string{"br_opala", "br_fusca"}}
	p, err := BuildEvent(d, lib)
	if err != nil {
		t.Fatal(err)
	}
	return lib, &Hub{Name: "Base", FilesURL: "https://site.com.br/ac/files", Events: []Event{p.Event}}
}

func TestBuildEventAndProblems(t *testing.T) {
	lib, h := eventFixture(t)
	ev := h.Events[0]
	if !strings.HasPrefix(ev.ID, "racha-de-opala-") || ev.Server.Host != "1.2.3.4" || ev.Server.HTTPPort != 8081 {
		t.Fatalf("event = %+v", ev)
	}
	op := ev.Cars[0]
	if !regexp.MustCompile(`^car-br_opala-[0-9a-f]{12}\.zip$`).MatchString(op.File) || len(op.Checks) == 0 {
		t.Fatalf("car entry = %+v", op)
	}
	if h.DownloadURL(op) != "https://site.com.br/ac/files/"+op.File || h.AllowedHost() != "site.com.br" {
		t.Fatalf("urls: %s %s", h.DownloadURL(op), h.AllowedHost())
	}
	bad := EventDraft{Private: true, LAN: true, Invite: "oi", Track: "nao_existe", Cars: []string{"br_opala", "x1", "x2"}}
	p := strings.Join(bad.Problems(lib), " | ")
	for _, want := range []string{"nome", "senha", "Radmin", "race invite", "pista nao_existe", "2 carros"} {
		if !strings.Contains(p, want) {
			t.Errorf("problems %q missing %q", p, want)
		}
	}
}

func TestChecksDetectDifferentFiles(t *testing.T) {
	lib, h := eventFixture(t)
	ac := t.TempDir()
	staging := filepath.Join(ac, "content", ".s")
	os.MkdirAll(staging, 0o755)
	for _, kind := range []string{"car", "track"} {
		for _, it := range lib.List() {
			if it.Kind == kind {
				if err := InstallArchive(lib.Path(it), ac, staging, kind, it.ID); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if st := StatusForEvent(h, ac, h.Events[0]); !st.Ready {
		t.Fatalf("fresh install not ready: %+v", st.Items)
	}
	// Same version number, different physics: the handshake would fail, so
	// the launcher must flag it.
	os.WriteFile(filepath.Join(carDir(ac, "br_opala"), "data.acd"), []byte("data-OUTRA"), 0o644)
	// Changes in skins or ui do not matter.
	os.WriteFile(filepath.Join(carDir(ac, "br_fusca"), "skins", "branco", "preview.jpg"), []byte("outra foto"), 0o644)
	st := StatusForEvent(h, ac, h.Events[0])
	got := map[string]string{}
	for _, it := range st.Items {
		got[it.ID] = it.State
	}
	if got["br_opala"] != "outdated" || got["br_fusca"] != "ok" || got["br_band"] != "ok" {
		t.Fatalf("states = %v", got)
	}
}

func TestParseInviteAndSplitTrack(t *testing.T) {
	cases := map[string]Invite{
		"acmanager://race/online/join?ip=26.12.3.4&httpPort=8081&password=abc":      {Host: "26.12.3.4", HTTPPort: 8081},
		"https://acstuff.club/s/q:race/online/join?ip=meu.server.com&httpPort=9000": {Host: "meu.server.com", HTTPPort: 9000},
		"  192.168.0.10:8081 ": {Host: "192.168.0.10", HTTPPort: 8081},
	}
	for in, want := range cases {
		got, err := ParseInvite(in)
		if err != nil || got.Host != want.Host || got.HTTPPort != want.HTTPPort {
			t.Errorf("ParseInvite(%q) = %+v %v", in, got, err)
		}
	}
	inv, _ := ParseInvite("acmanager://race/online/join?ip=1.2.3.4&httpPort=8081&password=a%20b")
	if l := inv.JoinLink(); !strings.HasPrefix(l, "acmanager://race/online/join?") || !strings.Contains(l, "password=a+b") {
		t.Errorf("join link %s", l)
	}
	for _, bad := range []string{"", "http://evil.com", "acmanager://x?ip=1.2.3.4", "1.2.3.4:99999"} {
		if _, err := ParseInvite(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	known := func(id, layout string) bool {
		return id == "shuto-revival" && layout == "main" || id == "br_av_brasil" && layout == ""
	}
	for raw, want := range map[string][2]string{
		"shuto-revival-main":              {"shuto-revival", "main"},
		"br_av_brasil":                    {"br_av_brasil", ""},
		"csp/2144/../H/../ks_red_bull-gp": {"ks_red_bull", "gp"},
		"desconhecida":                    {"desconhecida", ""},
	} {
		if a, b := SplitTrack(raw, known); a != want[0] || b != want[1] {
			t.Errorf("SplitTrack(%q) = %q %q", raw, a, b)
		}
	}
}

func TestHostCode(t *testing.T) {
	code := EncodeHostCode("https://site.com.br/ac/upload.php", "abc123")
	u, k, err := DecodeHostCode(" " + code[:10] + "\n" + code[10:] + " ")
	if err != nil || u != "https://site.com.br/ac/upload.php" || k != "abc123" {
		t.Fatalf("decode = %q %q %v", u, k, err)
	}
	if _, _, err := DecodeHostCode("LB1-quebrado"); err == nil {
		t.Fatal("broken code accepted")
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

func waitTask(t *testing.T, pub *Publisher, task *Task) Task {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		tasks, _, _ := pub.act.Snapshot()
		for _, x := range tasks {
			if x.ID == task.ID && (x.State == "done" || x.State == "error") {
				return x
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return Task{}
}

// TestEventsWithRealPHP runs the whole flow against receiver/upload.php:
// owner creates a host, the host publishes a public and a private event,
// a player lists, unlocks and installs them.
func TestEventsWithRealPHP(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("APPDATA", cfg)
	ownerKey := NewKey()
	endpoint := startPHP(t, ownerKey)
	ctx := context.Background()
	owner := NewRemote(endpoint, ownerKey)
	if id, err := owner.Ping(ctx); err != nil || id.Role != "owner" {
		t.Fatalf("owner ping: %+v %v", id, err)
	}
	if _, err := NewRemote(endpoint, "errada").Ping(ctx); err == nil {
		t.Fatal("wrong key accepted")
	}
	if err := owner.Call(ctx, "base-save", map[string]string{"name": "Rodovias BR"}, nil); err != nil {
		t.Fatal(err)
	}
	var added struct{ ID, Key string }
	if err := owner.Call(ctx, "host-add", map[string]string{"name": "Zé do Opala"}, &added); err != nil {
		t.Fatal(err)
	}
	ep, key, _ := DecodeHostCode(EncodeHostCode(endpoint, added.Key))
	host := NewRemote(ep, key)
	if id, err := host.Ping(ctx); err != nil || id.Role != "host" || id.Name != "Zé do Opala" {
		t.Fatalf("host ping: %+v %v", id, err)
	}
	if err := host.Call(ctx, "host-list", nil, nil); err == nil {
		t.Fatal("host managed hosts")
	}
	if err := host.Upload(ctx, bytesOpener([]byte("x")), "car-x.php-0123456789ab.zip", "", func(int64, int64) {}); err == nil {
		t.Fatal("script name accepted")
	}

	// The host publishes through the Publisher, exactly as the panel does.
	s := LoadSettings()
	s.UploadURL, s.UploadKey = endpoint, key
	SaveSettings(s)
	lib, _ := eventFixture(t)
	pub := NewPublisher(lib)
	pubDraft := EventDraft{Name: "Bandeirantes aberto", Invite: "1.2.3.4:8081", Track: "br_band", Layout: "trafego", Cars: []string{"br_opala", "br_fusca"}}
	privDraft := EventDraft{Name: "Racha fechado", Private: true, Password: "segredo", LAN: true, RadminNet: "RachaBR", RadminPass: "123",
		Invite: "acmanager://race/online/join?ip=26.1.2.3&httpPort=8081", Track: "br_band", Layout: "trafego", Cars: []string{"br_opala"}}
	for _, d := range []EventDraft{pubDraft, privDraft} {
		task, err := pub.PublishEvent(d)
		if err != nil {
			t.Fatal(err)
		}
		if r := waitTask(t, pub, task); r.State != "done" {
			t.Fatalf("publish %s: %+v", d.Name, r)
		}
	}
	for _, it := range lib.List() {
		if !it.Uploaded {
			t.Fatalf("%s not uploaded", it.ID)
		}
	}

	// Player view: the private event is listed but locked.
	hubLink := NewRemote(endpoint, "").HubURL()
	hub, err := LoadHub(hubLink)
	if err != nil {
		t.Fatal(err)
	}
	if hub.Name != "Rodovias BR" || len(hub.Events) != 2 || hub.FilesURL != FilesURLFor(endpoint) {
		t.Fatalf("hub = %+v", hub)
	}
	var open, closed Event
	for _, e := range hub.Events {
		if e.Private {
			closed = e
		} else {
			open = e
		}
	}
	if !closed.Locked || closed.Invite != "" || closed.Radmin != nil || closed.Cars[0].File != "" || closed.Host != "Zé do Opala" {
		t.Fatalf("private event leaks: %+v", closed)
	}
	if open.Locked || open.Invite == "" || open.Cars[0].File == "" {
		t.Fatalf("public event: %+v", open)
	}
	if _, err := Unlock(ctx, hubLink, closed.ID, "errada"); err == nil {
		t.Fatal("wrong event password accepted")
	}
	full, err := Unlock(ctx, hubLink, closed.ID, "segredo")
	if err != nil || full.Locked || full.Radmin == nil || full.Radmin.Network != "RachaBR" || full.Invite == "" {
		t.Fatalf("unlock: %+v %v", full, err)
	}

	// Install everything from the site and check the event is ready.
	ac := t.TempDir()
	os.MkdirAll(filepath.Join(ac, "content", "cars"), 0o755)
	items := PendingForEvents(hub, ac, []Event{open})
	if len(items) != 3 {
		t.Fatalf("pending = %d", len(items))
	}
	var in Installer
	if _, err := in.Start(hub, ac, items); err != nil {
		t.Fatal(err)
	}
	for in.Snapshot().Status == "running" {
		time.Sleep(20 * time.Millisecond)
	}
	if j := in.Snapshot(); j.Status != "done" {
		t.Fatalf("install: %+v", j.Items[0])
	}
	if st := StatusForEvent(hub, ac, open); !st.Ready {
		t.Fatalf("not ready: %+v", st.Items)
	}

	// Another host cannot edit or delete it; the owner can.
	var other struct{ ID, Key string }
	owner.Call(ctx, "host-add", map[string]string{"name": "Outro"}, &other)
	intruder := NewRemote(endpoint, other.Key)
	if err := intruder.Call(ctx, "event-delete", map[string]string{"id": open.ID}, nil); err == nil {
		t.Fatal("other host deleted the event")
	}
	var mine struct{ Events []Event }
	host.Call(ctx, "my-events", nil, &mine)
	if len(mine.Events) != 2 {
		t.Fatalf("my events = %d", len(mine.Events))
	}
	if err := owner.Call(ctx, "event-delete", map[string]string{"id": open.ID}, nil); err != nil {
		t.Fatal(err)
	}
	// Removing a host removes their events.
	owner.Call(ctx, "host-remove", map[string]string{"id": added.ID}, nil)
	if _, err := host.Ping(ctx); err == nil {
		t.Fatal("removed host still works")
	}
	hub, _ = LoadHub(hubLink)
	if len(hub.Events) != 0 {
		t.Fatalf("events left: %d", len(hub.Events))
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
	_, h := eventFixture(t)
	ac := t.TempDir()
	fakeCar(t, ac, "outro_mod", "")
	mods := ListMods(ac, h)
	got := map[string]ModEntry{}
	for _, m := range mods {
		got[m.ID] = m
	}
	if got["br_fusca"].State != "missing" || got["br_fusca"].Installed || !got["br_fusca"].Required || len(got["br_fusca"].Events) != 1 {
		t.Fatalf("fusca: %+v", got["br_fusca"])
	}
	if got["outro_mod"].Required || !got["outro_mod"].Installed || got["outro_mod"].Size == 0 {
		t.Fatalf("other: %+v", got["outro_mod"])
	}
	if !mods[0].Required || mods[0].State == "ok" {
		t.Fatalf("missing items should come first: %+v", mods[0])
	}
}
