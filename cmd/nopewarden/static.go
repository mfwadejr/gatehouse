package main

import (
	"net/http"

	"nopewarden/web"
)

// staticHandler serves the embedded console.
func staticHandler() http.Handler { return web.Handler() }
