// This file proxies to an already-running Prometheus/Loki/Alertmanager
// stack (Roadmap Phase 16) — Vinaire never installs or evaluates queries
// itself, it only forwards to whatever's configured (see
// Handler.PrometheusURL/LokiURL/AlertmanagerURL). Any of the three may be
// unconfigured or unreachable independently of the others; every handler
// here responds with an explicit `available: false` instead of erroring,
// the same pattern Phase 3 (metrics-server) and Phase 8 (Gateway API) use.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// observabilityHTTPClient is shared across every proxy below — a bounded
// timeout keeps a slow or unreachable observability backend from hanging a
// request indefinitely.
var observabilityHTTPClient = &http.Client{Timeout: 10 * time.Second}

// fetchUpstream GETs baseURL+path with query params and returns the raw
// response body, erroring on a network failure or non-2xx status.
func fetchUpstream(ctx context.Context, baseURL, path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := observabilityHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// --- Prometheus ---

// metricsQueryResponse wraps Prometheus's own JSON response as-is under
// `result` — this backend never evaluates PromQL, so there's no typed
// response to build; the frontend reads Prometheus's response shape
// directly, the same as it would calling Prometheus itself.
type metricsQueryResponse struct {
	Available bool            `json:"available"`
	Reason    string          `json:"reason,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// MetricsQuery handles GET /api/v1/metrics/query?query=<promql>&time=,
// proxying to Prometheus's instant-query endpoint.
func (h *Handler) MetricsQuery(w http.ResponseWriter, r *http.Request) {
	h.proxyPrometheus(w, r, "/api/v1/query")
}

// MetricsQueryRange handles
// GET /api/v1/metrics/query_range?query=&start=&end=&step=, proxying to
// Prometheus's range-query endpoint — what a CPU/Memory sparkline needs.
func (h *Handler) MetricsQueryRange(w http.ResponseWriter, r *http.Request) {
	h.proxyPrometheus(w, r, "/api/v1/query_range")
}

func (h *Handler) proxyPrometheus(w http.ResponseWriter, r *http.Request, promPath string) {
	entry, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	fetch, reason := h.prometheusFetcher(r.Context(), entry.Name, cs)
	if fetch == nil {
		respondJSON(w, metricsQueryResponse{Available: false, Reason: reason})
		return
	}

	body, err := fetch(r.Context(), promPath, r.URL.Query())
	if err != nil {
		respondJSON(w, metricsQueryResponse{Available: false, Reason: err.Error()})
		return
	}
	respondJSON(w, metricsQueryResponse{Available: true, Result: json.RawMessage(body)})
}

// --- Alertmanager ---

// AlertRow matches the frontend's AlertRow type — a simplified view of
// Alertmanager v2's alert shape (just what the Topbar bell needs).
type AlertRow struct {
	Name        string `json:"name"`
	Severity    string `json:"severity"`
	Summary     string `json:"summary"`
	StatusColor string `json:"statusColor"`
	StartsAt    string `json:"startsAt"`
}

type alertsResponse struct {
	Available bool       `json:"available"`
	Reason    string     `json:"reason,omitempty"`
	Alerts    []AlertRow `json:"alerts"`
}

// alertmanagerAlert mirrors just the fields this backend reads from
// Alertmanager's GET /api/v2/alerts response.
type alertmanagerAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
}

// Alerts handles GET /api/v1/alerts — every currently active alert
// (firing or suppressed) from Alertmanager.
func (h *Handler) Alerts(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	fetch, reason := h.alertmanagerFetcher(r.Context(), cs)
	if fetch == nil {
		respondJSON(w, alertsResponse{Available: false, Reason: reason})
		return
	}

	body, err := fetch(r.Context(), "/api/v2/alerts", nil)
	if err != nil {
		respondJSON(w, alertsResponse{Available: false, Reason: err.Error()})
		return
	}

	var raw []alertmanagerAlert
	if err := json.Unmarshal(body, &raw); err != nil {
		respondJSON(w, alertsResponse{Available: false, Reason: "unexpected Alertmanager response: " + err.Error()})
		return
	}

	rows := make([]AlertRow, 0, len(raw))
	for _, a := range raw {
		// the heartbeat rules kube-prometheus-stack ships: "Watchdog" fires forever on
		// purpose, to prove Alertmanager is alive; "InfoInhibitor" only silences others
		if n := a.Labels["alertname"]; n == "Watchdog" || n == "InfoInhibitor" {
			continue
		}
		color := StatusWarn
		if a.Labels["severity"] == "critical" {
			color = StatusErr
		}
		summary := a.Annotations["summary"]
		if summary == "" {
			summary = a.Annotations["description"]
		}
		rows = append(rows, AlertRow{
			Name:        a.Labels["alertname"],
			Severity:    a.Labels["severity"],
			Summary:     summary,
			StatusColor: string(color),
			StartsAt:    a.StartsAt,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].StartsAt > rows[j].StartsAt })
	respondJSON(w, alertsResponse{Available: true, Alerts: rows})
}

// --- Loki ---

// LogLine matches the frontend's LogLine type (frontend/src/lib/types.ts) —
// the exact shape Phase 4's live-tail already uses, so the Logs tab can
// render historical and live lines with the same component.
type LogLine struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

type logsSearchResponse struct {
	Available bool      `json:"available"`
	Reason    string    `json:"reason,omitempty"`
	Lines     []LogLine `json:"lines"`
}

const defaultLogsSearchSince = "1h"

// LogsSearch handles
// GET /api/v1/pods/{namespace}/{name}/logs/search?query=&since=. Proxies to
// Loki's range-query endpoint with a LogQL selector scoped to this pod —
// this assumes the cluster's log shipper labels entries with
// `namespace`/`pod` (the common Promtail/Grafana Agent convention). A
// differently-labeled Loki setup simply won't find this pod's logs; that's
// a limitation of proxying rather than reimplementing label discovery.
func (h *Handler) LogsSearch(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if !nsRe.MatchString(namespace) || !objNameRe.MatchString(name) {
		respondError(w, http.StatusBadRequest, "invalid namespace or pod name")
		return
	}
	// Loki holds every pod's logs and doesn't know who is asking, so the check the
	// cluster would have made on `pods/log` is made here, as the caller — without it
	// anyone signed in could read any namespace's logs through the search.
	allowed, err := callerCan(r.Context(), cs, "get", "", "pods", "log", namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	if !allowed {
		respondError(w, http.StatusForbidden, fmt.Sprintf("you don't have permission to read the logs of pod %s/%s (get pods/log)", namespace, name))
		return
	}

	query := r.URL.Query().Get("query")
	if len(query) > 500 {
		respondError(w, http.StatusBadRequest, "the search text is too long")
		return
	}
	since := r.URL.Query().Get("since")
	if since == "" {
		since = defaultLogsSearchSince
	}
	duration, err := time.ParseDuration(since)
	if err != nil || duration <= 0 || duration > 31*24*time.Hour {
		respondError(w, http.StatusBadRequest, "invalid since duration (up to 744h)")
		return
	}

	logql := fmt.Sprintf("{namespace=%q, pod=%q}", namespace, name)
	if query != "" {
		logql += fmt.Sprintf(" |= %q", query)
	}
	end := time.Now()
	start := end.Add(-duration)
	params := url.Values{
		"query": {logql},
		"start": {strconv.FormatInt(start.UnixNano(), 10)},
		"end":   {strconv.FormatInt(end.UnixNano(), 10)},
		"limit": {"1000"},
	}
	body, reason := h.searchLoki(r.Context(), cs, params)
	if reason != "" {
		respondJSON(w, logsSearchResponse{Available: false, Reason: reason})
		return
	}
	lines, err := parseLokiQueryRange(body)
	if err != nil {
		respondJSON(w, logsSearchResponse{Available: false, Reason: "unexpected Loki response: " + err.Error()})
		return
	}
	respondJSON(w, logsSearchResponse{Available: true, Lines: lines})
}

// lokiQueryRangeResponse mirrors just the fields this backend reads from
// Loki's GET /loki/api/v1/query_range response — each stream carries
// [timestampNanos, line] pairs.
type lokiQueryRangeResponse struct {
	Data struct {
		Result []struct {
			Values [][2]string `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func parseLokiQueryRange(body []byte) ([]LogLine, error) {
	var parsed lokiQueryRangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	lines := make([]LogLine, 0)
	for _, stream := range parsed.Data.Result {
		for _, v := range stream.Values {
			nanos, err := strconv.ParseInt(v[0], 10, 64)
			if err != nil {
				continue
			}
			lines = append(lines, LogLine{
				Time:  time.Unix(0, nanos).Format("2006-01-02 15:04:05"),
				Level: guessLogLevel(v[1]),
				Text:  v[1],
			})
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Time < lines[j].Time })
	return lines, nil
}

// guessLogLevel mirrors internal/stream's identical live-tail heuristic
// (internal/stream/logs.go's guessLogLevel) — duplicated rather than
// shared because internal/stream already imports internal/api (for the
// Row mapper functions used in its informers), so the reverse import would
// cycle. Most raw container output has no machine-readable level, so this
// greps for common level words rather than pretending to parse an
// arbitrary format.
func guessLogLevel(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "error"), strings.Contains(lower, "fatal"), strings.Contains(lower, "panic"):
		return "ERROR"
	case strings.Contains(lower, "warn"):
		return "WARN"
	default:
		return "INFO"
	}
}
