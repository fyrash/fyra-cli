package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	pb "github.com/fyrash/fyra-cli/proto/gen"
)

func stubWhoamiFn(t *testing.T, reply *pb.WhoAmIResponse, err error) (whoamiFn, **pb.WhoAmIRequest) {
	t.Helper()
	var got *pb.WhoAmIRequest
	fn := func(ctx context.Context, req *pb.WhoAmIRequest) (*pb.WhoAmIResponse, error) {
		got = req
		return reply, err
	}
	return fn, &got
}

func TestWhoamiText_PrintsEmail(t *testing.T) {
	t.Parallel()
	reply := &pb.WhoAmIResponse{Email: "user@example.com", Confirmed: true}
	fn, _ := stubWhoamiFn(t, reply, nil)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	if err := whoamiText(context.Background(), cfg, &out, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "user@example.com\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
}

func TestWhoamiText_UnconfirmedStillPrintsEmail(t *testing.T) {
	t.Parallel()
	// In text mode we print just the email; the unconfirmed notice is a TUI-only
	// affordance. This pins that contract.
	reply := &pb.WhoAmIResponse{Email: "pending@example.com", Confirmed: false}
	fn, _ := stubWhoamiFn(t, reply, nil)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	if err := whoamiText(context.Background(), cfg, &out, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.String() != "pending@example.com\n" {
		t.Errorf("expected only email + newline; got %q", out.String())
	}
}

func TestWhoamiText_RPCError(t *testing.T) {
	t.Parallel()
	rpcErr := errors.New("unavailable")
	fn, _ := stubWhoamiFn(t, nil, rpcErr)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err := whoamiText(context.Background(), cfg, &out, fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, rpcErr) {
		t.Errorf("expected wrapped rpc error; got %v", err)
	}
	if !strings.Contains(err.Error(), "whoami") {
		t.Errorf("expected 'whoami' context; got %q", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout on error; got %q", out.String())
	}
}
