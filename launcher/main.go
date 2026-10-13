// Launcher Brasil: one Windows executable that checks which cars and tracks a
// room needs, installs what is missing and hands the join to Content Manager.
// It also contains the admin panel that builds the base (hub.json) and the
// cm_content/content.json for each AssettoServer room.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// Version is set at build time with -ldflags "-X main.Version=...".
var Version = "0.1.0-dev"

func main() {
	admin := flag.Bool("admin", false, "abrir direto no painel do administrador")
	port := flag.Int("port", 0, "porta local da interface (0 = automática)")
	noBrowser := flag.Bool("no-browser", false, "não abrir o navegador")
	flag.Parse()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Não foi possível iniciar a interface:", err)
		pause()
		os.Exit(1)
	}
	app := NewApp()
	app.addr = ln.Addr().String()
	page := "http://" + app.addr + "/"
	if *admin {
		page += "admin"
	}
	fmt.Printf("Launcher Brasil %s\n\nInterface aberta em %s\nMantenha esta janela aberta enquanto usa o launcher. Feche-a para sair.\n", Version, page)
	if !*noBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			if err := openExternal(page); err != nil {
				fmt.Println("Abra este endereço no navegador:", page)
			}
		}()
	}
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func pause() {
	fmt.Println("Pressione Enter para sair.")
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
}
