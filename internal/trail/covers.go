package trail

import "embed"

// The starter themes' cover pictures, for print: a JPEG per theme id
// (the web's own copies are in web/packages/play-ui/src/covers).
//
//go:embed covers/*.jpg
var covers embed.FS

// Cover is the JPEG cover picture of the starter theme id, or nil.
func Cover(id string) []byte {
	if id == "" {
		return nil
	}
	b, err := covers.ReadFile("covers/" + id + ".jpg")
	if err != nil {
		return nil
	}
	return b
}
