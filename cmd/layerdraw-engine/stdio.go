// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

func serveStdio(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	compiler := engine.New(engine.BuildInfo{ReleaseVersion: releaseVersion, SourceRevision: sourceRevision})
	descriptor, err := endpoint.NewCompilerDescriptor(
		compiler,
		releaseManifestDigest,
		endpointInstanceID,
		[]string{transport.TransportID},
		endpoint.DefaultLimitPolicy(),
	)
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	return transport.Serve(ctx, stdin, stdout, transport.SessionConfig{
		Descriptor: descriptor,
		Dispatcher: endpoint.NewCompileDispatcher(compiler),
		Limits:     transport.DefaultSessionLimits(),
	})
}

func safeStdioError(err error) string {
	var sessionError *transport.SessionError
	if !errorsAs(err, &sessionError) {
		return "layerdraw-engine: stdio_invariant"
	}
	return fmt.Sprintf("layerdraw-engine: stdio_%s", sessionError.Code)
}

// Kept as a tiny indirection so operational error rendering has one auditable
// path and can never fall back to an underlying error string.
func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}
