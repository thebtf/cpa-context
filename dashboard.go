package main

import (
	_ "embed"
	"strings"
)

// The dashboard is a port of the dsh-context client, built by Vite into one
// script and one stylesheet. Both are embedded so the plugin stays a single
// shared library with no files to install beside it.
//
//go:embed web/dist/dashboard.js
var dashboardJS string

//go:embed web/dist/dashboard.css
var dashboardCSS string

// dashboardHTML returns the page that mounts the built client. The management
// key field and the refresh button live at the bottom of the sidebar, because
// the top-right corner of a browser is covered by extension and profile
// controls.
func dashboardHTML() string {
	var page strings.Builder
	page.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>CPA context</title>
<style>
html, body { height: 100%; margin: 0; background: #0d1017; }
#shell { display: flex; flex-direction: column; height: 100%; }
#bar { display: flex; gap: 8px; align-items: center; padding: 6px 12px;
  border-bottom: 1px solid #22262f; background: #11141b;
  font: 12px/1.4 ui-sans-serif, system-ui, sans-serif; color: #8b93a7; }
#bar .grow { flex: 1; }
#bar input { background: #10131a; border: 1px solid #2a3242; border-radius: 5px;
  padding: 4px 8px; color: #e6e9ef; font-size: 12px; width: 190px; }
#bar input:focus { outline: 1px solid #4b9ad6; }
#bar button { background: #232936; color: #e6e9ef; border: 1px solid #2a3242;
  border-radius: 5px; padding: 4px 10px; font-size: 12px; cursor: pointer; }
#bar button:hover { background: #2a3242; }
#root { flex: 1; min-height: 0; }
</style>
<style>`)
	page.WriteString(dashboardCSS)
	page.WriteString(`</style>
</head>
<body>
<div id="shell">
  <div id="bar">
    <strong style="color:#e6e9ef">CPA context</strong>
    <span>requests observed by this plugin since the proxy started</span>
    <span class="grow"></span>
    <input id="key" type="password" placeholder="management key" autocomplete="off">
    <button id="refresh">Refresh</button>
  </div>
  <div id="root"></div>
</div>
<script type="module">`)
	page.WriteString(dashboardJS)
	page.WriteString(`</script>
</body>
</html>
`)
	return page.String()
}
