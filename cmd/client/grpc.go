package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"runtime"
	"strings"

	pb "github.com/fyrash/fyra-cli/proto/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// appendClientMetadata adds CLI version and platform metadata to the context.
func appendClientMetadata(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		"x-cli-version", version,
		"x-cli-os", runtime.GOOS,
		"x-cli-arch", runtime.GOARCH,
	)
}

// unaryClientMetadata is a unary interceptor that attaches client metadata.
func unaryClientMetadata(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx = appendClientMetadata(ctx)
	return invoker(ctx, method, req, reply, cc, opts...)
}

// streamClientMetadata is a stream interceptor that attaches client metadata.
func streamClientMetadata(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx = appendClientMetadata(ctx)
	return streamer(ctx, desc, cc, method, opts...)
}

// authContext returns ctx enriched with a Bearer token for gRPC calls.
func authContext(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// dial opens a gRPC connection to the configured server and returns a
// DeployServiceClient plus a cleanup function. The cleanup function must be
// called when the client is no longer needed (typically via defer).
func (cfg clientConfig) dial() (pb.DeployServiceClient, func(), error) {
	cc := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})

	conn, err := grpc.NewClient(cfg.ServerAddress,
		grpc.WithTransportCredentials(cc),
		grpc.WithUnaryInterceptor(unaryClientMetadata),
		grpc.WithStreamInterceptor(streamClientMetadata),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to server: %w", err)
	}
	return pb.NewDeployServiceClient(conn), func() { conn.Close() }, nil
}

// isEmailNotConfirmed checks if a gRPC error is the "email not confirmed" response.
func isEmailNotConfirmed(err error) bool {
	return status.Code(err) == codes.PermissionDenied && strings.Contains(err.Error(), "email not confirmed")
}
