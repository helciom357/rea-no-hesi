package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Invite is a Content Manager race invite: where the server is, plus any
// extra parameters (password…) CM put in the link, kept as they came.
type Invite struct {
	Host     string     `json:"host"`
	HTTPPort int        `json:"httpPort"`
	Query    url.Values `json:"-"`
}

var hostPortRe = regexp.MustCompile(`^([A-Za-z0-9.\-]+|\[[0-9a-fA-F:]+\]):(\d{2,5})$`)

// ParseInvite accepts what hosts paste: a CM invite (acmanager://… or an
// acstuff.club share link) with ip= and httpPort=, or a plain "ip:port".
func ParseInvite(text string) (Invite, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Invite{}, errors.New("cole o race invite do servidor")
	}
	if m := hostPortRe.FindStringSubmatch(text); m != nil {
		port, _ := strconv.Atoi(m[2])
		return Invite{Host: strings.Trim(m[1], "[]"), HTTPPort: port, Query: url.Values{}}, validPort(port)
	}
	i := strings.IndexByte(text, '?')
	if i < 0 {
		return Invite{}, errors.New("não reconheci o race invite — cole o link do Content Manager ou IP:porta HTTP")
	}
	q, err := url.ParseQuery(text[i+1:])
	if err != nil {
		return Invite{}, errors.New("o race invite está incompleto")
	}
	host := strings.TrimSpace(q.Get("ip"))
	port, _ := strconv.Atoi(q.Get("httpPort"))
	if host == "" || port == 0 {
		return Invite{}, errors.New("o race invite não tem ip e httpPort")
	}
	if net.ParseIP(host) == nil && !regexp.MustCompile(`^[A-Za-z0-9.\-]+$`).MatchString(host) {
		return Invite{}, errors.New("endereço do servidor inválido no race invite")
	}
	return Invite{Host: host, HTTPPort: port, Query: q}, validPort(port)
}

func validPort(p int) error {
	if p < 1 || p > 65535 {
		return errors.New("porta inválida")
	}
	return nil
}

// JoinLink rebuilds the invite as the acmanager:// link Content Manager
// opens. Only the query is reused, so a hub can never make the launcher open
// anything but Content Manager.
func (in Invite) JoinLink() string {
	q := url.Values{}
	for k, v := range in.Query {
		q[k] = v
	}
	q.Set("ip", in.Host)
	q.Set("httpPort", strconv.Itoa(in.HTTPPort))
	return "acmanager://race/online/join?" + q.Encode()
}

// ServerProbe is what a running server says about itself on /INFO.
type ServerProbe struct {
	Name       string   `json:"name"`
	TrackRaw   string   `json:"trackRaw"`
	Track      string   `json:"track"`
	Layout     string   `json:"layout"`
	Cars       []string `json:"cars"`
	Clients    int      `json:"clients"`
	MaxClients int      `json:"maxClients"`
	Password   bool     `json:"password"`
}

var acColorCodes = regexp.MustCompile(`\$\{[^}]*\}|ⓘ\d*`)

// ProbeServer reads track and cars from the server, so the host does not
// have to type them.
func ProbeServer(in Invite, knownTrack func(id, layout string) bool) (ServerProbe, error) {
	resp, err := liveClient.Get(fmt.Sprintf("http://%s/INFO", net.JoinHostPort(in.Host, strconv.Itoa(in.HTTPPort))))
	if err != nil {
		return ServerProbe{}, errors.New("não consegui falar com o servidor — ele está ligado e acessível deste PC?")
	}
	defer resp.Body.Close()
	var v struct {
		Name       string   `json:"name"`
		Track      string   `json:"track"`
		Cars       []string `json:"cars"`
		Clients    int      `json:"clients"`
		MaxClients int      `json:"maxclients"`
		Pass       bool     `json:"pass"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil {
		return ServerProbe{}, errors.New("o endereço respondeu, mas não parece ser um servidor de Assetto Corsa")
	}
	p := ServerProbe{Name: strings.TrimSpace(acColorCodes.ReplaceAllString(v.Name, "")), TrackRaw: v.Track,
		Clients: v.Clients, MaxClients: v.MaxClients, Password: v.Pass}
	p.Track, p.Layout = SplitTrack(v.Track, knownTrack)
	seen := map[string]bool{}
	for _, c := range v.Cars {
		if c != "" && !seen[c] && ValidID(c) {
			seen[c] = true
			p.Cars = append(p.Cars, c)
		}
	}
	return p, nil
}

// SplitTrack turns the /INFO track ("track-layout", or a CSP path such as
// "csp/2144/../H/../track-layout") into folder and layout. Track folders may
// contain dashes themselves, so a split is only trusted when that track and
// layout are known; otherwise the last dash is used.
func SplitTrack(raw string, known func(id, layout string) bool) (string, string) {
	if i := strings.LastIndex(raw, "/"); i >= 0 && strings.Contains(raw, "..") {
		raw = raw[i+1:]
	}
	raw = strings.TrimSpace(raw)
	if known != nil {
		if known(raw, "") {
			return raw, ""
		}
		for i := len(raw) - 1; i > 0; i-- {
			if raw[i] == '-' && known(raw[:i], raw[i+1:]) {
				return raw[:i], raw[i+1:]
			}
		}
	}
	if i := strings.LastIndex(raw, "-"); i > 0 {
		return raw[:i], raw[i+1:]
	}
	return raw, ""
}
