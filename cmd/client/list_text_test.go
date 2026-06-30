package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	pb "github.com/fyrash/fyra-cli/proto/gen"
)

func stubListFn(t *testing.T, reply *pb.ListAppsResponse, err error) (listAppsFn, **pb.ListAppsRequest) {
	t.Helper()
	var got *pb.ListAppsRequest
	fn := func(ctx context.Context, req *pb.ListAppsRequest) (*pb.ListAppsResponse, error) {
		got = req
		return reply, err
	}
	return fn, &got
}

func TestListText_RendersRows(t *testing.T) {
	t.Parallel()
	reply := &pb.ListAppsResponse{
		Apps: []*pb.AppInfo{
			{SlugName: "alpha", Domain: "apps.fyra.sh", Url: "https://alpha.apps.fyra.sh", CreatedAt: "2026-06-01T12:00:00Z"},
			{SlugName: "beta", Domain: "ignite.fyra.sh", Url: "", CreatedAt: "2026-06-02T08:30:00Z"},
		},
	}
	fn, gotPtr := stubListFn(t, reply, nil)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	if err := listText(context.Background(), cfg, &out, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = gotPtr // ListAppsRequest carries no fields; the call is exercised via the stub.

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 rows, got %d (%q)", len(lines), out.String())
	}
	want0 := "alpha\tapps.fyra.sh\thttps://alpha.apps.fyra.sh\t2026-06-01"
	if lines[0] != want0 {
		t.Errorf("row 0 = %q, want %q", lines[0], want0)
	}
	// beta never deployed → empty url column.
	want1 := "beta\tignite.fyra.sh\t\t2026-06-02"
	if lines[1] != want1 {
		t.Errorf("row 1 = %q, want %q", lines[1], want1)
	}
}

func TestListText_EmptyList(t *testing.T) {
	t.Parallel()
	reply := &pb.ListAppsResponse{Apps: nil}
	fn, _ := stubListFn(t, reply, nil)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	if err := listText(context.Background(), cfg, &out, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for empty list; got %q", out.String())
	}
}

func TestListText_RPCError(t *testing.T) {
	t.Parallel()
	rpcErr := errors.New("unavailable")
	fn, _ := stubListFn(t, nil, rpcErr)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err := listText(context.Background(), cfg, &out, fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, rpcErr) {
		t.Errorf("expected wrapped rpc error; got %v", err)
	}
	if !strings.Contains(err.Error(), "list apps") {
		t.Errorf("expected 'list apps' context; got %q", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout on error; got %q", out.String())
	}
}

func TestTruncDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "rfc3339", in: "2026-06-30T12:34:56Z", want: "2026-06-30"},
		{name: "short", in: "2026", want: "2026"},
		{name: "empty", in: "", want: ""},
		{name: "spaces trimmed", in: "  x  ", want: "x"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := truncDate(tc.in); got != tc.want {
				t.Errorf("truncDate(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
