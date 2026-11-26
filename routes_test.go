package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// Every route the client or the operator can reach goes through one dispatch
// function. A route that is not registered never reaches the plugin at all, so
// the registration and the dispatch have to name the same paths.
func TestRegisteredRoutesResolve(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	registration := p.registerManagement()

	registered := make(map[string]bool, len(registration.Routes))
	for _, route := range registration.Routes {
		if route.Handler != nil {
			t.Errorf("%s %s carries a handler, the host injects it", route.Method, route.Path)
		}
		registered[route.Method+" "+route.Path] = true
	}
	// Resources are served to the browser as GET only, so they are registered
	// separately from the authenticated management routes.
	resources := make(map[string]bool, len(registration.Resources))
	for _, resource := range registration.Resources {
		if resource.Handler != nil {
			t.Errorf("resource %s carries a handler, the host injects it", resource.Path)
		}
		resources[resource.Path] = true
	}
	if !resources[pathDashboard] {
		t.Errorf("the dashboard resource %s is not registered", pathDashboard)
	}

	handler := managementHandler{plugin: p}
	probes := []struct {
		method string
		path   string
		query  string
	}{
		{http.MethodGet, pathOverview, ""},
		{http.MethodGet, pathSessions, ""},
		{http.MethodGet, pathSession, "session=missing"},
		{http.MethodGet, pathEvents, ""},
		{http.MethodPost, pathReset, ""},
		{http.MethodPost, clientDetailPath, ""},
		{http.MethodPost, clientFactsPath, ""},
		{http.MethodPost, clientHistoryPath, ""},
		{http.MethodPost, clientDeltaPath, ""},
		{http.MethodPost, clientRecomPath, ""},
		{http.MethodPost, clientVersionPath, ""},
		{http.MethodPost, clientFleetPath, ""},
		{http.MethodPost, clientOverviewPath, ""},
		{http.MethodPost, clientBalancePath, ""},
		{http.MethodPost, clientSkillsPath, ""},
	}

	// The page's own assets are resources, served to the browser without a key.
	for _, asset := range []string{assetScript, assetStyles} {
		if !resources[asset] {
			t.Errorf("asset %s is not registered, so the page would load nothing", asset)
		}
	}
	for _, probe := range probes {
		if !registered[probe.method+" "+probe.path] {
			t.Errorf("%s %s is not registered, so the host would answer 404", probe.method, probe.path)
		}
		query := url.Values{}
		if probe.query != "" {
			parts := strings.SplitN(probe.query, "=", 2)
			query.Set(parts[0], parts[1])
		}
		resp, err := handler.HandleManagement(t.Context(), pluginapi.ManagementRequest{
			Method: probe.method,
			Path:   "/v0/management" + probe.path,
			Query:  query,
			Body:   []byte("{}"),
		})
		if err != nil {
			t.Errorf("%s %s failed: %v", probe.method, probe.path, err)
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("%s %s answered 404 through the dispatcher", probe.method, probe.path)
		}
		if len(resp.Body) == 0 {
			t.Errorf("%s %s answered an empty body", probe.method, probe.path)
		}
	}
}

// The dispatch must answer a resource path the way the browser asks for it and a
// management path with the prefix the host uses.
func TestDispatchMatchesBySuffix(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	handler := managementHandler{plugin: p}

	for _, path := range []string{
		"/v0/management" + pathOverview,
		"/v0/resource/plugins/cpa-context" + pathOverview,
		pathOverview,
	} {
		resp, err := handler.HandleManagement(t.Context(), pluginapi.ManagementRequest{
			Method: http.MethodGet, Path: path,
		})
		if err != nil {
			t.Fatalf("%s failed: %v", path, err)
		}
		var body map[string]any
		if err := json.Unmarshal(resp.Body, &body); err != nil {
			t.Fatalf("%s answered invalid JSON: %v", path, err)
		}
		if body["plugin"] != pluginID {
			t.Errorf("%s answered plugin %v", path, body["plugin"])
		}
	}
}

// The dashboard page must embed the built client, not just a shell: a page that
// renders nothing is the failure this guards.
func TestDashboardEmbedsTheClient(t *testing.T) {
	page := dashboardHTML()
	for _, want := range []string{"<div id=\"root\">", "id=\"key\"", "id=\"refresh\""} {
		if !strings.Contains(page, want) {
			t.Errorf("the dashboard page is missing %q", want)
		}
	}
	if len(page) > 32_000 {
		t.Errorf("the dashboard page is %d bytes, the assets are probably inlined again", len(page))
	}
	for _, asset := range []string{assetScript, assetStyles} {
		body, contentType, ok := assetFor(asset)
		if !ok || len(body) < 1000 {
			t.Errorf("asset %s is empty or missing", asset)
		}
		if contentType == "" {
			t.Errorf("asset %s has no content type", asset)
		}
	}
}
