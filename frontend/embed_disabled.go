//go:build !marvo_web

package webapp

import "net/http"

func Handler() http.Handler {
	return nil
}
