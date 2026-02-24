package web

import "embed"

//go:embed index.html dashboard.js style.css
var Content embed.FS
