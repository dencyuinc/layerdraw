// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

func serveStdio(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	compiler := engine.New(engine.BuildInfo{ReleaseVersion: releaseVersion, SourceRevision: sourceRevision})
	instanceID, err := newEndpointInstanceID()
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	manifestDigest, err := resolvedReleaseManifestDigest()
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	descriptor, err := endpoint.NewCompilerDescriptor(
		compiler,
		manifestDigest,
		instanceID,
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

func resolvedReleaseManifestDigest() (string, error) {
	if releaseManifestDigest != "" {
		return releaseManifestDigest, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return releaseManifestDigestFromFile(filepath.Join(filepath.Dir(executable), "layerdraw-release-manifest.json"))
}

func releaseManifestDigestFromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func newEndpointInstanceID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return "stdio-" + hex.EncodeToString(entropy[:]), nil
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
