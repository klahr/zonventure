package admin

import (
	"testing"
	"time"
)

func TestCheckPartnerCode(t *testing.T) {
	t.Parallel()
	label := func(s string) map[string]string { return map[string]string{"en": s} }
	trueStory := []partnerField{{Label: label("Experience code"), Min: 9, Max: 9}}
	liveit := []partnerField{{Label: label("Box number"), Min: 1, Max: 20, Digits: true}, {Label: label("Security code"), Min: 1, Max: 32}}
	cases := []struct {
		name          string
		fields        []partnerField
		box, code     string
		wantBox, want string
		ok            bool
	}{
		{"truestory", trueStory, "", "abc-def-123", "", "ABCDEF123", true},
		{"truestory", trueStory, "", " ab cd ef 12 3 ", "", "ABCDEF123", true},
		{"truestory", trueStory, "", "ABCDEF12", "", "", false},    // 8 characters
		{"truestory", trueStory, "", "ABCDEF1234", "", "", false},  // 10 characters
		{"truestory", trueStory, "12", "ABCDEF123", "", "", false}, // one field: no box number
		{"truestory", trueStory, "", "ABCDÉF123", "", "", false},
		{"liveit", liveit, "123 456", "x9y8", "123456", "X9Y8", true},
		{"liveit", liveit, "", "X9Y8", "", "", false},     // box number required
		{"liveit", liveit, "12a", "X9Y8", "", "", false},  // box number is numeric
		{"liveit", liveit, "123", "", "", "", false},      // security code required
		{"liveit", liveit, "123", "X9;Y8", "", "", false}, // letters and digits only
		{"none", nil, "", "ABCDEF123", "", "", false},
	}
	for _, c := range cases {
		box, code, ok := checkPartnerCode(c.fields, c.box, c.code)
		if ok != c.ok || box != c.wantBox || code != c.want {
			t.Errorf("checkPartnerCode(%s, %q, %q) = %q, %q, %v; want %q, %q, %v",
				c.name, c.box, c.code, box, code, ok, c.wantBox, c.want, c.ok)
		}
	}
}

func TestPartnerSetupClean(t *testing.T) {
	t.Parallel()
	ok := func() partnerSetup {
		return partnerSetup{Name: " Smartbox ", Fields: []partnerField{{Label: map[string]string{"en": "Voucher", "sv": " "}, Min: 8, Max: 12}}}
	}
	s := ok()
	if err := s.clean(); err != nil || s.Name != "Smartbox" || len(s.Fields[0].Label) != 1 {
		t.Fatalf("clean: %v, %+v", err, s)
	}
	for name, bad := range map[string]func(*partnerSetup){
		"no name":      func(s *partnerSetup) { s.Name = " " },
		"no fields":    func(s *partnerSetup) { s.Fields = nil },
		"three fields": func(s *partnerSetup) { s.Fields = append(s.Fields, s.Fields[0], s.Fields[0]) },
		"no en label":  func(s *partnerSetup) { s.Fields[0].Label = map[string]string{"sv": "Kod"} },
		"min over max": func(s *partnerSetup) { s.Fields[0].Min = 13 },
		"too long":     func(s *partnerSetup) { s.Fields[0].Max = maxPartnerCode + 1 },
		"unknown lang": func(s *partnerSetup) { s.Hint = map[string]string{"xx": "?"} },
	} {
		s := ok()
		bad(&s)
		if s.clean() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRedeemLimiter(t *testing.T) {
	t.Parallel()
	l := redeemLimiter{perIP: map[string][]time.Time{}}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < redeemPerIPMax; i++ {
		if ok, _ := l.allow(now.Add(time.Duration(i)*time.Minute), "1.2.3.4"); !ok {
			t.Fatalf("attempt %d refused, want allowed", i+1)
		}
	}
	ok, wait := l.allow(now.Add(20*time.Minute), "1.2.3.4")
	if ok {
		t.Fatal("attempt past the cap allowed")
	}
	if wait != 40*time.Minute {
		t.Errorf("wait = %s, want 40m (until the first attempt leaves the window)", wait)
	}
	if ok, _ := l.allow(now.Add(20*time.Minute), "5.6.7.8"); !ok {
		t.Error("another IP refused")
	}
	if ok, _ := l.allow(now.Add(61*time.Minute), "1.2.3.4"); !ok {
		t.Error("attempt after the window refused")
	}
}
