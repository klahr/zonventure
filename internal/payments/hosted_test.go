package payments

import "testing"

func TestHostedPassFromEnv(t *testing.T) {
	t.Setenv("ZV_HOSTED_BLOCK_CENTS", "")
	t.Setenv("ZV_HOSTED_CURRENCY", "")
	if p := HostedPassFromEnv(true); p.BlockCents != DefaultHostedBlockCents || p.Currency != "sek" {
		t.Errorf("hosted instance default = %+v", p)
	}
	if p := HostedPassFromEnv(false); !p.Free() {
		t.Errorf("self-host default = %+v, want free", p)
	}
	t.Setenv("ZV_HOSTED_BLOCK_CENTS", "9900")
	t.Setenv("ZV_HOSTED_CURRENCY", "EUR")
	if p := HostedPassFromEnv(false); p.BlockCents != 9900 || p.Currency != "eur" || p.Price(3) != 29700 {
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
