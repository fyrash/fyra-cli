package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	pb "github.com/fyrash/fyra-cli/proto/gen"
	"google.golang.org/grpc"
  "google.golang.org/grpc/credentials"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// authContext returns ctx enriched with a Bearer token for gRPC calls.
func authContext(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// dial opens a gRPC connection to the configured server and returns a
// DeployServiceClient plus a cleanup function. The cleanup function must be
// called when the client is no longer needed (typically via defer).
func (cfg clientConfig) dial() (pb.DeployServiceClient, func(), error) {
	cc := credentials.NewTLS(&tls.Config{})

	conn, err := grpc.NewClient(cfg.ServerAddress, grpc.WithTransportCredentials(cc))
	if err != nil {
		return nil, nil, fmt.Errorf("connect to server: %w", err)
	}
	return pb.NewDeployServiceClient(conn), func() { conn.Close() }, nil
}

// isEmailNotConfirmed checks if a gRPC error is the "email not confirmed" response.
func isEmailNotConfirmed(err error) bool {
	return status.Code(err) == codes.PermissionDenied && strings.Contains(err.Error(), "email not confirmed")
}
