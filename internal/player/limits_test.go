package player

import "testing"

func TestAllowAnswerDevBurst(t *testing.T) {
	l := &playLimiter{}
	for i := 0; i < AnswerBurst; i++ {
		if ok, _ := l.AllowAnswer(1, false); !ok {
			t.Fatalf("player attempt %d refused", i+1)
		}
	}
	if ok, retry := l.AllowAnswer(1, false); ok || retry <= 0 {
		t.Fatalf("player attempt %d allowed (retry %v)", AnswerBurst+1, retry)
	}
	for i := 0; i < DevAnswerBurst; i++ {
		if ok, _ := l.AllowAnswer(2, true); !ok {
			t.Fatalf("dev attempt %d refused", i+1)
		}
	}
	if ok, _ := l.AllowAnswer(2, true); ok {
		t.Fatalf("dev attempt %d allowed", DevAnswerBurst+1)
	}
}
