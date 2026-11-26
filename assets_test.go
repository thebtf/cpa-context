package main

import (
	"strings"
	"testing"
)

// A relative asset reference resolves against the host root and 404s, which
// leaves a blank page. The page must name the absolute resource path.
func TestDashboardReferencesAssetsAbsolutely(t *testing.T) {
	page := dashboardHTML()
	wants := []string{
		"href=\"" + assetPrefix + assetStyles + "\"",
		"src=\"" + assetPrefix + assetScript + "\"",
	}
	for _, want := range wants {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not reference %s", want)
		}
	}
	if strings.Contains(page, "href=\""+assetStyles+"\"") {
		t.Error("the stylesheet is referenced relatively, which 404s")
	}
	if strings.Contains(page, "src=\""+assetScript+"\"") {
		t.Error("the script is referenced relatively, which 404s")
	}
}
