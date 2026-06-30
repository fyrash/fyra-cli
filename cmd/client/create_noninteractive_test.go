package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/fyrash/fyra-cli/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stubCreateFn returns a createFn that records the request it received and
// responds with the preset reply/err.
func stubCreateFn(t *testing.T, reply *pb.CreateAppResponse, err error) (createFn, **pb.CreateAppRequest) {
	t.Helper()
	var got *pb.CreateAppRequest
	fn := func(ctx context.Context, req *pb.CreateAppRequest) (*pb.CreateAppResponse, error) {
		got = req
		return reply, err
	}
	return fn, &got
}

// chdirTemp switches into a fresh temp dir for the test so .deploy.yaml writes
// do not pollute the repo. The original dir is restored on cleanup.
func chdirTemp(t *testing.T) string {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return dir
}

func TestCreateNonInteractive_Success(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	dir := chdirTemp(t)

	reply := &pb.CreateAppResponse{
		SlugName:  "myapp",
		Domain:    "apps.fyra.sh",
		CreatedAt: "2026-06-30T00:00:00Z",
	}
	fn, gotPtr := stubCreateFn(t, reply, nil)
	cfg := clientConfig{ServerAddress: "server.example:443", Token: "tok"}

	var out bytes.Buffer
	err := createNonInteractive(context.Background(), cfg, "myapp", "apps.fyra.sh", &out, fn)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	got := *gotPtr
	if got.SlugName != "myapp" {
		t.Errorf("request SlugName = %q, want %q", got.SlugName, "myapp")
	}
	if got.Domain != "apps.fyra.sh" {
		t.Errorf("request Domain = %q, want %q", got.Domain, "apps.fyra.sh")
	}

	outStr := out.String()
	if !strings.Contains(outStr, "Created app: myapp.apps.fyra.sh") {
		t.Errorf("stdout missing success line; got %q", outStr)
	}

	// .deploy.yaml must be written with slug + domain.
	data, err := os.ReadFile(filepath.Join(dir, ".deploy.yaml"))
	if err != nil {
		t.Fatalf("read .deploy.yaml: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "slug: myapp") {
		t.Errorf(".deploy.yaml missing slug; got %q", body)
	}
	if !strings.Contains(body, "domain: apps.fyra.sh") {
		t.Errorf(".deploy.yaml missing domain; got %q", body)
	}
}

func TestCreateNonInteractive_TrimSlug(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	reply := &pb.CreateAppResponse{SlugName: "trimmed", Domain: "apps.fyra.sh"}
	fn, gotPtr := stubCreateFn(t, reply, nil)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	if err := createNonInteractive(context.Background(), cfg, "  padded  ", "apps.fyra.sh", &out, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := *gotPtr; got.SlugName != "padded" {
		t.Errorf("expected trimmed slug %q, got %q", "padded", got.SlugName)
	}
}

func TestCreateNonInteractive_RPCError(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	rpcErr := status.Error(codes.AlreadyExists, "slug taken")
	fn, _ := stubCreateFn(t, nil, rpcErr)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err := createNonInteractive(context.Background(), cfg, "myapp", "apps.fyra.sh", &out, fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, rpcErr) {
		t.Errorf("expected error to wrap rpc error; got %v", err)
	}
	if !strings.Contains(err.Error(), "create app") {
		t.Errorf("expected 'create app' context in error; got %q", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout on error; got %q", out.String())
	}
}
