package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A user on the default password may change it and pick a language,
// nothing else.
func TestRequireNonDefaultPassword(t *testing.T) {
	h := RequireNonDefaultPassword(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/admin/me/password", http.StatusNoContent},
		{http.MethodPut, "/api/admin/me/language", http.StatusNoContent},
		{http.MethodGet, "/api/admin/events", http.StatusForbidden},
		{http.MethodPost, "/api/admin/me/language", http.StatusForbidden},
	} {
		r := httptest.NewRequest(c.method, c.path, nil)
		r = r.WithContext(WithUser(r.Context(), User{ID: 1, PasswordIsDefault: true}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, w.Code, c.want)
		}
	}
}

// An ended session says why: a party team is told whether the host took
// it out or finished its game.
func TestRequireSlot_EndedCodes(t *testing.T) {
	h := RequireSlot(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, c := range []struct {
		slot Slot
		want string
	}{
		{Slot{Ended: true}, `"event_ended"`},
		{Slot{Ended: true, Cancelled: true}, `"event_cancelled"`},
		{Slot{Ended: true, Party: true, Cancelled: true, Withdrawn: true}, `"event_cancelled"`},
		{Slot{Ended: true, Party: true, Withdrawn: true}, `"taken_out"`},
		{Slot{Ended: true, Party: true}, `"game_finished"`},
		{Slot{Ended: true, Withdrawn: true}, `"event_ended"`},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/player/play", nil)
		r = r.WithContext(context.WithValue(r.Context(), endedSlotCtxKey{}, c.slot))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%+v: %d %s, want %s", c.slot, w.Code, w.Body, c.want)
		}
	}
}
