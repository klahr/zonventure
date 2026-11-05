package social

import "testing"

func TestClean(t *testing.T) {
	got, err := Clean(map[string]string{
		"instagram": " https://www.instagram.com/zonventure ",
		"youtube":   "https://youtu.be/abc",
		"facebook":  "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["instagram"] != "https://www.instagram.com/zonventure" || got["youtube"] != "https://youtu.be/abc" {
		t.Fatalf("got %v", got)
	}
	for _, in := range []map[string]string{
		{"instagram": "https://evil.example/instagram.com"},
		{"instagram": "https://notinstagram.com/x"},
		{"linkedin": "javascript:alert(1)"},
		{"tiktok": "www.tiktok.com/@zv"},
		{"myspace": "https://myspace.com/zv"},
	} {
		if _, err := Clean(in); err == nil {
			t.Errorf("%v: want an error", in)
		}
	}
}

func TestOrdered(t *testing.T) {
	l := Links{"tiktok": "https://www.tiktok.com/@zv", "instagram": "https://www.instagram.com/zv"}
	o := l.Ordered()
	if len(o) != 2 || o[0].Key != "instagram" || o[1].Key != "tiktok" {
		t.Fatalf("got %v", o)
	}
}
