// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Command desktopattestation signs and verifies installed Desktop conformance results.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const schemaVersion = 1

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

var scenarioIDs = []string{
	"cold_start", "project_open", "search_analysis", "preview", "commit",
	"viewer_interaction", "mcp_bounded_operations", "external_reconcile", "shutdown",
}

var expectedEvidence = map[string]string{
	"cold_start": "desktop.lifecycle.cold_start", "project_open": "desktop.project.open_save_restart",
	"search_analysis": "desktop.search.query_analysis", "preview": "desktop.preview",
	"commit": "desktop.commit_durable", "viewer_interaction": "desktop.viewer.2d_3d_interaction",
	"mcp_bounded_operations": "desktop.mcp.bounded_operations", "external_reconcile": "desktop.external.reconcile",
	"shutdown": "desktop.lifecycle.shutdown",
}

type digestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type samples struct {
	Milliseconds []int64 `json:"samples_milliseconds"`
}

type scenarioResult struct {
	SchemaVersion  uint32             `json:"schema_version"`
	SourceRevision string             `json:"source_revision"`
	Platform       string             `json:"platform"`
	ArtifactKind   string             `json:"artifact_kind"`
	Iterations     int                `json:"iterations"`
	Scenarios      map[string]samples `json:"scenarios"`
	PeakRSSMiB     []int64            `json:"process_tree_peak_rss_mebibytes"`
	Evidence       map[string]string  `json:"scenario_evidence"`
}

type budget struct {
	MaxMilliseconds int64 `json:"max_milliseconds,omitempty"`
	MaxMebibytes    int64 `json:"max_mebibytes,omitempty"`
	MinIterations   int   `json:"min_iterations"`
	Percentile      int   `json:"percentile"`
}

type closure struct {
	SchemaVersion      uint32            `json:"schema_version"`
	Delivery           string            `json:"delivery"`
	NormativeMatrix    string            `json:"normative_matrix,omitempty"`
	Features           json.RawMessage   `json:"features,omitempty"`
	AcceptanceSuites   json.RawMessage   `json:"acceptance_suites,omitempty"`
	Faults             json.RawMessage   `json:"faults,omitempty"`
	ReleaseEvidence    []string          `json:"release_evidence,omitempty"`
	PerformanceBudgets map[string]budget `json:"performance_budgets"`
}

type signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Value     string `json:"value"`
}

type attestation struct {
	SchemaVersion  uint32     `json:"schema_version"`
	SourceRevision string     `json:"source_revision"`
	Platform       string     `json:"platform"`
	Installer      digestFile `json:"installer"`
	Closure        digestFile `json:"desktop_conformance"`
	ScenarioResult digestFile `json:"scenario_result"`
	SigningMode    string     `json:"signing_mode"`
	Signature      signature  `json:"signature"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "desktopattestation:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected create or verify")
	}
	switch args[0] {
	case "create":
		return createCommand(args[1:])
	case "verify":
		return verifyCommand(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func createCommand(args []string) error {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	installer := flags.String("installer", "", "installed artifact")
	closurePath := flags.String("closure", "", "Desktop closure manifest")
	resultPath := flags.String("scenario-result", "", "installed scenario measurements")
	output := flags.String("output", "", "signed attestation output")
	revision := flags.String("source-revision", "", "exact source commit")
	platform := flags.String("platform", "", "darwin, windows, or linux")
	keyEnv := flags.String("signing-key-env", "LAYERDRAW_DESKTOP_ATTESTATION_SIGNING_KEY", "base64 Ed25519 private key environment variable")
	testSigning := flags.Bool("test-signing", false, "use an ephemeral CI key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *installer == "" || *closurePath == "" || *resultPath == "" || *output == "" || !revisionPattern.MatchString(*revision) || !validPlatform(*platform) {
		return errors.New("create requires installer, closure, scenario-result, output, valid source-revision, and platform")
	}
	store, err := openArtifactStore(filepath.Dir(*output))
	if err != nil {
		return err
	}
	defer store.Close()
	installerName, err := store.directName(*installer)
	if err != nil {
		return fmt.Errorf("installer path: %w", err)
	}
	closureName, err := store.directName(*closurePath)
	if err != nil {
		return fmt.Errorf("closure path: %w", err)
	}
	resultName, err := store.directName(*resultPath)
	if err != nil {
		return fmt.Errorf("scenario result path: %w", err)
	}
	outputName, err := store.directName(*output)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	if err := validateResult(store, closureName, resultName, *revision, *platform); err != nil {
		return err
	}
	privateKey, mode, err := signingKey(*keyEnv, *testSigning)
	if err != nil {
		return err
	}
	value := attestation{SchemaVersion: schemaVersion, SourceRevision: *revision, Platform: *platform, SigningMode: mode}
	for name, target := range map[string]*digestFile{installerName: &value.Installer, closureName: &value.Closure, resultName: &value.ScenarioResult} {
		described, describeErr := describeFile(store, name)
		if describeErr != nil {
			return describeErr
		}
		*target = described
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyDigest := sha256.Sum256(publicKey)
	value.Signature = signature{Algorithm: "Ed25519", KeyID: hex.EncodeToString(keyDigest[:8]), PublicKey: base64.StdEncoding.EncodeToString(publicKey)}
	payload, err := signingPayload(value)
	if err != nil {
		return err
	}
	value.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeExclusive(store, outputName, append(encoded, '\n'))
}

func verifyCommand(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("attestation", "", "signed attestation")
	root := flags.String("root", ".", "artifact root")
	trusted := flags.String("trusted-public-key", "", "base64 trusted Ed25519 key")
	expectedRevision := flags.String("source-revision", "", "expected exact source commit")
	expectedPlatform := flags.String("platform", "", "expected platform")
	allowTest := flags.Bool("allow-test-signing", false, "accept embedded ephemeral CI key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	store, err := openArtifactStore(*root)
	if err != nil {
		return err
	}
	defer store.Close()
	attestationName, err := store.directName(*path)
	if err != nil {
		return errors.New("attestation path is invalid")
	}
	var value attestation
	if *path == "" || !revisionPattern.MatchString(*expectedRevision) || !validPlatform(*expectedPlatform) || decodeStrict(store, attestationName, &value) != nil || value.SchemaVersion != schemaVersion || value.SourceRevision != *expectedRevision || value.Platform != *expectedPlatform {
		return errors.New("attestation is invalid")
	}
	publicText := *trusted
	switch value.SigningMode {
	case "test":
		if !*allowTest {
			return errors.New("test attestation requires explicit opt-in")
		}
		publicText = value.Signature.PublicKey
	case "release":
		if publicText == "" {
			return errors.New("release attestation requires a trusted key")
		}
	default:
		return errors.New("attestation signing mode is invalid")
	}
	publicKey, err := base64.StdEncoding.DecodeString(publicText)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || value.Signature.Algorithm != "Ed25519" || value.Signature.PublicKey != publicText {
		return errors.New("trusted attestation identity is invalid")
	}
	keyDigest := sha256.Sum256(publicKey)
	if value.Signature.KeyID != hex.EncodeToString(keyDigest[:8]) {
		return errors.New("attestation key ID is invalid")
	}
	signed, err := base64.StdEncoding.DecodeString(value.Signature.Value)
	payload, payloadErr := signingPayload(value)
	if err != nil || payloadErr != nil || !ed25519.Verify(publicKey, payload, signed) {
		return errors.New("attestation signature verification failed")
	}
	for _, file := range []digestFile{value.Installer, value.Closure, value.ScenarioResult} {
		if filepath.Base(file.Path) != file.Path {
			return errors.New("attested artifact path is invalid")
		}
		actual, describeErr := describeFile(store, file.Path)
		if describeErr != nil || actual.Size != file.Size || actual.SHA256 != file.SHA256 {
			return fmt.Errorf("attested artifact digest mismatch for %s", file.Path)
		}
	}
	return validateResult(store, value.Closure.Path, value.ScenarioResult.Path, value.SourceRevision, value.Platform)
}

func validateResult(store *artifactStore, closurePath, resultPath, revision, platform string) error {
	var limits closure
	if err := decodeStrict(store, closurePath, &limits); err != nil || limits.SchemaVersion != schemaVersion || limits.Delivery != "desktop" {
		return errors.New("Desktop closure budgets are invalid")
	}
	var result scenarioResult
	if err := decodeStrict(store, resultPath, &result); err != nil {
		return fmt.Errorf("scenario result: %w", err)
	}
	if result.SchemaVersion != schemaVersion || result.SourceRevision != revision || result.Platform != platform || result.ArtifactKind != "installed_desktop" || result.Iterations < 5 {
		return errors.New("scenario result identity is invalid")
	}
	if !sameKeys(result.Scenarios, scenarioIDs) || !sameKeys(result.Evidence, scenarioIDs) {
		return errors.New("scenario result coverage is incomplete")
	}
	for _, id := range scenarioIDs {
		measurement := result.Scenarios[id].Milliseconds
		limit := limits.PerformanceBudgets[id]
		if result.Evidence[id] != expectedEvidence[id] || len(measurement) != result.Iterations || limit.MinIterations < 5 || len(measurement) < limit.MinIterations || limit.Percentile != 95 {
			return fmt.Errorf("scenario %s evidence is invalid", id)
		}
		observed, err := percentile95(measurement)
		if err != nil || observed > limit.MaxMilliseconds {
			return fmt.Errorf("scenario %s p95 exceeds %dms", id, limit.MaxMilliseconds)
		}
	}
	memory := limits.PerformanceBudgets["memory"]
	if len(result.PeakRSSMiB) != result.Iterations || len(result.PeakRSSMiB) < memory.MinIterations || memory.Percentile != 95 {
		return errors.New("process-tree RSS evidence is invalid")
	}
	observed, err := percentile95(result.PeakRSSMiB)
	if err != nil || observed > memory.MaxMebibytes {
		return fmt.Errorf("process-tree p95 RSS exceeds %dMiB", memory.MaxMebibytes)
	}
	return nil
}

func percentile95(values []int64) (int64, error) {
	copy := append([]int64(nil), values...)
	for _, value := range copy {
		if value <= 0 {
			return 0, errors.New("measurements must be positive")
		}
	}
	if len(copy) == 0 {
		return 0, errors.New("measurements are empty")
	}
	slices.Sort(copy)
	index := (95*len(copy)+99)/100 - 1
	return copy[index], nil
}

func sameKeys[T any](values map[string]T, expected []string) bool {
	if len(values) != len(expected) {
		return false
	}
	for _, key := range expected {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

func validPlatform(value string) bool {
	return value == "darwin" || value == "windows" || value == "linux"
}

func signingKey(envName string, test bool) (ed25519.PrivateKey, string, error) {
	if test {
		if os.Getenv(envName) != "" {
			return nil, "", errors.New("test signing refuses a configured release key")
		}
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		return privateKey, "test", err
	}
	raw := os.Getenv(envName)
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if raw == "" || err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, "", errors.New("release attestation signing key is required")
	}
	return ed25519.PrivateKey(decoded), "release", nil
}

func signingPayload(value attestation) ([]byte, error) {
	value.Signature.Value = ""
	return json.Marshal(value)
}

func describeFile(store *artifactStore, name string) (digestFile, error) {
	name, err := store.directName(name)
	if err != nil {
		return digestFile{}, err
	}
	linkInfo, err := store.root.Lstat(name)
	if err != nil || linkInfo.Mode()&os.ModeSymlink != 0 {
		return digestFile{}, errors.New("attested artifact must not be a symbolic link")
	}
	data, err := store.root.ReadFile(name)
	if err != nil {
		return digestFile{}, err
	}
	info, err := store.root.Stat(name)
	if err != nil || !info.Mode().IsRegular() {
		return digestFile{}, errors.New("attested artifact is not a regular file")
	}
	digest := sha256.Sum256(data)
	return digestFile{Path: name, Size: info.Size(), SHA256: hex.EncodeToString(digest[:])}, nil
}

func writeExclusive(store *artifactStore, name string, data []byte) error {
	name, err := store.directName(name)
	if err != nil {
		return err
	}
	file, err := store.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func decodeStrict(store *artifactStore, name string, target any) error {
	name, err := store.directName(name)
	if err != nil {
		return err
	}
	info, err := store.root.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("JSON input must not be a symbolic link")
	}
	file, err := store.root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON content")
	}
	return nil
}

type artifactStore struct {
	root     *os.Root
	absolute string
}

func openArtifactStore(path string) (*artifactStore, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	return &artifactStore{root: root, absolute: absolute}, nil
}

func (store *artifactStore) Close() error { return store.root.Close() }

func (store *artifactStore) directName(path string) (string, error) {
	if path == "" || (filepath.IsAbs(path) && filepath.VolumeName(path) != filepath.VolumeName(store.absolute)) {
		return "", errors.New("artifact path has an invalid volume")
	}
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(store.absolute, absolute)
	}
	absolute = filepath.Clean(absolute)
	relative, err := filepath.Rel(store.absolute, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) || filepath.Dir(relative) != "." {
		return "", errors.New("artifact path escapes its root or is not a direct child")
	}
	return relative, nil
}
