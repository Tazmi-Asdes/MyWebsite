package public

import (
	"embed"
	"io/fs"
)

// Files contains the server-rendered public templates and their static assets.
//
//go:embed templates/*.html assets/*.css assets/*.svg
var Files embed.FS

// Assets is the subset of Files exposed by the /assets/ route.
var Assets fs.FS

func init() {
	var err error
	Assets, err = fs.Sub(Files, "assets")
	if err != nil {
		panic(err)
	}
}
