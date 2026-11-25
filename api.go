package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// Routes are registered under /v0/management/ (authenticated) and
// /v0/resource/plugins/cpa-context/ (browser, unauthenticated).
const (
	pathOverview  = "/cpa-context/overview"
	pathSession   = "/cpa-context/session"
	pathEvents    = "/cpa-context/events"
	pathReset     = "/cpa-context/reset"
	pathDashboard = "/dashboard"
)

// registerManagement declares the routes. The host injects the Handler into
// each returned route, so the registration itself must carry none.
func (p *plugin) registerManagement() pluginapi.ManagementRegistrationResponse {
	return pluginapi.ManagementRegistrationResponse{
		Routes: []pluginapi.ManagementRoute{
			{Method: http.MethodGet, Path: pathOverview, Description: "Context composition per conversation"},
			{Method: http.MethodGet, Path: pathSession, Description: "One conversation with its timeline"},
			{Method: http.MethodGet, Path: pathEvents, Description: "Context window change events"},
			{Method: http.MethodPost, Path: pathReset, Description: "Drop collected context data"},
		},
		Resources: []pluginapi.ResourceRoute{
			{Path: pathDashboard, Menu: "Context", Description: "Context composition dashboard"},
		},
	}
}

// handleManagement decodes the host frame and dispatches to the shared handler.
func (p *plugin) handleManagement(raw []byte) any {
	var req pluginapi.ManagementRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorResponse(http.StatusBadRequest, "invalid request")
	}
	handler := managementHandler{plugin: p}
	resp, err := handler.HandleManagement(context.Background(), req)
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "handler failed")
	}
	return resp
}

type managementHandler struct {
	plugin *plugin
}

// HandleManagement serves both the authenticated management route and the
// browser resource route, so the paths are matched by suffix.
func (h managementHandler) HandleManagement(_ context.Context, req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	path := req.Path
	if index := strings.Index(path, "/cpa-context/"); index >= 0 {
		path = path[index:]
	}
	if strings.HasSuffix(path, pathDashboard) || strings.HasSuffix(req.Path, pathDashboard) {
		return htmlResponse(dashboardHTML()), nil
	}

	switch {
	case strings.HasSuffix(path, "/overview"):
		return jsonResponse(h.plugin.overview()), nil
	case strings.HasSuffix(path, "/session"):
		id := ""
		if req.Query != nil {
			id = strings.TrimSpace(req.Query.Get("id"))
		}
		sess := h.plugin.store.snapshotSession(id)
		if sess == nil {
			return jsonResponse(map[string]any{"error": "session not found", "id": id}), nil
		}
		return jsonResponse(map[string]any{
			"session": sess,
			"events":  h.plugin.store.snapshotEvents(id, 200),
		}), nil
	case strings.HasSuffix(path, "/events"):
		limit := 500
		sessionID := ""
		if req.Query != nil {
			if raw := strings.TrimSpace(req.Query.Get("limit")); raw != "" {
				if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 5000 {
					limit = parsed
				}
			}
			sessionID = strings.TrimSpace(req.Query.Get("id"))
		}
		return jsonResponse(map[string]any{
			"events": h.plugin.store.snapshotEvents(sessionID, limit),
		}), nil
	case strings.HasSuffix(path, "/reset"):
		if req.Method != http.MethodPost {
			return errorResponse(http.StatusMethodNotAllowed, "POST required"), nil
		}
		h.plugin.store.reset()
		return jsonResponse(map[string]any{"ok": true}), nil
	}
	return errorResponse(http.StatusNotFound, "unknown route"), nil
}

// overview is the whole dashboard payload in one round trip.
type overview struct {
	Plugin     string            `json:"plugin"`
	Version    string            `json:"version"`
	Sessions   []*session        `json:"sessions"`
	Recent     []*snapshot       `json:"recent"`
	Events     []event           `json:"events"`
	Totals     totals            `json:"totals"`
	Categories []string          `json:"categories"`
	Limits     limitsDescription `json:"limits"`
}

type totals struct {
	Sessions      int   `json:"sessions"`
	Requests      int   `json:"requests"`
	Failed        int   `json:"failed"`
	InputTokens   int64 `json:"inputTokens"`
	OutputTokens  int64 `json:"outputTokens"`
	CacheRead     int64 `json:"cacheReadTokens"`
	CacheWrite    int64 `json:"cacheWriteTokens"`
	Reasoning     int64 `json:"reasoningTokens"`
	CurrentWindow int64 `json:"currentWindowTokens"`
}

type limitsDescription struct {
	// Scope tells the reader what this data covers: the plugin can only report
	// traffic it observed since the proxy started.
	Scope string `json:"scope"`
}

func (p *plugin) overview() overview {
	sessions := p.store.snapshotSessions()
	sum := totals{Sessions: len(sessions)}
	for _, sess := range sessions {
		sum.Requests += sess.Requests
		sum.Failed += sess.Failed
		sum.InputTokens += sess.InputTokens
		sum.OutputTokens += sess.OutputTokens
		sum.CacheRead += sess.CacheRead
		sum.CacheWrite += sess.CacheWrite
		sum.Reasoning += sess.Reasoning
		sum.CurrentWindow += sess.LastContext
	}
	return overview{
		Plugin:     pluginID,
		Version:    pluginVersion,
		Sessions:   sessions,
		Recent:     p.store.snapshotRecent(200),
		Events:     p.store.snapshotEvents("", 300),
		Totals:     sum,
		Categories: categoryOrder,
		Limits: limitsDescription{
			Scope: "Requests observed by this plugin since the proxy started. CPA exposes no history API to plugins.",
		},
	}
}

func jsonHeader() http.Header {
	return http.Header{"Content-Type": []string{"application/json; charset=utf-8"}}
}

func jsonResponse(value any) pluginapi.ManagementResponse {
	body, err := json.Marshal(value)
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "encode failed")
	}
	return pluginapi.ManagementResponse{StatusCode: http.StatusOK, Headers: jsonHeader(), Body: body}
}

func errorResponse(status int, message string) pluginapi.ManagementResponse {
	body, _ := json.Marshal(map[string]string{"error": message})
	return pluginapi.ManagementResponse{StatusCode: status, Headers: jsonHeader(), Body: body}
}

func htmlResponse(html string) pluginapi.ManagementResponse {
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       []byte(html),
	}
}
