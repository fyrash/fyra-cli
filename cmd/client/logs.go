package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	pb "github.com/fyrash/fyra-cli/proto/gen"

	"golang.org/x/term"
)

var (
	logsFormat string
	logsSince  string
	logsLimit  int32
	logsOutput string
	logsFollow bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Stream HTTP request logs for the current app",
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().StringVar(&logsFormat, "format", "table", "output format: table, json, clf, combined")
	logsCmd.Flags().Bool("json", false, "shorthand for --format json (deprecated)")
	logsCmd.Flags().StringVar(&logsSince, "since", "", `start time for initial fetch — RFC3339 or relative (e.g. "5 minutes ago", "2 days ago")`)
	logsCmd.Flags().Int32Var(&logsLimit, "limit", 200, "page size for log fetches")
	logsCmd.Flags().StringVar(&logsOutput, "output", "", "write logs to file without TUI")
	logsCmd.Flags().BoolVar(&logsFollow, "follow", false, "keep streaming new log entries (pipe mode only)")
	logsCmd.Flags().MarkHidden("json")
}

var reRelativeTime = regexp.MustCompile(`^(\d+)\s+(minute|minutes|hour|hours|day|days|week|weeks)\s+ago$`)

// parseSinceFlag converts a --since value to RFC3339.
func parseSinceFlag(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return s, nil
	}
	m := reRelativeTime.FindStringSubmatch(strings.TrimSpace(strings.ToLower(s)))
	if m != nil {
		n, _ := strconv.Atoi(m[1])
		var d time.Duration
		switch strings.TrimSuffix(m[2], "s") {
		case "minute":
			d = time.Duration(n) * time.Minute
		case "hour":
			d = time.Duration(n) * time.Hour
		case "day":
			d = time.Duration(n) * 24 * time.Hour
		case "week":
			d = time.Duration(n) * 7 * 24 * time.Hour
		}
		return time.Now().Add(-d).UTC().Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("invalid --since value %q: use RFC3339 (e.g. 2006-01-02T15:04:05Z) or relative (e.g. \"5 minutes ago\", \"2 days ago\")", s)
}

func runLogs(cmd *cobra.Command, _ []string) error {
	if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
		logsFormat = "json"
	}
	switch logsFormat {
	case "table", "json", "clf", "combined":
	default:
		return fmt.Errorf("invalid --format %q: must be table, json, clf or combined", logsFormat)
	}

	parsed, err := parseSinceFlag(logsSince)
	if err != nil {
		return err
	}
	logsSince = parsed

	af, err := readAppFile()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	// Pipe detected — stream line-by-line to stdout.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return streamLogs(cmd.Context(), af.Slug, af.Domain, cfg, logsFormat, logsSince, logsFollow)
	}

	// Non-interactive file dump.
	if logsOutput != "" {
		return dumpLogsToFile(cmd.Context(), af.Slug, af.Domain, cfg)
	}

	// TUI mode.
	m := newLogsModel(af.Slug, af.Domain, cfg, cmd.Context())

	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	lm, ok := final.(logsModel)
	if ok && lm.err != nil {
		return lm.err
	}

	if lm.totalEntries > 0 {
		fmt.Printf("\nCaptured %d log entries.\n", lm.totalEntries)
	} else {
		fmt.Println("\nNo log entries captured.")
	}

	return nil
}

// dumpLogsToFile fetches all log pages and writes them to the output file
// without launching the TUI.
func dumpLogsToFile(ctx context.Context, slug, domain string, cfg clientConfig) error {
	f, err := os.OpenFile(logsOutput, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open output file: %w", err)
	}
	defer f.Close()

	client, cleanup, err := cfg.dial()
	if err != nil {
		return err
	}
	defer cleanup()

	seen := make(map[string]struct{})
	var firstTS string
	total := 0
	page := 0

	since := logsSince
	for {
		page++
		resp, err := client.GetRequestLogs(authContext(ctx, cfg.Token), &pb.GetRequestLogsRequest{
			SlugName: slug,
			Domain:   domain,
			Since:    since,
			Until:    firstTS,
			Limit:    logsLimit,
		})
		if err != nil {
			return friendlyLogsError(err)
		}

		entries := resp.Entries
		sort.Slice(entries, func(i, j int) bool { return entries[i].Ts < entries[j].Ts })

		count := 0
		for _, entry := range entries {
			key := entry.Ts + ":" + entry.NodeId
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			fmt.Fprintln(f, formatLine(entry, logsFormat, false))
			count++
			total++
			if firstTS == "" || entry.Ts < firstTS {
				firstTS = entry.Ts
			}
		}

		fmt.Fprintf(os.Stderr, "\rDumping logs... page %d (%d entries)", page, total)

		if count == 0 {
			break
		}
	}

	fmt.Fprintf(os.Stderr, "\nDumped %d log entries to %s\n", total, logsOutput)
	return nil
}

func streamLogs(ctx context.Context, slug, domain string, cfg clientConfig, format, since string, follow bool) error {
	client, cleanup, err := cfg.dial()
	if err != nil {
		return err
	}
	defer cleanup()

	seen := make(map[string]struct{})
	lastTS := since

	for {
		resp, err := client.GetRequestLogs(authContext(ctx, cfg.Token), &pb.GetRequestLogsRequest{
			SlugName: slug,
			Domain:   domain,
			Since:    lastTS,
			Limit:    logsLimit,
		})
		if err != nil {
			return friendlyLogsError(err)
		}

		entries := resp.Entries
		sort.Slice(entries, func(i, j int) bool { return entries[i].Ts < entries[j].Ts })

		for _, entry := range entries {
			key := entry.Ts + ":" + entry.NodeId
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			fmt.Fprintln(os.Stdout, formatLine(entry, format, false))
		}

		if len(entries) > 0 {
			lastTS = entries[len(entries)-1].Ts
		}

		if !follow {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}

	return nil
}
