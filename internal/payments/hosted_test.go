package payments

import (
	"testing"

	"github.com/klahr/zonventure/internal/settings"
)

func TestHostedFrom(t *testing.T) {
	if p := HostedFrom(settings.DefaultPricing); p.BlockCents != 14900 || p.Currency != "sek" {
		t.Errorf("default = %+v", p)
	}
	if p := HostedFrom(settings.Pricing{}); !p.Free() {
		t.Errorf("0 kr = %+v, want free", p)
	}
	if p := HostedFrom(settings.Pricing{HostedBlockCents: 9900}); p.Price(3) != 29700 {
		t.Errorf("configured = %+v", p)
	}
}

func TestHostedPassFee(t *testing.T) {
	p := HostedPass{BlockCents: 14900}
	if got := p.Fee(120000, 1); got != 14900 {
		t.Errorf("fee on a 1200 kr party = %d", got)
	}
	if got := p.Fee(10000, 1); got != 9999 {
		t.Errorf("fee clamped below the charge = %d", got)
	}
}
