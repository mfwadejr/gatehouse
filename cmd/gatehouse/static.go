package main

import (
	"net/http"

	"gatehouse/web"
)

// staticHandler serves the embedded console.
func staticHandler() http.Handler { return web.Handler() }
