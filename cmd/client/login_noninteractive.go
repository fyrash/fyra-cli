package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/fyrash/fyra-cli/proto/gen"
)

// loginFn is the gRPC call seam for the non-interactive login path. Production
// callers pass client.Login; tests pass a stub — no real network needed.
type loginFn func(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error)

// runLoginNonInteractive dials the server, delegates to the testable core,
// then persists the returned token to ~/.fyra/config.yaml.
func runLoginNonInteractive(ctx context.Context, cfg clientConfig, email, password string, out io.Writer) error {
	client, cleanup, err := cfg.dial()
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer cleanup()

	login := func(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
		return client.Login(ctx, req)
	}

	token, err := loginNonInteractive(ctx, email, password, login)
	if err != nil {
		return err
	}

	cfg.Token = token
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(out, "Logged in as %s\n", email)
	return nil
}

// loginNonInteractive is the testable core of the non-interactive login path.
// It validates inputs, calls the login seam, and maps RPC errors to friendly
// messages. Returns the raw session token on success.
func loginNonInteractive(ctx context.Context, email, password string, login loginFn) (string, error) {
	if !strings.Contains(email, "@") {
		return "", fmt.Errorf("invalid email format")
	}
	if password == "" {
		return "", fmt.Errorf("password required")
	}

	resp, err := login(ctx, &pb.LoginRequest{Email: email, Password: password})
	if err != nil {
		return "", friendlyLoginError(err)
	}
	return resp.Token, nil
}

// friendlyLoginError rewrites well-known gRPC status codes into actionable
// user-facing messages. Unrecognised errors are wrapped with %w so callers
// can still errors.Is them.
func friendlyLoginError(err error) error {
	switch status.Code(err) {
	case codes.Unauthenticated:
		return fmt.Errorf("invalid email or password")
	case codes.PermissionDenied:
		if strings.Contains(err.Error(), "email not confirmed") {
			return fmt.Errorf("please confirm your email first. Run '%s confirm' to resend the code.", binaryName)
		}
		return fmt.Errorf("login failed: %w", err)
	default:
		return fmt.Errorf("login failed: %w", err)
	}
}
