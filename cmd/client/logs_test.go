package main

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/fyrash/fyra-cli/proto/gen"
)

func TestParseLogPayload(t *testing.T) {
	t.Parallel()
	inner := `{"method":"GET","path":"/index.html","status":200,"duration_ms":5,"cache_status":"HIT","host":"app.fyra.sh","client_ip":"1.2.3.4","user_agent":"Mozilla/5.0 test"}`
	escaped, _ := json.Marshal(inner)

	tests := []struct {
		name    string
		payload string
		want    logFields
	}{
		{
			name:    "double encoded JSON",
			payload: string(escaped),
			want: logFields{
				Method:      "GET",
				Path:        "/index.html",
				Status:      200,
				DurationMs:  5,
				CacheStatus: "HIT",
				Host:        "app.fyra.sh",
				ClientIP:    "1.2.3.4",
				UserAgent:   "Mozilla/5.0 test",
			},
		},
		{
			name:    "direct JSON",
			payload: `{"method":"POST","path":"/api","status":201,"duration_ms":10,"cache_status":"","host":"h","client_ip":"9.8.7.6","user_agent":"curl/8"}`,
			want: logFields{
				Method:     "POST",
				Path:       "/api",
				Status:     201,
				DurationMs: 10,
				Host:       "h",
				ClientIP:   "9.8.7.6",
				UserAgent:  "curl/8",
			},
		},
		{
			name:    "empty payload",
			payload: "",
			want:    logFields{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseLogPayload(tt.payload)
			if got != tt.want {
				t.Errorf("parseLogPayload() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		maxChars int
		want     string
	}{
		{name: "short string unchanged", input: "hello", maxChars: 10, want: "hello"},
		{name: "wraps at word boundary", input: "hello world foo", maxChars: 11, want: "hello world\nfoo"},
		{name: "no spaces falls back to hard break", input: "abcdefghij", maxChars: 5, want: "abcde\nfghij"},
		{name: "zero maxChars returns unchanged", input: "hello world", maxChars: 0, want: "hello world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := wrapText(tt.input, tt.maxChars)
			if got != tt.want {
				t.Errorf("wrapText(%q, %d)\ngot:  %q\nwant: %q", tt.input, tt.maxChars, got, tt.want)
			}
		})
	}
}

func TestFormatCLFLine(t *testing.T) {
	t.Parallel()
	inner := `{"method":"GET","path":"/index.html","status":200,"duration_ms":5,"cache_status":"HIT","host":"app.fyra.sh","client_ip":"1.2.3.4","user_agent":"curl/8"}`
	escaped, _ := json.Marshal(inner)

	tests := []struct {
		name    string
		entry   *pb.RequestLogEntry
		wantHas []string
	}{
		{
			name: "normal entry",
			entry: &pb.RequestLogEntry{
				Ts:      "2026-05-06T14:23:45Z",
				Payload: string(escaped),
			},
			wantHas: []string{
				"1.2.3.4 - -",
				"[06/May/2026:14:23:45",
				`"GET /index.html HTTP/1.1" 200`,
			},
		},
		{
			name: "missing fields use dashes",
			entry: &pb.RequestLogEntry{
				Ts:      "2026-05-06T14:23:45Z",
				Payload: `{}`,
			},
			wantHas: []string{
				"- - -",
				`"- - HTTP/1.1" 200`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatCLFLine(tt.entry)
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("formatCLFLine() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestFormatLine(t *testing.T) {
	t.Parallel()
	inner := `{"method":"GET","path":"/api/data","status":200,"duration_ms":12,"cache_status":"MISS","host":"app.fyra.sh","client_ip":"10.0.0.1","user_agent":"curl/8.7","referer":"https://example.com","bytes_sent":1234}`
	escaped, _ := json.Marshal(inner)

	entry := &pb.RequestLogEntry{
		Ts:      "2026-05-10T08:30:00Z",
		NodeId:  "node-1",
		Payload: string(escaped),
	}

	tests := []struct {
		name    string
		format  string
		wantHas []string
	}{
		{
			name:   "clf format",
			format: "clf",
			wantHas: []string{
				"10.0.0.1 - -",
				`"GET /api/data HTTP/1.1" 200 1234`,
			},
		},
		{
			name:   "combined format",
			format: "combined",
			wantHas: []string{
				"10.0.0.1 - -",
				`"GET /api/data HTTP/1.1" 200 1234`,
				`"https://example.com"`,
				`"curl/8.7"`,
			},
		},
		{
			name:   "json format",
			format: "json",
			wantHas: []string{
				`"ts": "2026-05-10T08:30:00Z"`,
				`"node_id": "node-1"`,
			},
		},
		{
			name:   "table format",
			format: "table",
			wantHas: []string{
				"app.fyra.sh",
				"10.0.0.1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatLine(entry, tt.format, false)
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("formatLine(_, %q, false) = %q, want to contain %q", tt.format, got, want)
				}
			}
		})
	}
}
