package settings

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// TimeZone and ShipCountries are what an organisation that has set
// neither writes times in and posts letters to. LoadLocale sets them
// from ZV_TIME_ZONE and ZV_SHIP_COUNTRIES at startup.
var (
	TimeZone      = "Europe/Stockholm"
	ShipCountries = []string{"SE"}
)

var countryRe = regexp.MustCompile(`^[A-Z]{2}$`)

// LoadLocale sets TimeZone and ShipCountries from the environment;
// unset keeps the defaults.
func LoadLocale() error {
	if v := strings.TrimSpace(os.Getenv("ZV_TIME_ZONE")); v != "" {
		if _, err := time.LoadLocation(v); err != nil {
			return fmt.Errorf("ZV_TIME_ZONE: %w", err)
		}
		TimeZone = v
	}
	if v := strings.TrimSpace(os.Getenv("ZV_SHIP_COUNTRIES")); v != "" {
		var list []string
		for _, c := range strings.Split(strings.ToUpper(v), ",") {
			if c = strings.TrimSpace(c); !countryRe.MatchString(c) {
				return fmt.Errorf("ZV_SHIP_COUNTRIES: %q is not an ISO 3166-1 alpha-2 code", c)
			}
			list = append(list, c)
		}
		ShipCountries = list
	}
	return nil
}

// OrgShipCountries is an organisation's ship_countries as
// array_to_string writes them, or ShipCountries when it has set none.
func OrgShipCountries(list string) []string {
	if list == "" {
		return ShipCountries
	}
	return strings.Split(list, ",")
}

// OrgTimeZone is an organisation's time_zone, or TimeZone when it has
// set none.
func OrgTimeZone(tz string) string {
	if tz == "" {
		return TimeZone
	}
	return tz
}
