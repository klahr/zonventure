package webhooks

import (
	"testing"
	"time"
)

func TestSignKnownVector(t *testing.T) {
	// Stable test vector — guards against accidental algorithm
	// changes (eg. someone swapping sha256 for sha1) that would
	// silently invalidate every deployed customer's verification.
	// A fixed expected string also pins determinism and the
	// "sha256=" prefix.
	t.Run("vector", func(t *testing.T) {
		got := sign("topsecret", []byte("hello"))
		want := "sha256=ed76fd36523b8becda5a3b36d0e3737e8ae5111f55e26c7c3a455a3ce29636d2"
		if got != want {
			t.Errorf("vector mismatch:\ngot  %s\nwant %s", got, want)
		}
	})
	// One vector can't tell a real HMAC from something that ignores
	// an input, so pin both inputs as load-bearing.
	t.Run("changes with body", func(t *testing.T) {
		if sign("k", []byte("a")) == sign("k", []byte("b")) {
			t.Fatalf("different bodies must produce different signatures")
		}
	})
	t.Run("changes with secret", func(t *testing.T) {
		if sign("k1", []byte("body")) == sign("k2", []byte("body")) {
			t.Fatalf("different secrets must produce different signatures")
		}
	})
}

func TestBackoffDelaySchedule(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},
		{1, time.Minute},
		{2, 5 * time.Minute},
		{3, 30 * time.Minute},
		{4, 2 * time.Hour},
		{5, 8 * time.Hour},
		{6, 24 * time.Hour},
		// Past the schedule we clamp to the last entry — defensive
		// for manual replays of dead deliveries.
		{99, 24 * time.Hour},
	}
	for _, c := range cases {
		got := backoffDelay(c.attempt)
		if got != c.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestBackoffScheduleCoversMaxAttempts(t *testing.T) {
	if len(backoffSchedule) < MaxAttempts {
		t.Fatalf("backoffSchedule has %d entries, MaxAttempts is %d — "+
			"a real attempt would index past the schedule",
			len(backoffSchedule), MaxAttempts)
	}
}

func TestKnownKindAcceptsPublishedKinds(t *testing.T) {
	for _, k := range AllKinds {
		if !KnownKind(k) {
			t.Errorf("AllKinds entry %q failed KnownKind", k)
		}
	}
}

func TestKnownKindRejectsUnknown(t *testing.T) {
	for _, k := range []string{"", "run.scheduled.typo", "RUN.SCHEDULED", "answer.right"} {
		if KnownKind(k) {
			t.Errorf("KnownKind(%q) = true, want false", k)
		}
	}
}

func TestNewSecretIsHexAndLong(t *testing.T) {
	s, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 64 {
		t.Errorf("secret length %d, want 64 hex chars", len(s))
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex char %q in secret", c)
			break
		}
	}
}

func TestNewSecretIsRandom(t *testing.T) {
	a, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("newSecret returned the same value twice — RNG broken?")
	}
}

func TestNewDeliveryUUIDIsRandom(t *testing.T) {
	a, err := newDeliveryUUID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newDeliveryUUID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("newDeliveryUUID returned the same value twice")
	}
	if len(a) != 32 {
		t.Errorf("delivery uuid length %d, want 32", len(a))
	}
}
