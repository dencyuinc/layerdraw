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
	"os/signal"
	"path/filepath"

	"github.com/dencyuinc/layerdraw/internal/engine"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	hostendpoint "github.com/dencyuinc/layerdraw/internal/host"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

func main() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, processSignals()...)
	defer signal.Stop(signals)
	os.Exit(runIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, signals))
}

func runIO(args []string, stdin io.Reader, stdout, stderr io.Writer, signals <-chan os.Signal) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Fprintf(stdout, "layerdraw-host %s (%s)\n", releaseVersion, sourceRevision)
		return 0
	}
	if len(args) != 3 || (args[0] != "stdio" && args[0] != "engine-stdio") || args[1] != "--root" || args[2] == "" || stdin == nil {
		fmt.Fprintln(stderr, "usage: layerdraw-host --version|version|stdio|engine-stdio --root ABSOLUTE_PATH")
		return 2
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan os.Signal, 1)
	if signals != nil {
		go func() {
			select {
			case current := <-signals:
				received <- current
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	var err error
	if args[0] == "engine-stdio" {
		err = serveEngine(ctx, stdin, stdout)
	} else {
		err = serve(ctx, args[2], stdin, stdout)
	}
	select {
	case current := <-received:
		return signalExitCode(current)
	default:
	}
	if err != nil {
		fmt.Fprintln(stderr, safeError(err))
		return 1
	}
	return 0
}

func serveEngine(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	compiler := engine.New(engine.BuildInfo{ReleaseVersion: releaseVersion, SourceRevision: sourceRevision})
	digest, err := manifestDigest()
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	instanceID, err := endpointInstanceID()
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	descriptor, err := engineendpoint.NewCompilerDescriptor(compiler, digest, instanceID, []string{transport.TransportID}, engineendpoint.DefaultLimitPolicy())
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	return transport.Serve(ctx, stdin, stdout, transport.SessionConfig{Descriptor: descriptor, Dispatcher: engineendpoint.NewCompileDispatcher(compiler), Limits: transport.DefaultSessionLimits()})
}

func serve(ctx context.Context, root string, stdin io.Reader, stdout io.Writer) error {
	if !filepath.IsAbs(root) {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	digest, err := manifestDigest()
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	instanceID, err := endpointInstanceID()
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	endpoint, shutdown, err := hostendpoint.NewLocal(hostendpoint.LocalConfig{
		Root: root, ReleaseVersion: releaseVersion, SourceRevision: sourceRevision,
		ReleaseManifestDigest: digest, EndpointInstanceID: instanceID, TransportID: transport.TransportID,
	})
	if err != nil {
		return &transport.SessionError{Code: transport.SessionErrorConfiguration}
	}
	defer shutdown(context.WithoutCancel(ctx))
	return transport.ServePortable(ctx, stdin, stdout, endpoint, transport.DefaultSessionLimits())
}

func endpointInstanceID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "host-" + hex.EncodeToString(bytes[:]), nil
}

func manifestDigest() (string, error) {
	if releaseManifestDigest != "" {
		return releaseManifestDigest, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(executable), "layerdraw-release-manifest.json"))
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func safeError(err error) string {
	var session *transport.SessionError
	if errors.As(err, &session) {
		return "layerdraw-host: stdio_" + session.Code
	}
	return "layerdraw-host: stdio_invariant"
}
