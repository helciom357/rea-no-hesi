package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeCar(t *testing.T, ac, id, version string) {
	d := carDir(ac, id)
	write(t, filepath.Join(d, "data.acd"), "x")
	write(t, filepath.Join(d, "body.kn5"), "x")
	write(t, filepath.Join(d, "collider.kn5"), "x")
	v := ""
	if version != "" {
		v = `"version": "` + version + `",`
	}
	write(t, filepath.Join(d, "ui", "ui_car.json"), "{\n\t\"name\": \"Car "+id+"\",\n\t"+v+"\n\t\"brand\": \"X\"\n}")
}

func fakeTrack(t *testing.T, ac, id, layout, version string) {
	d := trackDir(ac, id)
	write(t, filepath.Join(d, id+".kn5"), "x")
	ui := `{"name": "Track", "version": "` + version + `"}`
	if layout == "" {
		write(t, filepath.Join(d, "models.ini"), "x")
		write(t, filepath.Join(d, "data", "surfaces.ini"), "x")
		write(t, filepath.Join(d, "ui", "ui_track.json"), ui)
		return
	}
	write(t, filepath.Join(d, "models_"+layout+".ini"), "x")
	write(t, filepath.Join(d, layout, "data", "surfaces.ini"), "x")
	write(t, filepath.Join(d, "ui", layout, "ui_track.json"), ui)
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		s, l string
		want bool
	}{
		{"1.1", "1.0", true}, {"1.10", "1.9", true}, {"1.0", "1.0", false},
		{"1.0", "1.0.1", false}, {"2", "1.9.9", true}, {"1.0", "", true}, {"", "1.0", false},
		{"v1.2", "1.1", true},
	}
	for _, c := range cases {
		if got := NewerVersion(c.s, c.l); got != c.want {
			t.Errorf("NewerVersion(%q,%q)=%v", c.s, c.l, got)
		}
	}
}

func TestCheckCarAndTrack(t *testing.T) {
	ac := t.TempDir()
	if s := CheckCar(ac, "nope"); s.Found {
		t.Fatal("missing car reported as found")
	}
	fakeCar(t, ac, "opala", "1.2")
	s := CheckCar(ac, "opala")
	if !s.Found || !s.Healthy || s.Version != "1.2" {
		t.Fatalf("healthy car: %+v", s)
	}
	os.Remove(filepath.Join(carDir(ac, "opala"), "collider.kn5"))
	if s := CheckCar(ac, "opala"); s.Healthy {
		t.Fatal("car without collider reported healthy")
	}
	fakeTrack(t, ac, "interlagos", "", "1.0")
	if s := CheckTrack(ac, "interlagos", ""); !s.Healthy || s.Version != "1.0" {
		t.Fatalf("single layout track: %+v", s)
	}
	fakeTrack(t, ac, "band", "trafego", "2.0")
	if s := CheckTrack(ac, "band", "trafego"); !s.Healthy || s.Version != "2.0" {
		t.Fatalf("layout track: %+v", s)
	}
	if s := CheckTrack(ac, "band", "outro"); s.Healthy {
		t.Fatal("missing layout reported healthy")
	}
}

func TestSetVersionInUI(t *testing.T) {
	in := []byte("{\n\t\"name\": \"Opala\"\n}")
	out := SetVersionInUI(in, "1.3")
	if !strings.Contains(string(out), `"version": "1.3"`) {
		t.Fatalf("version not inserted: %s", out)
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil || v["version"] != "1.3" {
		t.Fatalf("result not valid json: %s %v", out, err)
	}
	out2 := SetVersionInUI(out, "1.4")
	if strings.Count(string(out2), "version") != 1 || !strings.Contains(string(out2), "1.4") {
		t.Fatalf("version not replaced: %s", out2)
	}
}

func makeZip(t *testing.T, p string, files map[string]string) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for n, c := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(c))
	}
	zw.Close()
	f.Close()
}

func carFiles(prefix, version string) map[string]string {
	return map[string]string{
		prefix + "data.acd":       "x",
		prefix + "body.kn5":       "x",
		prefix + "collider.kn5":   "x",
		prefix + "ui/ui_car.json": `{"name":"Opala","version":"` + version + `"}`,
	}
}

func TestInstallArchiveLayouts(t *testing.T) {
	for _, prefix := range []string{"content/cars/opala/", "opala/", ""} {
		ac := t.TempDir()
		os.MkdirAll(filepath.Join(ac, "content", "cars"), 0o755)
		staging := filepath.Join(ac, "content", ".s")
		os.MkdirAll(staging, 0o755)
		z := filepath.Join(t.TempDir(), "p.zip")
		makeZip(t, z, carFiles(prefix, "1.0"))
		if err := InstallArchive(z, ac, staging, "car", "opala"); err != nil {
			t.Fatalf("prefix %q: %v", prefix, err)
		}
		if s := CheckCar(ac, "opala"); !s.Healthy || s.Version != "1.0" {
			t.Fatalf("prefix %q: installed car %+v", prefix, s)
		}
	}
}

func TestInstallArchiveReplacesAndRejectsTraversal(t *testing.T) {
	ac := t.TempDir()
	staging := filepath.Join(ac, "content", ".s")
	os.MkdirAll(staging, 0o755)
	fakeCar(t, ac, "opala", "1.0")
	write(t, filepath.Join(carDir(ac, "opala"), "old-only.txt"), "x")
	z := filepath.Join(t.TempDir(), "p.zip")
	makeZip(t, z, carFiles("opala/", "1.1"))
	if err := InstallArchive(z, ac, staging, "car", "opala"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(carDir(ac, "opala"), "old-only.txt")) {
		t.Fatal("old files left behind after replace")
	}
	if CheckCar(ac, "opala").Version != "1.1" {
		t.Fatal("not updated")
	}
	bad := filepath.Join(t.TempDir(), "bad.zip")
	makeZip(t, bad, map[string]string{"opala/ui/ui_car.json": "{}", "opala/../../evil.txt": "x"})
	if err := InstallArchive(bad, ac, staging, "car", "opala"); err == nil {
		t.Fatal("traversal accepted")
	}
	if exists(filepath.Join(ac, "content", "evil.txt")) || exists(filepath.Join(ac, "evil.txt")) {
		t.Fatal("traversal wrote outside")
	}
	if CheckCar(ac, "opala").Version != "1.1" {
		t.Fatal("failed install damaged the existing car")
	}
}

func TestAllowedURL(t *testing.T) {
	h := &Hub{AllowedHosts: []string{"dl.exemplo.com.br", "*.cdn.com"}}
	ok := []string{"https://dl.exemplo.com.br/a.zip", "https://x.cdn.com/a.zip", "http://127.0.0.1:9000/a.zip"}
	bad := []string{"http://dl.exemplo.com.br/a.zip", "https://evil.com/a.zip", "", "ftp://dl.exemplo.com.br/a"}
	for _, u := range ok {
		if err := checkAllowedURL(h, u); err != nil {
			t.Errorf("%s rejected: %v", u, err)
		}
	}
	for _, u := range bad {
		if checkAllowedURL(h, u) == nil {
			t.Errorf("%s accepted", u)
		}
	}
}

func TestStatusAndModeDedup(t *testing.T) {
	ac := t.TempDir()
	h := ExampleBase()
	fakeTrack(t, ac, "br_interlagos", "", "1.0")
	fakeCar(t, ac, "br_opala_ss", "0.9") // outdated in both rooms
	fakeCar(t, ac, "br_fusca_1300", "1.0")
	os.Remove(filepath.Join(carDir(ac, "br_fusca_1300"), "data.acd")) // broken
	st := StatusForRoom(h, ac, *h.Room("interlagos-passeio"))
	got := map[string]string{}
	for _, it := range st.Items {
		got[it.ID] = it.State
	}
	want := map[string]string{"br_interlagos": "ok", "br_opala_ss": "outdated", "br_fusca_1300": "broken"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %s want %s", k, got[k], v)
		}
	}
	all := PendingForRooms(h, ac, h.Rooms)
	n := 0
	for _, it := range all {
		if it.ID == "br_opala_ss" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("shared car listed %d times", n)
	}
}

func TestBuildInspectExport(t *testing.T) {
	ac := t.TempDir()
	fakeCar(t, ac, "br_opala_ss", "")
	pkgs := t.TempDir()
	info, err := BuildPackage(ac, pkgs, "car", "br_opala_ss", "1.5")
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "car" || info.ID != "br_opala_ss" || info.Version != "1.5" || len(info.SHA256) != 64 {
		t.Fatalf("package info: %+v", info)
	}
	if v := CheckCar(ac, "br_opala_ss").Version; v != "" {
		t.Fatalf("local install was modified: %q", v)
	}
	// The built package installs cleanly on another PC.
	other := t.TempDir()
	staging := filepath.Join(other, "content", ".s")
	os.MkdirAll(staging, 0o755)
	if err := InstallArchive(filepath.Join(pkgs, "br_opala_ss.zip"), other, staging, "car", "br_opala_ss"); err != nil {
		t.Fatal(err)
	}
	if s := CheckCar(other, "br_opala_ss"); !s.Healthy || s.Version != "1.5" {
		t.Fatalf("round trip: %+v", s)
	}
	out := t.TempDir()
	res, err := Export(ExampleBase(), out)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "servidores", "interlagos-passeio", "cfg", "cm_content", "content.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c CMContent
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if c.Track == nil || c.Track.URL != "https://downloads.exemplo.com.br/ac/br_interlagos.zip" || c.Cars["br_fusca_1300"].Version != "1.0" {
		t.Fatalf("content.json: %s", b)
	}
	if len(res.Upload) != 5 {
		t.Fatalf("upload list: %v", res.Upload)
	}
}

func TestInstallerEndToEnd(t *testing.T) {
	pkgDir := t.TempDir()
	srcAC := t.TempDir()
	fakeCar(t, srcAC, "br_opala_ss", "")
	info, err := BuildPackage(srcAC, pkgDir, "car", "br_opala_ss", "2.0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(pkgDir)))
	defer srv.Close()
	h := &Hub{Name: "t", DownloadBase: srv.URL}
	ac := t.TempDir()
	os.MkdirAll(filepath.Join(ac, "content", "cars"), 0o755)
	items := []Item{{Kind: "car", ID: "br_opala_ss", URL: srv.URL + "/br_opala_ss.zip", SHA256: info.SHA256, Size: info.Size}}
	var in Installer
	if _, err := in.Start(h, ac, items); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for in.Snapshot().Status == "running" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	j := in.Snapshot()
	if j.Status != "done" {
		t.Fatalf("job %s: %+v", j.Status, j.Items[0])
	}
	if s := CheckCar(ac, "br_opala_ss"); !s.Healthy || s.Version != "2.0" {
		t.Fatalf("installed: %+v", s)
	}
	// Wrong hash must be refused and leave nothing installed.
	ac2 := t.TempDir()
	os.MkdirAll(filepath.Join(ac2, "content", "cars"), 0o755)
	items[0].SHA256 = strings.Repeat("0", 64)
	var in2 Installer
	in2.Start(h, ac2, items)
	for in2.Snapshot().Status == "running" {
		time.Sleep(20 * time.Millisecond)
	}
	if in2.Snapshot().Status != "error" || CheckCar(ac2, "br_opala_ss").Found {
		t.Fatal("bad hash was installed")
	}
}

func TestAPIGuard(t *testing.T) {
	a := NewApp()
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()
	a.addr = strings.TrimPrefix(ts.URL, "http://")
	resp, _ := http.Get(ts.URL + "/api/state")
	if resp.StatusCode != 403 {
		t.Fatalf("api without token: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/state", nil)
	req.Header.Set("X-Token", a.token)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("api with token: %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/")
	if resp.StatusCode != 200 {
		t.Fatalf("player page: %d", resp.StatusCode)
	}
}
