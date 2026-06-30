package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	pb "github.com/fyrash/fyra-cli/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stubLoginFn returns a loginFn that records the inbound request and replies
// with the preset reply/err. Mirrors the stub pattern in delete/create tests.
func stubLoginFn(t *testing.T, reply *pb.LoginResponse, err error) (loginFn, **pb.LoginRequest) {
	t.Helper()
	var got *pb.LoginRequest
	fn := func(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
		got = req
		return reply, err
	}
	return fn, &got
}

func TestLoginNonInteractive_Success(t *testing.T) {
	reply := &pb.LoginResponse{Token: "tok_abc"}
	fn, gotPtr := stubLoginFn(t, reply, nil)

	token, err := loginNonInteractive(context.Background(), "user@example.com", "password", fn)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if token != "tok_abc" {
		t.Errorf("token = %q, want %q", token, "tok_abc")
	}

	got := *gotPtr
	if got.Email != "user@example.com" {
		t.Errorf("request Email = %q, want %q", got.Email, "user@example.com")
	}
	if got.Password != "password" {
		t.Errorf("request Password = %q, want %q", got.Password, "password")
	}
}

func TestLoginNonInteractive_InvalidEmail(t *testing.T) {
	fn, _ := stubLoginFn(t, nil, nil)

	_, err := loginNonInteractive(context.Background(), "not-an-email", "password", fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid email") {
		t.Errorf("expected 'invalid email' error; got %q", err.Error())
	}
}

func TestLoginNonInteractive_EmptyPassword(t *testing.T) {
	fn, _ := stubLoginFn(t, nil, nil)

	_, err := loginNonInteractive(context.Background(), "user@example.com", "", fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "password required") {
		t.Errorf("expected 'password required' error; got %q", err.Error())
	}
}

func TestLoginNonInteractive_FriendlyUnauthenticated(t *testing.T) {
	rpcErr := status.Error(codes.Unauthenticated, "bad creds")
	fn, _ := stubLoginFn(t, nil, rpcErr)

	_, err := loginNonInteractive(context.Background(), "user@example.com", "wrong", fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid email or password") {
		t.Errorf("expected friendly 'invalid email or password' message; got %q", err.Error())
	}
}

func TestLoginNonInteractive_EmailNotConfirmed(t *testing.T) {
	rpcErr := status.Error(codes.PermissionDenied, "email not confirmed")
	fn, _ := stubLoginFn(t, nil, rpcErr)

	_, err := loginNonInteractive(context.Background(), "user@example.com", "password", fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "confirm your email") {
		t.Errorf("expected friendly 'confirm your email' message; got %q", err.Error())
	}
}

func TestLoginNonInteractive_OtherError(t *testing.T) {
	rpcErr := status.Error(codes.Internal, "boom")
	fn, _ := stubLoginFn(t, nil, rpcErr)

	_, err := loginNonInteractive(context.Background(), "user@example.com", "password", fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, rpcErr) {
		t.Errorf("expected underlying rpc error wrapped; got %v", err)
	}
	if !strings.Contains(err.Error(), "login failed") {
		t.Errorf("expected 'login failed' prefix; got %q", err.Error())
	}
}
