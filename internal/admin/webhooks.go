package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/webhooks"

	"github.com/go-chi/chi/v5"
)

/* ---------------- Webhook endpoints ----------------
 *
 * Org-scoped CRUD for outbound webhook subscriptions. The secret is
 * returned only on create — listing returns its first 8 chars as a
 * fingerprint so customers can identify which one a verifier was
 * configured against without exposing the bytes.
 *
 * Admins acting on behalf of an org are blocked here intentionally —
 * webhook delivery URLs are sensitive customer config and should be
 * managed by the org itself, not by support typing on their behalf.
 */

type webhookEndpointDTO struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	EventKinds  []string `json:"event_kinds"`
	Active      bool     `json:"active"`
	SecretHint  string   `json:"secret_hint"`
	CreatedAt   string   `json:"created_at"`
}

type createWebhookRequest struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	EventKinds []string `json:"event_kinds"`
}

type patchWebhookRequest struct {
	Name       *string   `json:"name,omitempty"`
	URL        *string   `json:"url,omitempty"`
	EventKinds *[]string `json:"event_kinds,omitempty"`
	Active     *bool     `json:"active,omitempty"`
}

// validateWebhookURL rejects anything that isn't a fully-qualified
// validateWebhookURL gates customer-supplied webhook destinations.
// Two layers:
//
//  1. Scheme: https in production, http only for loopback names so dev
//     clones can target a local listener.
//  2. Address: the resolved IP must not be private, loopback, link-
//     local, multicast, or one of the cloud metadata services. The
//     gate runs at registration time AND every delivery (see
//     internal/webhooks/delivery.go) — registration alone is not
//     enough because DNS can resolve to different IPs over time
//     (DNS rebinding, pointer-record flip after a cron). Only
//     loopback hosts ("localhost", "127.x.x.x", "::1") are
//     intentionally exempt: they're how dev hits a local sink.
//
// Without this, a customer can register https://169.254.169.254/ and
// the worker happily POSTs at the AWS metadata endpoint with their
// signed payload — a textbook SSRF pivot from a customer-supplied
// string into the cloud-host's secret store.
func validateWebhookURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid url")
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("url must have a host")
	}
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasPrefix(host, "127.")
	switch u.Scheme {
	case "https":
		// fine
	case "http":
		if !loopback {
			return "", errors.New("http urls are only allowed for localhost — use https in production")
		}
	default:
		return "", errors.New("url must be http(s)")
	}
	// Skip the IP-address gate for loopback dev URLs. For everything
	// else, resolve and reject if any answer is non-public. The
	// resolver lives in internal/webhooks/ so the delivery worker
	// can reuse it (defending against DNS rebinding between
	// registration and dispatch).
	if !loopback {
		if err := webhooks.ValidateHost(host); err != nil {
			return "", err
		}
	}
	return u.String(), nil
}

// validateEventKinds rejects unknown / typoed kinds early — better to
// 400 the operator than enqueue events that will never match anything.
func validateEventKinds(kinds []string) ([]string, error) {
	if len(kinds) == 0 {
		return nil, errors.New("event_kinds must not be empty")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		k = strings.TrimSpace(k)
		if !webhooks.KnownKind(k) {
			return nil, errors.New("unknown event kind: " + k)
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out, nil
}

// orgFromCaller is a small guard that returns the caller's org id and
// 403s admins (who don't own webhooks). Used by every handler in this
// file.
func orgFromCaller(w http.ResponseWriter, r *http.Request) (int64, bool) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "webhooks are configured per organization")
		return 0, false
	}
	return *me.OrgID, true
}

// ListWebhooks returns the caller's org's endpoints, secrets redacted
// to a hint. Sorted newest-first to match the rest of the admin UI.
func (h *Handlers) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgFromCaller(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, name, url, event_kinds, active, secret, created_at
		  FROM webhook_endpoints
		 WHERE org_id = $1
		 ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []webhookEndpointDTO{}
	for rows.Next() {
		var (
			d        webhookEndpointDTO
			kindsRaw []byte
			secret   string
		)
		if err := rows.Scan(&d.ID, &d.Name, &d.URL, &kindsRaw, &d.Active, &secret, &d.CreatedAt); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if err := json.Unmarshal(kindsRaw, &d.EventKinds); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "decode kinds: "+err.Error())
			return
		}
		// Hint = first 8 chars of secret. Enough to disambiguate which
		// secret a customer configured their verifier with; not enough
		// to forge a signature.
		if len(secret) >= 8 {
			d.SecretHint = secret[:8]
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"endpoints": out})
}

// CreateWebhook mints an endpoint + a fresh HMAC secret. The secret
// is returned ONCE, in this response, with the customer expected to
// store it on their verifier side. Subsequent reads expose only the
// hint — losing the secret means rotating the endpoint.
func (h *Handlers) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgFromCaller(w, r)
	if !ok {
		return
	}
	var req createWebhookRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	cleanURL, err := validateWebhookURL(req.URL)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	cleanKinds, err := validateEventKinds(req.EventKinds)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	secret, err := webhooks.NewSecret()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "secret gen failed")
		return
	}
	kindsJSON, err := json.Marshal(cleanKinds)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "encode kinds failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO webhook_endpoints(org_id, url, secret, event_kinds, name)
		VALUES ($1, $2, $3, $4::jsonb, $5)
		RETURNING id
	`, orgID, cleanURL, secret, string(kindsJSON), strings.TrimSpace(req.Name)).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id":          id,
		"name":        strings.TrimSpace(req.Name),
		"url":         cleanURL,
		"event_kinds": cleanKinds,
		"active":      true,
		// Returned exactly once. After this, only the hint is visible.
		"secret": secret,
	})
}

// UpdateWebhook patches mutable fields. Each field is optional;
// callers send only the ones they want to change.
func (h *Handlers) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgFromCaller(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "endpointID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad endpoint id")
		return
	}
	var req patchWebhookRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	// Confirm the endpoint exists in the caller's org before any
	// UPDATE — prevents an org from learning about another org's id
	// space via differential error messages.
	var owningOrg int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT org_id FROM webhook_endpoints WHERE id = $1`, id).Scan(&owningOrg); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "endpoint not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if owningOrg != orgID {
		httpx.Error(w, http.StatusNotFound, "endpoint not found")
		return
	}

	if req.URL != nil {
		clean, err := validateWebhookURL(*req.URL)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE webhook_endpoints SET url = $1 WHERE id = $2`, clean, id); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update url failed")
			return
		}
	}
	if req.EventKinds != nil {
		clean, err := validateEventKinds(*req.EventKinds)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		kindsJSON, err := json.Marshal(clean)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "encode kinds failed")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE webhook_endpoints SET event_kinds = $1::jsonb WHERE id = $2`, string(kindsJSON), id); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update kinds failed")
			return
		}
	}
	if req.Name != nil {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE webhook_endpoints SET name = $1 WHERE id = $2`, strings.TrimSpace(*req.Name), id); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update name failed")
			return
		}
	}
	if req.Active != nil {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE webhook_endpoints SET active = $1 WHERE id = $2`, *req.Active, id); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update active failed")
			return
		}
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteWebhook removes the endpoint. Cascade on webhook_deliveries
// (set in the migration) wipes any pending/delivered/dead history.
func (h *Handlers) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgFromCaller(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "endpointID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad endpoint id")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM webhook_endpoints WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "endpoint not found")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ListWebhookEventKinds returns the v1 catalog so the UI / API
// consumer can render checkboxes without hardcoding the list.
func (h *Handlers) ListWebhookEventKinds(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"event_kinds": webhooks.AllKinds})
}

// webhookDeliveryDTO is the wire shape for a row in the deliveries
// log. Payload is the verbatim JSON we POSTed (the customer-side
// verifier hashes it; clients pretty-print it for inspection).
// LastError is omitted when empty so a fresh delivery doesn't carry
// a noisy `"last_error": ""` field.
type webhookDeliveryDTO struct {
	ID            int64   `json:"id"`
	EndpointID    int64   `json:"endpoint_id"`
	EndpointName  string  `json:"endpoint_name"`
	DeliveryUUID  string  `json:"delivery_uuid"`
	EventKind     string  `json:"event_kind"`
	Status        string  `json:"status"`
	Attempts      int     `json:"attempts"`
	LastStatus    *int    `json:"last_status,omitempty"`
	LastError     string  `json:"last_error,omitempty"`
	NextAttemptAt string  `json:"next_attempt_at"`
	CreatedAt     string  `json:"created_at"`
	DeliveredAt   string  `json:"delivered_at,omitempty"`
	Payload       string  `json:"payload"`
}

// ListWebhookDeliveries returns the org's recent webhook delivery
// attempts, newest first. Filters: endpoint_id (limit to one
// endpoint), status (pending|delivered|dead). Cursor pagination via
// `before=<id>` — the client passes the smallest id from the
// previous page. Page size capped at 100 to keep payloads reasonable.
//
// Org scoping happens via the JOIN to webhook_endpoints, so a caller
// can never see another org's deliveries even if they guess a
// delivery id.
func (h *Handlers) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgFromCaller(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	args := []any{orgID}
	where := []string{"e.org_id = $1"}
	if v := q.Get("endpoint_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			args = append(args, n)
			where = append(where, "d.endpoint_id = $"+strconv.Itoa(len(args)))
		}
	}
	if v := strings.TrimSpace(q.Get("status")); v != "" {
		switch v {
		case "pending", "delivered", "dead":
			args = append(args, v)
			where = append(where, "d.status = $"+strconv.Itoa(len(args)))
		}
	}
	if v := q.Get("before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			args = append(args, n)
			where = append(where, "d.id < $"+strconv.Itoa(len(args)))
		}
	}
	// limit is appended last as its own placeholder so the query
	// plan caches well across paginations of the same query.
	args = append(args, limit)
	whereClause := strings.Join(where, " AND ")
	query := `
		SELECT d.id, d.endpoint_id, COALESCE(e.name, ''), d.delivery_uuid,
		       d.event_kind, d.status, d.attempts,
		       d.last_status, COALESCE(d.last_error, ''),
		       d.next_attempt_at, d.created_at, COALESCE(d.delivered_at, ''),
		       d.payload
		  FROM webhook_deliveries d
		  JOIN webhook_endpoints e ON e.id = d.endpoint_id
		 WHERE ` + whereClause + `
		 ORDER BY d.id DESC
		 LIMIT $` + strconv.Itoa(len(args))
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}
	defer rows.Close()
	out := []webhookDeliveryDTO{}
	for rows.Next() {
		var (
			d          webhookDeliveryDTO
			lastStatus sql.NullInt64
		)
		if err := rows.Scan(&d.ID, &d.EndpointID, &d.EndpointName, &d.DeliveryUUID,
			&d.EventKind, &d.Status, &d.Attempts,
			&lastStatus, &d.LastError,
			&d.NextAttemptAt, &d.CreatedAt, &d.DeliveredAt,
			&d.Payload); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if lastStatus.Valid {
			v := int(lastStatus.Int64)
			d.LastStatus = &v
		}
		out = append(out, d)
	}
	// next_before lets the client request the page after this one
	// without parsing ids itself: it's just the smallest id in this
	// response. Empty when the page was short (= caller saw all).
	var nextBefore int64
	if len(out) == limit {
		nextBefore = out[len(out)-1].ID
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"deliveries":  out,
		"next_before": nextBefore,
	})
}

// SendTestWebhook enqueues one webhook.test delivery against the
// endpoint, bypassing the subscription filter. The customer sees the
// payload arrive at their URL the next time the worker drains
// (typically within a few seconds). Useful during integration —
// "did my verifier validate?" — without waiting for a real
// lifecycle event to fire.
func (h *Handlers) SendTestWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgFromCaller(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "endpointID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad endpoint id")
		return
	}
	// Single statement guards ownership: only emit if the row exists
	// in the caller's org. The org check happens here, separately
	// from the SendTest call, because SendTest takes only an
	// endpoint id and shouldn't grow auth concerns.
	var owningOrg int64
	var active bool
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT org_id, active FROM webhook_endpoints WHERE id = $1`, id).Scan(&owningOrg, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "endpoint not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if owningOrg != orgID {
		httpx.Error(w, http.StatusNotFound, "endpoint not found")
		return
	}
	if !active {
		httpx.Error(w, http.StatusConflict, "endpoint is disabled — re-enable to receive deliveries")
		return
	}
	if err := webhooks.SendTest(r.Context(), h.DB, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "enqueue test failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"queued": true,
		"note":   "Test delivery enqueued. The worker drains every 5s.",
	})
}

// RotateWebhookSecret mints a fresh HMAC secret for an existing
// endpoint and returns it once in the response. The old secret is
// immediately invalid — there is no overlap window. Customers should
// deploy the new secret to their verifier *first* and only then call
// this; expect a brief window of signature mismatches otherwise.
//
// The narrow no-overlap design is deliberate for v1: a dual-secret
// scheme would need extra columns + an expiry sweep, and the cost of
// "deploy first, then rotate" is small for the operational simplicity
// it buys.
func (h *Handlers) RotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgFromCaller(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "endpointID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad endpoint id")
		return
	}
	secret, err := webhooks.NewSecret()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "secret gen failed")
		return
	}
	// Single statement: confirm ownership and rotate atomically.
	// RowsAffected==0 covers both "not found" and "wrong org" with
	// one error message so the id space isn't probable across orgs.
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE webhook_endpoints SET secret = $1 WHERE id = $2 AND org_id = $3`,
		secret, id, orgID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "rotate failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "endpoint not found")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"secret": secret,
	})
}
