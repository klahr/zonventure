// Package social holds an organisation's profiles on social networks
// (organizations.social_links): which networks there are, what a link
// to each may be, and the site's own organisation's, which every page's
// footer shows.
package social

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
)

// Network is one social network an organisation can link to.
type Network struct {
	Key   string
	Label string
	// Hosts are the network's domains; a link must be on one of them.
	Hosts []string
}

// Networks, in the order they show.
var Networks = []Network{
	{Key: "instagram", Label: "Instagram", Hosts: []string{"instagram.com"}},
	{Key: "facebook", Label: "Facebook", Hosts: []string{"facebook.com", "fb.com"}},
	{Key: "linkedin", Label: "LinkedIn", Hosts: []string{"linkedin.com"}},
	{Key: "youtube", Label: "YouTube", Hosts: []string{"youtube.com", "youtu.be"}},
	{Key: "tiktok", Label: "TikTok", Hosts: []string{"tiktok.com"}},
}

// Links maps a network's key to the organisation's profile there.
type Links map[string]string

// maxURL caps one link's length.
const maxURL = 500

// ErrInvalid is a link that isn't a profile on its network.
var ErrInvalid = errors.New("invalid social link")

// Clean checks links as an organisation sends them: each a full http
// or https URL on its network's domain. Empty ones are left out.
func Clean(in map[string]string) (Links, error) {
	out := Links{}
	for k, v := range in {
		v = strings.TrimSpace(v)
		n, ok := network(k)
		if !ok {
			return nil, errors.Join(ErrInvalid, errors.New("unknown network "+k))
		}
		if v == "" {
			continue
		}
		u, err := url.Parse(v)
		if len(v) > maxURL || err != nil || (u.Scheme != "http" && u.Scheme != "https") || !onHost(u.Hostname(), n.Hosts) {
			return nil, errors.Join(ErrInvalid, errors.New(n.Label+" link must be a full URL on "+n.Hosts[0]))
		}
		out[k] = v
	}
	return out, nil
}

func network(key string) (Network, bool) {
	for _, n := range Networks {
		if n.Key == key {
			return n, true
		}
	}
	return Network{}, false
}

func onHost(host string, hosts []string) bool {
	host = strings.ToLower(host)
	for _, h := range hosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// Link is one profile, with its network.
type Link struct {
	Network
	URL string
}

// Ordered returns the links in Networks' order.
func (l Links) Ordered() []Link {
	var out []Link
	for _, n := range Networks {
		if u := l[n.Key]; u != "" {
			out = append(out, Link{n, u})
		}
	}
	return out
}

var site atomic.Pointer[Links]

// Load reads the site's own organisation's links (ZV_HOME_ORG), which
// Site then returns. Call it again when they change.
func Load(ctx context.Context, db *sql.DB, homeOrg string) error {
	l := Links{}
	if homeOrg != "" {
		var raw []byte
		err := db.QueryRowContext(ctx, `SELECT social_links FROM organizations WHERE slug = $1`, homeOrg).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &l); err != nil {
				return err
			}
		}
	}
	site.Store(&l)
	return nil
}

// Site returns the site's own organisation's links, none before Load.
func Site() Links {
	if l := site.Load(); l != nil {
		return *l
	}
	return Links{}
}

// Key identifies the site's links, for caching pages that show them.
func Key() string {
	var b strings.Builder
	for _, o := range Site().Ordered() {
		b.WriteString(o.Key)
		b.WriteByte('=')
		b.WriteString(o.URL)
		b.WriteByte(0)
	}
	return b.String()
}
