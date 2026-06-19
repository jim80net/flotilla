package dash

import (
	"embed"
	"html/template"
	"io/fs"
)

// assetsFS embeds the dash's static web assets so the `flotilla` binary is
// self-contained (no asset path to configure) — consistent with one-binary
// flotilla. index.html is the page TEMPLATE (rendered server-side as static
// chrome); dash.css and dash.js are served verbatim under /static/.
//
//go:embed assets
var assetsFS embed.FS

// parseTemplates parses the embedded page template. The page carries NO dynamic
// fleet data — all live data reaches it via fetch of the JSON endpoints — so the
// template is static chrome; html/template's contextual escaping covers the few
// static fields (bind, xo) regardless.
func parseTemplates() (*template.Template, error) {
	return template.ParseFS(assetsFS, "assets/index.html")
}

// staticAssets returns the sub-filesystem rooted at assets/, so /static/dash.css
// resolves to assets/dash.css.
func staticAssets() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		// Unreachable: the embed directive guarantees assets/ exists at build time.
		panic("dash: embedded assets/ missing: " + err.Error())
	}
	return sub
}
