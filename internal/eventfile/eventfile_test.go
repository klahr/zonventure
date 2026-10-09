package eventfile

import "testing"

func TestTypeOf(t *testing.T) {
	zip := []byte("PK\x03\x04\x14\x00\x00\x00")
	for _, c := range []struct {
		name    string
		content []byte
		want    string
	}{
		{"rules.pdf", []byte("%PDF-1.7\n"), "application/pdf"},
		{"list.txt", []byte("Bring water.\n"), "text/plain; charset=utf-8"},
		{"map.png", []byte("\x89PNG\r\n\x1a\n"), "image/png"},
		{"rules.docx", zip, officeTypes[".docx"]},
		{"rules.DOCX", zip, officeTypes[".docx"]},
		{"bundle.zip", zip, ""},
		{"page.html", []byte("<!DOCTYPE html><html>"), ""},
	} {
		got, err := TypeOf(c.name, c.content)
		if got != c.want || (c.want == "") != (err != nil) {
			t.Errorf("TypeOf(%q) = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestCleanName(t *testing.T) {
	long := ""
	for len(long) < 300 {
		long += "å"
	}
	for _, c := range []struct{ in, want string }{
		{"Regler.pdf", "Regler.pdf"},
		{"../../etc/passwd", "passwd"},
		{`C:\Users\me\karta.pdf`, "karta.pdf"},
		{"bad\x00\"name\n.pdf", "badname.pdf"},
		{"  ", "file"},
		{"..", "file"},
		{long + ".pdf", long[:maxName-4] + ".pdf"},
	} {
		if got := CleanName(c.in); got != c.want {
			t.Errorf("CleanName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestContentDisposition(t *testing.T) {
	got := ContentDisposition("Karta över parken.pdf")
	want := `attachment; filename="Karta _ver parken.pdf"; filename*=UTF-8''Karta%20%C3%B6ver%20parken.pdf`
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}
