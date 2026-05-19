package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "github.com/fyrash/fyra-cli/proto/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// logFields holds the parsed fields from a request log payload.
type logFields struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Status      int    `json:"status"`
	DurationMs  int64  `json:"duration_ms"`
	CacheStatus string `json:"cache_status"`
	Host        string `json:"host"`
	ClientIP    string `json:"client_ip"`
	UserAgent   string `json:"user_agent"`
	Referer     string `json:"referer"`
	BytesSent   int64  `json:"bytes_sent"`
}

// parseLogPayload extracts structured fields from a double-encoded or
// direct JSON payload string. Returns a zero struct on any parse failure.
func parseLogPayload(payload string) logFields {
	if payload == "" {
		return logFields{}
	}

	// Try double-encoded first: payload is a JSON string containing escaped JSON.
	var payloadStr string
	if err := json.Unmarshal([]byte(payload), &payloadStr); err == nil {
		var f logFields
		if json.Unmarshal([]byte(payloadStr), &f) == nil {
			return f
		}
	}

	// Fallback: try direct JSON object.
	var f logFields
	_ = json.Unmarshal([]byte(payload), &f)
	return f
}

// formatLine dispatches to the appropriate format function based on format name.
func formatLine(entry *pb.RequestLogEntry, format string, localTime bool) string {
	switch format {
	case "json":
		return formatJSONLine(entry)
	case "clf":
		return formatCLFLine(entry)
	case "combined":
		return formatCombinedLogFormatLine(entry)
	default:
		return formatTabularLine(entry, localTime)
	}
}

// formatTabularLine renders a log entry as a single line.
func formatTabularLine(entry *pb.RequestLogEntry, localTime bool) string {
	f := parseLogPayload(entry.Payload)
	return formatLogFields(entry.Ts, localTime, f.Host, f.ClientIP, f.Path, f.CacheStatus, f.UserAgent)
}

// formatLogFields renders the common log fields into a single tabular line.
func formatLogFields(ts string, localTime bool, host, ip, path, cacheStatus, userAgent string) string {
	if host == "" {
		host = "-"
	}
	if ip == "" {
		ip = "-"
	}
	if cacheStatus == "" {
		cacheStatus = "-"
	}
	ua := truncate(userAgent, 30)
	if ua == "" {
		ua = "-"
	}
	return fmt.Sprintf("%s  %-15s  %-15s  %-16s  %-4s  %s",
		formatTS(ts, localTime), host, ip, path, cacheStatus, ua)
}

// logHeader returns the header row for the logs table.
func logHeader() string {
	return fmt.Sprintf("%-19s  %-15s  %-15s  %-16s  %-4s  %s",
		"Timestamp", "Hostname", "IP", "Path", "Cache", "User-Agent")
}

// formatJSONLine renders a log entry as a single JSON line.
func formatJSONLine(entry *pb.RequestLogEntry) string {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(entry.Payload), &payload); err != nil {
		return fmt.Sprintf("{\"ts\": %q, \"node_id\": %q, \"payload\": %s}",
			entry.Ts, entry.NodeId, entry.Payload)
	}
	payload["ts"] = entry.Ts
	payload["node_id"] = entry.NodeId
	b, _ := json.Marshal(payload)
	return string(b)
}

// formatCLFLine renders a log entry in Common Log Format.
func formatCLFLine(entry *pb.RequestLogEntry) string {
	f := parseLogPayload(entry.Payload)

	t, _ := time.Parse(time.RFC3339, entry.Ts)
	ts := t.UTC().Format("02/Jan/2006:15:04:05 -0700")
	ip := f.ClientIP
	if ip == "" {
		ip = "-"
	}
	method := f.Method
	if method == "" {
		method = "-"
	}
	path := f.Path
	if path == "" {
		path = "-"
	}
	status := f.Status
	if status == 0 {
		status = 200
	}

	return fmt.Sprintf(`%s - - [%s] "%s %s HTTP/1.1" %d %d`, ip, ts, method, path, status, f.BytesSent)
}

// formatCombinedLogFormatLine renders a log entry in Combined Log Format.
func formatCombinedLogFormatLine(entry *pb.RequestLogEntry) string {
	f := parseLogPayload(entry.Payload)

	t, _ := time.Parse(time.RFC3339, entry.Ts)
	ts := t.UTC().Format("02/Jan/2006:15:04:05 -0700")
	ip := f.ClientIP
	if ip == "" {
		ip = "-"
	}
	method := f.Method
	if method == "" {
		method = "-"
	}
	path := f.Path
	if path == "" {
		path = "-"
	}
	status := f.Status
	if status == 0 {
		status = 200
	}

	referer := "-"
	if f.Referer != "" {
		referer = f.Referer
	}
	userAgent := "-"
	if f.UserAgent != "" {
		userAgent = f.UserAgent
	}

	return fmt.Sprintf(`%s - - [%s] "%s %s HTTP/1.1" %d %d %q %q`, ip, ts, method, path, status, f.BytesSent, referer, userAgent)
}

// formatTS formats an RFC3339 timestamp for display.
func formatTS(rfc3339 string, localTime bool) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		if len(rfc3339) >= 19 {
			return strings.Replace(rfc3339[:19], "T", " ", 1)
		}
		return rfc3339
	}
	if localTime {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

// truncate shortens s to at most maxLen characters, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// wrapText inserts newlines so that no line exceeds maxChars.
func wrapText(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	var b strings.Builder
	for len(s) > maxChars {
		breakAt := maxChars
		for i := maxChars; i > 0; i-- {
			if i < len(s) && s[i] == ' ' {
				breakAt = i
				break
			}
		}
		if breakAt == 0 {
			breakAt = maxChars
		}
		b.WriteString(s[:breakAt])
		b.WriteByte('\n')
		s = strings.TrimPrefix(s[breakAt:], " ")
	}
	b.WriteString(s)
	return b.String()
}

// friendlyLogsError maps gRPC errors to user-friendly messages.
func friendlyLogsError(err error) error {
	code := status.Code(err)
	switch code {
	case codes.NotFound:
		return fmt.Errorf("app not found or not deployed yet — try '%s push' first", binaryName)
	case codes.PermissionDenied:
		return fmt.Errorf("you don't have permission to view logs for this app")
	case codes.Unauthenticated:
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	case codes.FailedPrecondition:
		return fmt.Errorf("DNS management not configured on server")
	case codes.Unavailable:
		return fmt.Errorf("server unavailable — check your connection or try again later")
	case codes.Internal:
		return fmt.Errorf("internal server error — please try again later")
	case codes.DeadlineExceeded:
		return fmt.Errorf("request timed out — check your connection")
	default:
		return fmt.Errorf("failed to fetch logs: %w", err)
	}
}
