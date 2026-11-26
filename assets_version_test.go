package main

import (
	"strings"
	"testing"
)

// The assets are cached for a day, so their path has to change when the plugin
// changes. Without the version a browser serves the previous build after an
// upgrade and the dashboard looks broken until the cache expires.
func TestAssetPathsCarryTheVersion(t *testing.T) {
	for _, asset := range []string{assetScript, assetStyles} {
		if !strings.Contains(asset, pluginVersion) {
			t.Errorf("asset path %q does not carry the plugin version %q", asset, pluginVersion)
		}
	}
	// The two assets must not collide on a suffix match, or one would answer for
	// the other.
	if strings.HasSuffix(assetScript, assetStyles) || strings.HasSuffix(assetStyles, assetScript) {
		t.Error("the two asset paths share a suffix, so suffix matching is ambiguous")
	}
}

// The page and the routes must name the same paths, or the browser asks for
// something the plugin does not serve.
func TestPageAndRoutesAgreeOnAssetPaths(t *testing.T) {
	page := dashboardHTML()
	registration := newPlugin(nil).registerManagement()

	registered := map[string]bool{}
	for _, resource := range registration.Resources {
		registered[resource.Path] = true
	}
	for _, asset := range []string{assetScript, assetStyles} {
		if !registered[asset] {
			t.Errorf("asset %q is not registered", asset)
		}
		if !strings.Contains(page, assetPrefix+asset) {
			t.Errorf("the page does not reference %q", assetPrefix+asset)
		}
	}
}
