package look

import "testing"

func TestBackdropValidate(t *testing.T) {
	for _, ok := range []Backdrop{
		{Kind: "dots", Opacity: 0.12, Scale: 24},
		{Kind: "spots", Opacity: 1, Scale: 80, Points: []Spot{{X: 0, Y: 0, Color: "#ff9f1c"}, {X: 100, Y: 100}}},
		{Kind: "confetti", Opacity: 0.6, Scale: 26, Colors: []string{"#ff3d7f", "#ffb703", "#2ec4b6"}},
		{Kind: "picture", Opacity: 0.8, Scale: 100, Media: 7, Blur: 6},
		{Kind: "horizon", Opacity: 1, Scale: 40, Color: "#2de2e6", Color2: "#ffd319", Colors: []string{"#1c0e31", "#59255b", "#d74789"}},
	} {
		if err := ok.Validate(); err != nil {
			t.Fatalf("valid backdrop %+v: %v", ok, err)
		}
	}
	bad := map[string]Backdrop{
		"unknown kind":     {Kind: "plaid", Opacity: 0.12, Scale: 24},
		"colour not hex":   {Kind: "dots", Color: "red", Opacity: 0.12, Scale: 24},
		"short hex":        {Kind: "dots", Color2: "#0a0", Opacity: 0.12, Scale: 24},
		"too faint":        {Kind: "dots", Opacity: 0.001, Scale: 24},
		"too strong":       {Kind: "dots", Opacity: 0.9, Scale: 24},
		"too small":        {Kind: "dots", Opacity: 0.12, Scale: 2},
		"too large":        {Kind: "dots", Opacity: 0.12, Scale: 500},
		"angle past 359":   {Kind: "stripes", Opacity: 0.12, Scale: 24, Angle: 360},
		"negative angle":   {Kind: "stripes", Opacity: 0.12, Scale: 24, Angle: -10},
		"css in a colour":  {Kind: "dots", Color: "#fff;}", Opacity: 0.12, Scale: 24},
		"spots, none":      {Kind: "spots", Opacity: 0.8, Scale: 80},
		"spots, too many":  {Kind: "spots", Opacity: 0.8, Scale: 80, Points: make([]Spot, MaxSpots+1)},
		"spot off page":    {Kind: "spots", Opacity: 0.8, Scale: 80, Points: []Spot{{X: 120, Y: 10}}},
		"spot colour":      {Kind: "spots", Opacity: 0.8, Scale: 80, Points: []Spot{{X: 10, Y: 10, Color: "pink"}}},
		"points on dots":   {Kind: "dots", Opacity: 0.12, Scale: 24, Points: []Spot{{X: 10, Y: 10}}},
		"dots at full":     {Kind: "dots", Opacity: 1, Scale: 24},
		"picture, none":    {Kind: "picture", Opacity: 1, Scale: 100},
		"image on dots":    {Kind: "dots", Opacity: 0.1, Scale: 24, Media: 3},
		"too blurry":       {Kind: "picture", Opacity: 1, Scale: 100, Media: 3, Blur: 40},
		"colours on dots":  {Kind: "dots", Opacity: 0.1, Scale: 24, Colors: []string{"#ff0000"}},
		"empty colour":     {Kind: "confetti", Opacity: 0.5, Scale: 24, Colors: []string{""}},
		"too many colours": {Kind: "stripes", Opacity: 0.1, Scale: 24, Colors: []string{"#000000", "#000000", "#000000", "#000000", "#000000", "#000000", "#000000", "#000000", "#000000"}},
	}
	for name, b := range bad {
		if b.Validate() == nil {
			t.Errorf("%s: accepted %+v", name, b)
		}
	}
}

func TestStyleValidate(t *testing.T) {
	for _, ok := range []Style{{}, {CardOpacity: 1}, {CardOpacity: MinCardOpacity, CardBlur: true}} {
		if err := ok.Validate(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	if err := (Style{Transition: "slide"}).Validate(); err != nil {
		t.Errorf("slide: %v", err)
	}
	full := Style{ButtonShape: "square", ButtonFill: "outline", ButtonSize: "large", Dock: "floating", TextScale: 1.2, TitleStyle: "ribbon", TitleUpper: true}
	if err := full.Validate(); err != nil {
		t.Errorf("%+v: %v", full, err)
	}
	for _, bad := range []Style{{CardOpacity: 0.1}, {CardOpacity: 1.5}, {CardOpacity: -1}, {Transition: "spin"},
		{ButtonShape: "blob"}, {ButtonFill: "glitter"}, {ButtonSize: "huge"}, {Dock: "top"}, {TitleStyle: "neon"}, {TextScale: 3}, {TextScale: 0.5}} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if (Look{Style: &Style{CardOpacity: 3}}).Validate() == nil {
		t.Error("a look accepted a bad style")
	}
}
