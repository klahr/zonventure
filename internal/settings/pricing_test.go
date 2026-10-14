package settings

import (
	"strings"
	"testing"
)

func TestLoadCurrency(t *testing.T) {
	defer func(c string) { Currency = c }(Currency)
	for _, tc := range []struct {
		env, want string
		ok        bool
	}{
		{"", "sek", true},
		{" EUR ", "eur", true},
		{"euro", "sek", false},
		{"e1r", "sek", false},
		{"JPY", "sek", false},
	} {
		Currency = "sek"
		t.Setenv("ZV_CURRENCY", tc.env)
		err := LoadCurrency()
		if (err == nil) != tc.ok || Currency != tc.want {
			t.Errorf("ZV_CURRENCY=%q: Currency %q, err %v", tc.env, Currency, err)
		}
	}
}

func TestLoadLocale(t *testing.T) {
	defer func(tz string, sc []string) { TimeZone, ShipCountries = tz, sc }(TimeZone, ShipCountries)
	t.Setenv("ZV_TIME_ZONE", "America/New_York")
	t.Setenv("ZV_SHIP_COUNTRIES", "us, ca")
	if err := LoadLocale(); err != nil || TimeZone != "America/New_York" || strings.Join(ShipCountries, ",") != "US,CA" {
		t.Fatalf("got %q %v, err %v", TimeZone, ShipCountries, err)
	}
	if got := OrgShipCountries(""); strings.Join(got, ",") != "US,CA" {
		t.Errorf("empty org list: %v", got)
	}
	if got := OrgTimeZone("Europe/Oslo"); got != "Europe/Oslo" {
		t.Errorf("org time zone: %q", got)
	}
	t.Setenv("ZV_TIME_ZONE", "Mars/Olympus")
	if LoadLocale() == nil {
		t.Error("unknown time zone accepted")
	}
	t.Setenv("ZV_TIME_ZONE", "")
	t.Setenv("ZV_SHIP_COUNTRIES", "SWE")
	if LoadLocale() == nil {
		t.Error("three-letter country accepted")
	}
}
