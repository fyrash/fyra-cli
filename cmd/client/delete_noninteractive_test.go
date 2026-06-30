package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	pb "github.com/fyrash/fyra-cli/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func stubDeleteFn(t *testing.T, reply *pb.DeleteAppResponse, err error) (deleteFn, **pb.DeleteAppRequest) {
	t.Helper()
	var got *pb.DeleteAppRequest
	fn := func(ctx context.Context, req *pb.DeleteAppRequest) (*pb.DeleteAppResponse, error) {
		got = req
		return reply, err
	}
	return fn, &got
}

func TestDeleteNonInteractive_Success(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	reply := &pb.DeleteAppResponse{SlugName: "myapp"}
	fn, gotPtr := stubDeleteFn(t, reply, nil)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err := deleteNonInteractive(context.Background(), cfg, "myapp", "apps.fyra.sh", &out, fn)
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

	if !strings.Contains(out.String(), "App myapp deleted.") {
		t.Errorf("stdout missing success line; got %q", out.String())
	}
}

func TestDeleteNonInteractive_FriendlyNotFound(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	rpcErr := status.Error(codes.NotFound, "no such app")
	fn, _ := stubDeleteFn(t, nil, rpcErr)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err := deleteNonInteractive(context.Background(), cfg, "ghost", "apps.fyra.sh", &out, fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// friendlyDeleteError rewrites NotFound to a user hint.
	if !strings.Contains(err.Error(), "app not found") {
		t.Errorf("expected friendly 'app not found' message; got %q", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout on error; got %q", out.String())
	}
}

func TestDeleteNonInteractive_FriendlyUnauthenticated(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	rpcErr := status.Error(codes.Unauthenticated, "bad token")
	fn, _ := stubDeleteFn(t, nil, rpcErr)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err := deleteNonInteractive(context.Background(), cfg, "myapp", "apps.fyra.sh", &out, fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("expected friendly 'not logged in' message; got %q", err.Error())
	}
}

func TestDeleteNonInteractive_OtherError(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	rpcErr := status.Error(codes.Internal, "boom")
	fn, _ := stubDeleteFn(t, nil, rpcErr)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err := deleteNonInteractive(context.Background(), cfg, "myapp", "apps.fyra.sh", &out, fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, rpcErr) {
		t.Errorf("expected underlying rpc error wrapped; got %v", err)
	}
}

func TestDeleteNonInteractive_RemovesDeployYaml(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	dir := chdirTemp(t)

	// Pre-create a .deploy.yaml that the non-interactive delete should sweep.
	if err := os.WriteFile(dir+"/.deploy.yaml", []byte("slug: myapp\ndomain: apps.fyra.sh\n"), 0644); err != nil {
		t.Fatalf("seed .deploy.yaml: %v", err)
	}

	reply := &pb.DeleteAppResponse{SlugName: "myapp"}
	fn, _ := stubDeleteFn(t, reply, nil)
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	if err := deleteNonInteractive(context.Background(), cfg, "myapp", "apps.fyra.sh", &out, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dir + "/.deploy.yaml"); !os.IsNotExist(err) {
		t.Errorf("expected .deploy.yaml removed after delete; stat err=%v", err)
	}
}
