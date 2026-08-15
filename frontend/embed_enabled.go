//go:build marvo_web

package webapp

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var assets embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return distHandler(dist)
}
