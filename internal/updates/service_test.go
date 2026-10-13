package updates

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Error("no request expected while update checks are off")
	return nil, errors.New("unexpected request")
}

func TestService_OffFetchesNothing(t *testing.T) {
	s := NewService(Config{Version: "0.1.0", ChangelogURL: Off, HTTPClient: &http.Client{Transport: failTransport{t}}})
	st, err := s.CheckNow(context.Background())
	if err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if !st.Disabled || st.FetchError != "" {
		t.Fatalf("status = %+v, want disabled with no fetch error", st)
	}
}
