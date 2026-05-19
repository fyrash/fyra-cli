package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/fyrash/fyra-cli/proto/gen"
)

func TestCnameTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		domain string
		zone   string
		want   string
	}{
		{
			name:   "single-level domain",
			domain: "mysite.com",
			zone:   "ignite.fyra.sh",
			want:   "mysite-com.ignite.fyra.sh",
		},
		{
			name:   "subdomain",
			domain: "blog.mysite.com",
			zone:   "ignite.fyra.sh",
			want:   "blog-mysite-com.ignite.fyra.sh",
		},
		{
			name:   "multiple dots",
			domain: "a.b.c.com",
			zone:   "ignite.fyra.sh",
			want:   "a-b-c-com.ignite.fyra.sh",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cnameTarget(tc.domain, tc.zone)
			if got != tc.want {
				t.Errorf("cnameTarget(%q, %q) = %q, want %q", tc.domain, tc.zone, got, tc.want)
			}
		})
	}
}

func TestPollCertStatusReturnsOnCompleted(t *testing.T) {
	t.Parallel()
	calls := 0
	getStatus := func() (*pb.GetCertStatusResponse, error) {
		calls++
		return &pb.GetCertStatusResponse{
			Status:        pb.GetCertStatusResponse_CERT_STATUS_COMPLETED,
			StatusMessage: "completed",
		}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pollCertStatus(ctx, getStatus)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestPollCertStatusPollsThroughFailed(t *testing.T) {
	t.Parallel()
	responses := []*pb.GetCertStatusResponse{
		{Status: pb.GetCertStatusResponse_CERT_STATUS_PENDING, StatusMessage: "pending"},
		{Status: pb.GetCertStatusResponse_CERT_STATUS_FAILED, StatusMessage: "failed_validation", ErrorDetail: "acme: some internal error"},
		{Status: pb.GetCertStatusResponse_CERT_STATUS_PENDING, StatusMessage: "provisioning"},
		{Status: pb.GetCertStatusResponse_CERT_STATUS_COMPLETED, StatusMessage: "completed"},
	}
	idx := 0
	getStatus := func() (*pb.GetCertStatusResponse, error) {
		if idx >= len(responses) {
			t.Fatal("too many calls")
		}
		r := responses[idx]
		idx++
		return r, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := pollCertStatus(ctx, getStatus)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if idx != 4 {
		t.Fatalf("expected 4 calls (polling through failed), got %d", idx)
	}
}

func TestPollCertStatusStopsOnContextCancel(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	getStatus := func() (*pb.GetCertStatusResponse, error) {
		if calls.Add(1) == 1 {
			cancel()
		}
		return &pb.GetCertStatusResponse{
			Status:        pb.GetCertStatusResponse_CERT_STATUS_PENDING,
			StatusMessage: "pending",
		}, nil
	}

	err := pollCertStatus(ctx, getStatus)
	if err == nil {
		t.Fatal("expected context cancelled error, got nil")
	}
}
