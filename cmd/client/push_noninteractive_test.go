package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	pb "github.com/fyrash/fyra-cli/proto/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakePushStream satisfies pb.DeployService_PushClient for tests. Only Send
// and CloseAndRecv are exercised by the push code path; the embedded
// grpc.ClientStream satisfies the rest of the interface at compile time.
type fakePushStream struct {
	grpc.ClientStream
	received []*pb.PushRequest
	resp     *pb.PushResponse
	recvErr  error
}

func (s *fakePushStream) Send(req *pb.PushRequest) error {
	s.received = append(s.received, req)
	return nil
}

func (s *fakePushStream) CloseAndRecv() (*pb.PushResponse, error) {
	return s.resp, s.recvErr
}

func TestPushNonInteractive_FirstDeploy_Success(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	if err := os.WriteFile("index.html", []byte("<h1>hello</h1>"), 0644); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	stream := &fakePushStream{
		resp: &pb.PushResponse{Url: "myapp.apps.fyra.sh", FirstDeploy: true},
	}
	openStream := func(ctx context.Context) (pb.DeployService_PushClient, error) {
		return stream, nil
	}
	manifestFetch := func(ctx context.Context, slug, domain string) (map[string]string, error) {
		return nil, nil // first deploy — server has no manifest
	}
	cfg := clientConfig{Token: "tok", ServerAddress: "server.example:443"}

	var out bytes.Buffer
	err := pushNonInteractive(
		context.Background(), cfg, "myapp", "apps.fyra.sh",
		false /* saveAppFile */, nil /* deployConfig */, &out,
		openStream, manifestFetch,
	)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(stream.received) == 0 {
		t.Error("expected stream to receive at least one chunk, got 0")
	}

	outStr := out.String()
	if !strings.Contains(outStr, "Live: https://myapp.apps.fyra.sh") {
		t.Errorf("stdout missing 'Live:' line; got %q", outStr)
	}
}

func TestPushNonInteractive_AlreadyUpToDate(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	if err := os.WriteFile("index.html", []byte("<h1>hello</h1>"), 0644); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	// Server manifest matches the local file exactly — no changes to push.
	localHash, err := sha256File("index.html")
	if err != nil {
		t.Fatalf("hash fixture: %v", err)
	}
	manifestFetch := func(ctx context.Context, slug, domain string) (map[string]string, error) {
		return map[string]string{"index.html": localHash}, nil
	}

	streamOpened := false
	openStream := func(ctx context.Context) (pb.DeployService_PushClient, error) {
		streamOpened = true
		return nil, errors.New("stream must not be opened when up to date")
	}
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err = pushNonInteractive(
		context.Background(), cfg, "myapp", "apps.fyra.sh",
		false, nil, &out, openStream, manifestFetch,
	)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if streamOpened {
		t.Error("expected stream to NOT open when already up to date")
	}

	if !strings.Contains(out.String(), "Already up to date") {
		t.Errorf("stdout missing 'Already up to date'; got %q", out.String())
	}
}

func TestPushNonInteractive_FriendlyUnauthenticated(t *testing.T) {
	// Not parallel: chdirTemp mutates the process-global CWD.
	chdirTemp(t)

	if err := os.WriteFile("index.html", []byte("x"), 0644); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	stream := &fakePushStream{
		recvErr: status.Error(codes.Unauthenticated, "bad token"),
	}
	openStream := func(ctx context.Context) (pb.DeployService_PushClient, error) {
		return stream, nil
	}
	manifestFetch := func(ctx context.Context, slug, domain string) (map[string]string, error) {
		return nil, nil
	}
	cfg := clientConfig{Token: "tok"}

	var out bytes.Buffer
	err := pushNonInteractive(
		context.Background(), cfg, "myapp", "apps.fyra.sh",
		false, nil, &out, openStream, manifestFetch,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("expected friendly 'not logged in' message; got %q", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout on error; got %q", out.String())
	}
}
