package admin

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
)

// writerRoutes is everything an org_writer may reach under /api/admin
// besides their own account (/me…): authoring the organisation's
// adventures — the designer, the party creator, collaboration, test
// play, media, themes — and publishing them. Events, teams, members,
// billing, the store and the organisation's settings are not here,
// and neither is any route added later until it is listed.
var writerRoutes = map[string]bool{
	"/api/admin/users/{userID}/avatar": true,
	"/api/admin/oauth/request":         true,
	"/api/admin/oauth/approve":         true,

	"/api/admin/adventures":                                         true,
	"/api/admin/adventures/import":                                  true,
	"/api/admin/adventures/{slug}":                                  true,
	"/api/admin/markdown/preview":                                   true,
	"/api/admin/adventures/{slug}/proposal":                         true,
	"/api/admin/adventures/{slug}/proposal/submit":                  true,
	"/api/admin/adventures/{slug}/proposal/image":                   true,
	"/api/admin/trail-templates":                                    true,
	"/api/admin/trail-keep":                                         true,
	"/api/admin/trails":                                             true,
	"/api/admin/adventures/{slug}/cards":                            true,
	"/api/admin/adventures/{slug}/cards/publish":                    true,
	"/api/admin/adventures/{slug}/cards/designer":                   true,
	"/api/admin/adventures/{slug}/cards/theme":                      true,
	"/api/admin/adventures/{slug}/cards.pdf":                        true,
	"/api/admin/adventures/{slug}/edit":                             true,
	"/api/admin/adventures/{slug}/graph":                            true,
	"/api/admin/adventures/{slug}/graph/publish":                    true,
	"/api/admin/adventures/{slug}/graph/versions":                   true,
	"/api/admin/adventures/{slug}/graph/versions/{version}":         true,
	"/api/admin/adventures/{slug}/graph/versions/{version}/publish": true,
	"/api/admin/adventures/{slug}/graph/validate":                   true,
	"/api/admin/adventures/{slug}/graph/qr":                         true,
	"/api/admin/adventures/{slug}/collab":                           true,
	"/api/admin/adventures/{slug}/threads":                          true,
	"/api/admin/adventures/{slug}/threads/{threadID}/comments":      true,
	"/api/admin/adventures/{slug}/threads/{threadID}":               true,
	"/api/admin/adventures/{slug}/comments/{commentID}":             true,
	"/api/admin/adventures/{slug}/steps/{node}/status":              true,
	"/api/admin/adventures/{slug}/reactions":                        true,
	"/api/admin/adventures/{slug}/seen":                             true,
	"/api/admin/adventures/{slug}/simulate":                         true,
	"/api/admin/adventures/{slug}/dev-ticket":                       true,
	"/api/admin/adventures/{slug}/dev-ticket/send":                  true,
	"/api/admin/adventures/{slug}/dev-screen":                       true,
	"/api/admin/adventures/{slug}/dev-goto":                         true,
	"/api/admin/adventures/{slug}/dev-reset":                        true,
	"/api/admin/adventures/{slug}/simulate-event":                   true,
	"/api/admin/adventures/{slug}/media":                            true,
	"/api/admin/adventures/{slug}/avatar":                           true,
	"/api/admin/adventures/{slug}/duplicate":                        true,
	"/api/admin/adventures/{slug}/export":                           true,
	"/api/admin/org-themes":                                         true,
	"/api/admin/org-themes/{id}":                                    true,
}

// writerMay reports whether an org_writer may call the route r is for.
// Their own account is theirs except the billing portal; the listing
// review queue checks its own per-user flag.
func writerMay(r *http.Request) bool {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil || rctx.Routes == nil {
		return false
	}
	p := rctx.Routes.Find(chi.NewRouteContext(), r.Method, r.URL.Path)
	switch {
	case p == "/api/admin/me/portal-session":
		return false
	case p == "/api/admin/me", strings.HasPrefix(p, "/api/admin/me/"), strings.HasPrefix(p, "/api/admin/listing-reviews"):
		return true
	}
	return writerRoutes[p]
}

// RestrictWriter refuses an org_writer every /api/admin route but
// writerMay's, whether they come with a session or an API token.
func RestrictWriter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if me, ok := auth.FromContext(r.Context()); ok && me.Role == auth.RoleOrgWriter && !writerMay(r) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}
