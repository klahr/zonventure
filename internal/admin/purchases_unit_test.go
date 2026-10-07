package admin

import (
	"testing"

	"github.com/klahr/zonventure/internal/settings"
)

func TestPassAmounts(t *testing.T) {
	p := settings.Pricing{PlayerCents: 4900, FreePlayers: 15}
	for _, tc := range []struct{ players, want int }{{10, 0}, {15, 0}, {40, 25 * 4900}} {
		if got := purchaseAmountCents(p, tc.players); got != tc.want {
			t.Errorf("purchaseAmountCents(%d) = %d, want %d", tc.players, got, tc.want)
		}
	}
	if got := topUpAmountCents(p, 3); got != 3*4900 {
		t.Errorf("topUpAmountCents(3) = %d", got)
	}
}
