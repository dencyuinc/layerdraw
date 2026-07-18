// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	LayerdrawFormat        = "layerdraw-document"
	LayerdrawFormatVersion = 1
	LayerdrawLanguage      = 1

	LayerdrawErrorInvalidLimits      = "engine.layerdraw.invalid_limits"
	LayerdrawErrorArchiveTooLarge    = "engine.layerdraw.archive_too_large"
	LayerdrawErrorInvalidArchive     = "engine.layerdraw.invalid_archive"
	LayerdrawErrorUnsafeEntry        = "engine.layerdraw.unsafe_entry"
	LayerdrawErrorEntryCountExceeded = "engine.layerdraw.entry_count_exceeded"
	LayerdrawErrorEntrySizeExceeded  = "engine.layerdraw.entry_size_exceeded"
	LayerdrawErrorTotalSizeExceeded  = "engine.layerdraw.total_size_exceeded"
	LayerdrawErrorCompressionRatio   = "engine.layerdraw.compression_ratio_exceeded"
	LayerdrawErrorTruncated          = "engine.layerdraw.truncated_entry"
	LayerdrawErrorManifest           = "engine.layerdraw.invalid_manifest"
	LayerdrawErrorUnsupportedVersion = "engine.layerdraw.unsupported_version"
	LayerdrawErrorDigestMismatch     = "engine.layerdraw.digest_mismatch"
	LayerdrawErrorResolvedMetadata   = "engine.layerdraw.invalid_resolved_metadata"
	LayerdrawErrorSemanticValidation = "engine.layerdraw.semantic_validation_failed"
	LayerdrawErrorDerivedArtifact    = "engine.layerdraw.invalid_derived_artifact"
	LayerdrawErrorStateSnapshot      = "engine.layerdraw.invalid_state_snapshot"
	LayerdrawErrorForbiddenPortable  = "engine.layerdraw.forbidden_portable_content"
	LayerdrawErrorCancelled          = "engine.layerdraw.cancelled"
	LayerdrawErrorInvariant          = "engine.layerdraw.invariant"
)

// LayerdrawError is a stable, path-safe failure from the container facade.
type LayerdrawError struct {
	Code  string
	Entry string
	cause error
}

func (e *LayerdrawError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Entry != "" {
		return e.Code + ": " + e.Entry
	}
	return e.Code
}

func (e *LayerdrawError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// IsLayerdrawError reports whether err has the requested stable code.
func IsLayerdrawError(err error, code string) bool {
	var target *LayerdrawError
	return errors.As(err, &target) && target.Code == code
}

func layerdrawFailure(code, entry string, cause error) error {
	return &LayerdrawError{Code: code, Entry: entry, cause: cause}
}

// LayerdrawLimits bounds archive inspection and decompression. Zero selects a
// safe deterministic default; negative values are invalid.
type LayerdrawLimits struct {
	MaxArchiveBytes     int64
	MaxEntries          int64
	MaxEntryBytes       int64
	MaxTotalBytes       int64
	MaxCompressionRatio int64
	MaxPathBytes        int64
	MaxPathDepth        int64
	MaxManifestBytes    int64
}

func DefaultLayerdrawLimits() LayerdrawLimits {
	return LayerdrawLimits{
		MaxArchiveBytes: 512 << 20, MaxEntries: 16_384, MaxEntryBytes: 256 << 20,
		MaxTotalBytes: 512 << 20, MaxCompressionRatio: 100, MaxPathBytes: 1_024,
		MaxPathDepth: 32, MaxManifestBytes: 1 << 20,
	}
}

func (l LayerdrawLimits) effective() (LayerdrawLimits, bool) {
	d := DefaultLayerdrawLimits()
	values := []*int64{&l.MaxArchiveBytes, &l.MaxEntries, &l.MaxEntryBytes, &l.MaxTotalBytes, &l.MaxCompressionRatio, &l.MaxPathBytes, &l.MaxPathDepth, &l.MaxManifestBytes}
	fallbacks := []int64{d.MaxArchiveBytes, d.MaxEntries, d.MaxEntryBytes, d.MaxTotalBytes, d.MaxCompressionRatio, d.MaxPathBytes, d.MaxPathDepth, d.MaxManifestBytes}
	for i, value := range values {
		if *value < 0 {
			return LayerdrawLimits{}, false
		}
		if *value == 0 {
			*value = fallbacks[i]
		}
	}
	return l, true
}

// LayerdrawEntry is safe central-directory metadata. Directory entries are
// omitted because canonical containers do not emit them.
type LayerdrawEntry struct {
	Name             string
	CompressedSize   uint64
	UncompressedSize uint64
	Method           uint16
}

// LayerdrawInspection never compiles content. Supported is false for a newer
// major while the safe entry listing and declared versions remain available.
type LayerdrawInspection struct {
	Format        string
	FormatVersion int
	Language      int
	Supported     bool
	Entries       []LayerdrawEntry
}

type LayerdrawInspectInput struct {
	Bytes  []byte
	Limits LayerdrawLimits
}

type LayerdrawReadInput struct {
	Bytes  []byte
	Limits LayerdrawLimits
}

// PackageRedaction records a non-secret policy identifier applied before the
// portable package was produced.
type PackageRedaction struct {
	PolicyID string `json:"policy_id"`
}

type LayerdrawManifest struct {
	Format             string            `json:"format"`
	FormatVersion      int               `json:"format_version"`
	Language           int               `json:"language"`
	Entry              string            `json:"entry"`
	ProjectAddress     string            `json:"project_address"`
	DefinitionHash     string            `json:"definition_hash"`
	ResolvedFileDigest string            `json:"resolved_file_digest"`
	Files              map[string]string `json:"files"`
	Redaction          *PackageRedaction `json:"redaction,omitempty"`
}

// LayerdrawDocument is a fully integrity- and semantics-validated portable
// document. All byte maps and snapshots own their storage. Artifacts contains
// opaque derived bytes from previews/ and exports/: the container validates
// their paths, raw digests, bounds, and portable-content safety, but does not
// claim serializer- or profile-specific semantic validation.
type LayerdrawDocument struct {
	Manifest          LayerdrawManifest
	Compilation       Snapshot
	Files             map[string][]byte
	ProjectSourceTree map[string][]byte
	InstalledPackTree map[string][]byte
	StateSnapshots    map[string]StateQuerySnapshot
	Artifacts         map[string][]byte
}

// LayerdrawWriteInput accepts the already-closed compiler input plus optional
// portable state snapshots and opaque derived artifacts. Artifacts are
// restricted to previews/ and exports/ and receive only container path, raw
// digest, bounds, and portable-content safety validation. Secrets are exact
// byte sequences that must not occur anywhere in the output.
type LayerdrawWriteInput struct {
	CompileInput      CompileInput
	StateSnapshots    []StateQuerySnapshot
	Artifacts         map[string][]byte
	RedactionPolicyID string
	Secrets           [][]byte
	Limits            LayerdrawLimits
}

type layerdrawManifestProbe struct {
	Format        string `json:"format"`
	FormatVersion int    `json:"format_version"`
	Language      int    `json:"language"`
}

type scannedLayerdraw struct {
	files   []*zip.File
	entries []LayerdrawEntry
	limits  LayerdrawLimits
}

// InspectLayerdraw safely lists entries and declared versions without
// compiling or decompressing unsupported container content.
func (e Engine) InspectLayerdraw(ctx context.Context, input LayerdrawInspectInput) (LayerdrawInspection, error) {
	scanned, err := scanLayerdraw(ctx, input.Bytes, input.Limits)
	if err != nil {
		return LayerdrawInspection{}, err
	}
	manifestFile := findZipFile(scanned.files, "manifest.json")
	if manifestFile == nil || manifestFile.UncompressedSize64 > uint64(scanned.limits.MaxManifestBytes) {
		return LayerdrawInspection{}, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
	}
	manifestBytes, err := readZipFile(ctx, manifestFile, scanned.limits)
	if err != nil {
		return LayerdrawInspection{}, err
	}
	var probe layerdrawManifestProbe
	if err := decodeJSON(manifestBytes, &probe, false); err != nil || probe.Format == "" || probe.FormatVersion < 1 || probe.Language < 1 {
		return LayerdrawInspection{}, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", err)
	}
	return LayerdrawInspection{
		Format: probe.Format, FormatVersion: probe.FormatVersion, Language: probe.Language,
		Supported: probe.Format == LayerdrawFormat && probe.FormatVersion == LayerdrawFormatVersion && probe.Language == LayerdrawLanguage,
		Entries:   append([]LayerdrawEntry{}, scanned.entries...),
	}, nil
}

// ReadLayerdraw validates archive safety, all raw digests, the exact resolved
// tree, compiler semantics, generated artifacts, and state snapshots.
func (e Engine) ReadLayerdraw(ctx context.Context, input LayerdrawReadInput) (LayerdrawDocument, error) {
	inspection, err := e.InspectLayerdraw(ctx, LayerdrawInspectInput(input))
	if err != nil {
		return LayerdrawDocument{}, err
	}
	if !inspection.Supported {
		return LayerdrawDocument{}, layerdrawFailure(LayerdrawErrorUnsupportedVersion, "manifest.json", nil)
	}
	scanned, err := scanLayerdraw(ctx, input.Bytes, input.Limits)
	if err != nil {
		return LayerdrawDocument{}, err
	}
	files := make(map[string][]byte, len(scanned.files))
	for _, file := range scanned.files {
		if file.FileInfo().IsDir() {
			continue
		}
		value, err := readZipFile(ctx, file, scanned.limits)
		if err != nil {
			return LayerdrawDocument{}, err
		}
		files[file.Name] = value
	}
	var manifest LayerdrawManifest
	if err := decodeJSON(files["manifest.json"], &manifest, true); err != nil {
		return LayerdrawDocument{}, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", err)
	}
	if err := validateLayerdrawManifest(manifest, files); err != nil {
		return LayerdrawDocument{}, err
	}
	if err := validatePortableFiles(files, nil, scanned.limits); err != nil {
		return LayerdrawDocument{}, err
	}
	compileInput, err := compileInputFromContainer(files, manifest.Entry)
	if err != nil {
		return LayerdrawDocument{}, err
	}
	compiled, err := e.Compile(ctx, compileInput)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return LayerdrawDocument{}, layerdrawFailure(LayerdrawErrorCancelled, "", err)
		}
		return LayerdrawDocument{}, layerdrawFailure(LayerdrawErrorSemanticValidation, manifest.Entry, err)
	}
	snapshot := compiled.Snapshot()
	if hasErrorDiagnostics(snapshot.Diagnostics) || snapshot.NormalizedDocument == nil || snapshot.TypedAST.Project == nil {
		return LayerdrawDocument{}, layerdrawFailure(LayerdrawErrorSemanticValidation, manifest.Entry, nil)
	}
	if snapshot.TypedAST.Project.Address != manifest.ProjectAddress || snapshot.DefinitionHash != manifest.DefinitionHash {
		return LayerdrawDocument{}, layerdrawFailure(LayerdrawErrorSemanticValidation, manifest.Entry, nil)
	}
	if document, ok := files["document.json"]; ok && !bytes.Equal(document, snapshot.ArtifactJSON) {
		return LayerdrawDocument{}, layerdrawFailure(LayerdrawErrorDerivedArtifact, "document.json", nil)
	}
	if indexBytes, ok := files["layerdraw.index.json"]; ok {
		expected, err := canonicalIndex(snapshot)
		if err != nil || !bytes.Equal(indexBytes, expected) {
			return LayerdrawDocument{}, layerdrawFailure(LayerdrawErrorDerivedArtifact, "layerdraw.index.json", err)
		}
	}
	stateSnapshots, err := validateContainerState(ctx, snapshot, files, manifest.Redaction)
	if err != nil {
		return LayerdrawDocument{}, err
	}
	artifacts := map[string][]byte{}
	for name, value := range files {
		if strings.HasPrefix(name, "previews/") || strings.HasPrefix(name, "exports/") {
			artifacts[name] = bytes.Clone(value)
		}
	}
	return LayerdrawDocument{
		Manifest: manifest, Compilation: snapshot,
		Files:             cloneByteMap(files),
		ProjectSourceTree: cloneByteMap(compileInput.ProjectSourceTree),
		InstalledPackTree: cloneByteMap(compileInput.InstalledPackTree),
		StateSnapshots:    stateSnapshots, Artifacts: artifacts,
	}, nil
}

// WriteLayerdraw emits a canonical byte-stable ZIP using lexical entry order,
// STORE compression, fixed metadata, canonical JSON, and no directory entries.
func (e Engine) WriteLayerdraw(ctx context.Context, input LayerdrawWriteInput) ([]byte, error) {
	if input.CompileInput.Mode != CompileProject {
		return nil, layerdrawFailure(LayerdrawErrorSemanticValidation, input.CompileInput.EntryPath, nil)
	}
	compiled, err := e.Compile(ctx, input.CompileInput)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, layerdrawFailure(LayerdrawErrorCancelled, "", err)
		}
		return nil, layerdrawFailure(LayerdrawErrorSemanticValidation, input.CompileInput.EntryPath, err)
	}
	snapshot := compiled.Snapshot()
	if hasErrorDiagnostics(snapshot.Diagnostics) || snapshot.NormalizedDocument == nil || snapshot.TypedAST.Project == nil {
		return nil, layerdrawFailure(LayerdrawErrorSemanticValidation, input.CompileInput.EntryPath, nil)
	}
	files := cloneByteMap(input.CompileInput.ProjectSourceTree)
	for name, value := range input.CompileInput.InstalledPackTree {
		if _, exists := files[name]; exists {
			return nil, layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
		}
		files[name] = bytes.Clone(value)
	}
	resolvedBytes, err := canonicalResolved(input.CompileInput.ResolvedDependencies)
	if err != nil {
		return nil, layerdrawFailure(LayerdrawErrorResolvedMetadata, "layerdraw.resolved.json", err)
	}
	files["layerdraw.resolved.json"] = resolvedBytes
	for _, pack := range input.CompileInput.ResolvedDependencies.Installs {
		manifestPath := pack.ManifestPath
		if manifestPath == "" {
			manifestPath = "manifest.json"
		}
		fullPath := pack.Path + "/" + manifestPath
		if existing, ok := files[fullPath]; ok && !bytes.Equal(existing, pack.Manifest) {
			return nil, layerdrawFailure(LayerdrawErrorResolvedMetadata, fullPath, nil)
		}
		canonical, err := canonicalJSONBytes(pack.Manifest)
		if err != nil || !bytes.Equal(canonical, pack.Manifest) {
			return nil, layerdrawFailure(LayerdrawErrorResolvedMetadata, fullPath, err)
		}
		files[fullPath] = bytes.Clone(pack.Manifest)
	}
	for _, asset := range input.CompileInput.ReferencedAssets {
		if asset.Origin != SourceOriginProject {
			continue
		}
		if existing, ok := files[asset.Locator]; ok && !bytes.Equal(existing, asset.Bytes) {
			return nil, layerdrawFailure(LayerdrawErrorDigestMismatch, asset.Locator, nil)
		}
		files[asset.Locator] = bytes.Clone(asset.Bytes)
	}
	files["document.json"] = bytes.Clone(snapshot.ArtifactJSON)
	indexBytes, err := canonicalIndex(snapshot)
	if err != nil {
		return nil, layerdrawFailure(LayerdrawErrorInvariant, "layerdraw.index.json", err)
	}
	files["layerdraw.index.json"] = indexBytes
	redacted := false
	for _, state := range input.StateSnapshots {
		stateBytes, stateHash, hasRedaction, err := canonicalStateSnapshot(ctx, snapshot, state)
		if err != nil {
			return nil, err
		}
		redacted = redacted || hasRedaction
		files["state/query-snapshots/"+strings.TrimPrefix(stateHash, "sha256:")+".json"] = stateBytes
	}
	for name, value := range input.Artifacts {
		if !strings.HasPrefix(name, "previews/") && !strings.HasPrefix(name, "exports/") {
			return nil, layerdrawFailure(LayerdrawErrorForbiddenPortable, name, nil)
		}
		if _, exists := files[name]; exists {
			return nil, layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
		}
		files[name] = bytes.Clone(value)
	}
	// Apply the same closed entry classification as the reader so the canonical
	// writer cannot emit bytes that a conforming reader must reject.
	if _, err := compileInputFromContainer(files, input.CompileInput.EntryPath); err != nil {
		return nil, err
	}
	if redacted && input.RedactionPolicyID == "" {
		return nil, layerdrawFailure(LayerdrawErrorForbiddenPortable, "manifest.json", nil)
	}
	if input.RedactionPolicyID != "" && (norm.NFC.String(input.RedactionPolicyID) != input.RedactionPolicyID || !utf8.ValidString(input.RedactionPolicyID)) {
		return nil, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
	}
	limits, ok := input.Limits.effective()
	if !ok {
		return nil, layerdrawFailure(LayerdrawErrorInvalidLimits, "", nil)
	}
	fileDigests := make(map[string]string, len(files))
	for name, value := range files {
		fileDigests[name] = rawDigest(value)
	}
	manifest := LayerdrawManifest{
		Format: LayerdrawFormat, FormatVersion: LayerdrawFormatVersion, Language: LayerdrawLanguage,
		Entry: input.CompileInput.EntryPath, ProjectAddress: snapshot.TypedAST.Project.Address,
		DefinitionHash: snapshot.DefinitionHash, ResolvedFileDigest: rawDigest(resolvedBytes), Files: fileDigests,
	}
	if input.RedactionPolicyID != "" {
		manifest.Redaction = &PackageRedaction{PolicyID: input.RedactionPolicyID}
	}
	manifestBytes, err := canonicalArtifact(manifest)
	if err != nil {
		return nil, layerdrawFailure(LayerdrawErrorInvariant, "manifest.json", err)
	}
	if int64(len(manifestBytes)) > limits.MaxManifestBytes || int64(len(manifestBytes)) > limits.MaxEntryBytes {
		return nil, layerdrawFailure(LayerdrawErrorEntrySizeExceeded, "manifest.json", nil)
	}
	files["manifest.json"] = manifestBytes
	if err := validatePortableFiles(files, input.Secrets, limits); err != nil {
		return nil, err
	}
	archive, err := writeCanonicalZip(ctx, files)
	if err != nil {
		return nil, err
	}
	if int64(len(archive)) > limits.MaxArchiveBytes {
		return nil, layerdrawFailure(LayerdrawErrorArchiveTooLarge, "", nil)
	}
	return archive, nil
}

func scanLayerdraw(ctx context.Context, data []byte, requested LayerdrawLimits) (scannedLayerdraw, error) {
	limits, ok := requested.effective()
	if !ok {
		return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorInvalidLimits, "", nil)
	}
	if int64(len(data)) > limits.MaxArchiveBytes {
		return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorArchiveTooLarge, "", nil)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorInvalidArchive, "", err)
	}
	if int64(len(reader.File)) > limits.MaxEntries {
		return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorEntryCountExceeded, "", nil)
	}
	seen := map[string]string{}
	allNames := map[string]bool{}
	entries := make([]LayerdrawEntry, 0, len(reader.File))
	var total uint64
	for _, file := range reader.File {
		if err := layerdrawContext(ctx); err != nil {
			return scannedLayerdraw{}, err
		}
		if err := validateContainerPath(file.Name, limits); err != nil {
			return scannedLayerdraw{}, err
		}
		collisionKey := cases.Fold().String(norm.NFC.String(strings.TrimSuffix(file.Name, "/")))
		if prior, duplicate := seen[collisionKey]; duplicate {
			return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorUnsafeEntry, prior+"|"+file.Name, nil)
		}
		seen[collisionKey] = file.Name
		allNames[file.Name] = true
		mode := file.Mode()
		if !mode.IsRegular() && !mode.IsDir() {
			return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorUnsafeEntry, file.Name, nil)
		}
		if mode.IsDir() {
			if !strings.HasSuffix(file.Name, "/") || file.UncompressedSize64 != 0 {
				return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorUnsafeEntry, file.Name, nil)
			}
			continue
		}
		if file.Flags&1 != 0 || (file.Method != zip.Store && file.Method != zip.Deflate) {
			return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorUnsafeEntry, file.Name, nil)
		}
		if file.UncompressedSize64 > uint64(limits.MaxEntryBytes) {
			return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorEntrySizeExceeded, file.Name, nil)
		}
		if total > math.MaxUint64-file.UncompressedSize64 {
			return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorTotalSizeExceeded, file.Name, nil)
		}
		total += file.UncompressedSize64
		if total > uint64(limits.MaxTotalBytes) {
			return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorTotalSizeExceeded, file.Name, nil)
		}
		if compressionRatioExceeded(file.UncompressedSize64, file.CompressedSize64, limits.MaxCompressionRatio) {
			return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorCompressionRatio, file.Name, nil)
		}
		entries = append(entries, LayerdrawEntry{Name: file.Name, CompressedSize: file.CompressedSize64, UncompressedSize: file.UncompressedSize64, Method: file.Method})
	}
	for name := range allNames {
		if !strings.HasSuffix(name, "/") {
			continue
		}
		if allNames[strings.TrimSuffix(name, "/")] {
			return scannedLayerdraw{}, layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return scannedLayerdraw{files: reader.File, entries: entries, limits: limits}, nil
}

func compressionRatioExceeded(uncompressed, compressed uint64, maxRatio int64) bool {
	if compressed == 0 {
		compressed = 1
	}
	if maxRatio <= 0 {
		return uncompressed != 0
	}
	ratio := uint64(maxRatio)
	quotient := uncompressed / compressed
	return quotient > ratio || (quotient == ratio && uncompressed%compressed != 0)
}

func validateContainerPath(name string, limits LayerdrawLimits) error {
	if name == "" || !utf8.ValidString(name) || len(name) > int(limits.MaxPathBytes) || norm.NFC.String(name) != name || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) {
		return layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || path.IsAbs(trimmed) || path.Clean(trimmed) != trimmed || strings.Count(trimmed, "/")+1 > int(limits.MaxPathDepth) {
		return layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
	}
	first := strings.Split(trimmed, "/")[0]
	if strings.HasSuffix(first, ":") || (len(first) >= 2 && first[1] == ':') {
		return layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
		}
		for _, character := range segment {
			if unicode.IsControl(character) {
				return layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
			}
		}
	}
	decoded, err := url.PathUnescape(trimmed)
	if err != nil || strings.Contains(decoded, "\\") || strings.ContainsRune(decoded, 0) || strings.HasPrefix(decoded, "/") || path.Clean(decoded) != decoded {
		return layerdrawFailure(LayerdrawErrorUnsafeEntry, name, err)
	}
	return nil
}

func readZipFile(ctx context.Context, file *zip.File, limits LayerdrawLimits) ([]byte, error) {
	if err := layerdrawContext(ctx); err != nil {
		return nil, err
	}
	if file.UncompressedSize64 > uint64(limits.MaxEntryBytes) || file.UncompressedSize64 > uint64(math.MaxInt64-1) {
		return nil, layerdrawFailure(LayerdrawErrorEntrySizeExceeded, file.Name, nil)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, layerdrawFailure(LayerdrawErrorTruncated, file.Name, err)
	}
	limited := io.LimitReader(reader, int64(file.UncompressedSize64)+1)
	value, readErr := io.ReadAll(&contextReader{ctx: ctx, reader: limited})
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || uint64(len(value)) != file.UncompressedSize64 {
		cause := errors.Join(readErr, closeErr)
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			return nil, layerdrawFailure(LayerdrawErrorCancelled, file.Name, cause)
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, layerdrawFailure(LayerdrawErrorCancelled, file.Name, contextErr)
		}
		return nil, layerdrawFailure(LayerdrawErrorTruncated, file.Name, cause)
	}
	return value, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(value []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(value)
}

func findZipFile(files []*zip.File, name string) *zip.File {
	for _, file := range files {
		if file.Name == name && !file.FileInfo().IsDir() {
			return file
		}
	}
	return nil
}

func validateLayerdrawManifest(manifest LayerdrawManifest, files map[string][]byte) error {
	if manifest.Format != LayerdrawFormat || manifest.FormatVersion != LayerdrawFormatVersion || manifest.Language != LayerdrawLanguage {
		return layerdrawFailure(LayerdrawErrorUnsupportedVersion, "manifest.json", nil)
	}
	if manifest.Entry == "" || !strings.HasSuffix(manifest.Entry, ".ldl") || manifest.ProjectAddress == "" || !validSemanticHash(manifest.DefinitionHash) || !validSemanticHash(manifest.ResolvedFileDigest) || manifest.Files == nil {
		return layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
	}
	if manifest.Redaction != nil && (manifest.Redaction.PolicyID == "" || !utf8.ValidString(manifest.Redaction.PolicyID) || norm.NFC.String(manifest.Redaction.PolicyID) != manifest.Redaction.PolicyID) {
		return layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
	}
	if _, listed := manifest.Files["manifest.json"]; listed {
		return layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
	}
	if len(manifest.Files) != len(files)-1 {
		return layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
	}
	for name, value := range files {
		if name == "manifest.json" {
			continue
		}
		digest, exists := manifest.Files[name]
		if !exists || !validSemanticHash(digest) || rawDigest(value) != digest {
			return layerdrawFailure(LayerdrawErrorDigestMismatch, name, nil)
		}
	}
	if _, ok := files[manifest.Entry]; !ok {
		return layerdrawFailure(LayerdrawErrorManifest, manifest.Entry, nil)
	}
	resolved, ok := files["layerdraw.resolved.json"]
	if !ok || rawDigest(resolved) != manifest.ResolvedFileDigest {
		return layerdrawFailure(LayerdrawErrorDigestMismatch, "layerdraw.resolved.json", nil)
	}
	return nil
}

type resolvedDocument struct {
	Format        string                  `json:"format"`
	FormatVersion int                     `json:"format_version"`
	Language      int                     `json:"language"`
	RootPackID    string                  `json:"root_pack_id,omitempty"`
	Installs      map[string]resolvedPack `json:"installs"`
}

type resolvedPack struct {
	CanonicalID    string            `json:"canonical_id"`
	Version        string            `json:"version"`
	Digest         string            `json:"digest"`
	Path           string            `json:"path"`
	Entry          string            `json:"entry"`
	Files          map[string]string `json:"files"`
	Dependencies   map[string]string `json:"dependencies"`
	RegistrySource string            `json:"registry_source"`
}

func compileInputFromContainer(files map[string][]byte, entry string) (CompileInput, error) {
	var resolved resolvedDocument
	if err := decodeJSON(files["layerdraw.resolved.json"], &resolved, true); err != nil {
		return CompileInput{}, layerdrawFailure(LayerdrawErrorResolvedMetadata, "layerdraw.resolved.json", err)
	}
	if resolved.Format != "layerdraw-resolved" || resolved.FormatVersion != 1 || resolved.Language != 1 || resolved.RootPackID != "" || resolved.Installs == nil {
		return CompileInput{}, layerdrawFailure(LayerdrawErrorResolvedMetadata, "layerdraw.resolved.json", nil)
	}
	input := CompileInput{
		Mode: CompileProject, EntryPath: entry, ProjectSourceTree: map[string][]byte{}, InstalledPackTree: map[string][]byte{},
		ResolvedDependencies: ResolvedDependencies{Format: resolved.Format, FormatVersion: resolved.FormatVersion, Language: resolved.Language},
	}
	installNames := sortedKeys(resolved.Installs)
	for _, installName := range installNames {
		pack := resolved.Installs[installName]
		if pack.RegistrySource == "" || pack.Files == nil || pack.Dependencies == nil {
			return CompileInput{}, layerdrawFailure(LayerdrawErrorResolvedMetadata, "layerdraw.resolved.json", nil)
		}
		manifestPath := pack.Path + "/manifest.json"
		manifestBytes, ok := files[manifestPath]
		if !ok {
			return CompileInput{}, layerdrawFailure(LayerdrawErrorResolvedMetadata, manifestPath, nil)
		}
		converted := ResolvedPack{
			InstallName: installName, CanonicalID: pack.CanonicalID, Version: pack.Version, Digest: pack.Digest,
			Path: pack.Path, Entry: pack.Entry, ManifestPath: "manifest.json", Manifest: bytes.Clone(manifestBytes), RegistrySource: pack.RegistrySource,
		}
		for _, filePath := range sortedKeys(pack.Files) {
			converted.Files = append(converted.Files, ResolvedPackFile{Path: filePath, Digest: pack.Files[filePath]})
		}
		for _, localName := range sortedKeys(pack.Dependencies) {
			converted.Dependencies = append(converted.Dependencies, ResolvedPackDependency{LocalName: localName, InstallName: pack.Dependencies[localName]})
		}
		input.ResolvedDependencies.Installs = append(input.ResolvedDependencies.Installs, converted)
	}
	type packFileIdentity struct {
		packID  string
		locator string
	}
	packPrefixes := make([]string, 0, len(input.ResolvedDependencies.Installs))
	packFiles := map[string]packFileIdentity{}
	for _, pack := range input.ResolvedDependencies.Installs {
		prefix := pack.Path + "/"
		packPrefixes = append(packPrefixes, prefix)
		manifestPath := prefix + pack.ManifestPath
		if _, duplicate := packFiles[manifestPath]; duplicate {
			return CompileInput{}, layerdrawFailure(LayerdrawErrorResolvedMetadata, manifestPath, nil)
		}
		packFiles[manifestPath] = packFileIdentity{packID: pack.CanonicalID, locator: pack.ManifestPath}
		for _, file := range pack.Files {
			fullPath := prefix + file.Path
			if _, duplicate := packFiles[fullPath]; duplicate {
				return CompileInput{}, layerdrawFailure(LayerdrawErrorResolvedMetadata, fullPath, nil)
			}
			packFiles[fullPath] = packFileIdentity{packID: pack.CanonicalID, locator: file.Path}
		}
	}
	sort.Strings(packPrefixes)
	for name, value := range files {
		if name == "manifest.json" || name == "layerdraw.resolved.json" || name == "document.json" || name == "layerdraw.index.json" || strings.HasPrefix(name, "state/query-snapshots/") || strings.HasPrefix(name, "previews/") || strings.HasPrefix(name, "exports/") {
			continue
		}
		if packFile, ok := packFiles[name]; ok {
			input.InstalledPackTree[name] = bytes.Clone(value)
			if mediaType := imageMediaType(name, value); mediaType != "" {
				input.ReferencedAssets = append(input.ReferencedAssets, AssetInput{
					Origin: SourceOriginPack, PackID: packFile.packID, Locator: packFile.locator,
					Bytes: bytes.Clone(value), Digest: rawDigest(value), MediaType: mediaType, ByteLength: int64(len(value)),
				})
			}
			continue
		}
		insidePackTree := false
		for _, prefix := range packPrefixes {
			if strings.HasPrefix(name, prefix) {
				insidePackTree = true
				break
			}
		}
		if insidePackTree {
			return CompileInput{}, layerdrawFailure(LayerdrawErrorForbiddenPortable, name, nil)
		}
		if strings.HasSuffix(name, ".ldl") {
			input.ProjectSourceTree[name] = bytes.Clone(value)
			continue
		}
		if strings.HasPrefix(name, "assets/") {
			if mediaType := imageMediaType(name, value); mediaType != "" {
				input.ReferencedAssets = append(input.ReferencedAssets, AssetInput{
					Origin: SourceOriginProject, Locator: name, Bytes: bytes.Clone(value), Digest: rawDigest(value),
					MediaType: mediaType, ByteLength: int64(len(value)),
				})
				continue
			}
		}
		return CompileInput{}, layerdrawFailure(LayerdrawErrorForbiddenPortable, name, nil)
	}
	sort.Slice(input.ReferencedAssets, func(i, j int) bool {
		left := string(input.ReferencedAssets[i].Origin) + "\x00" + input.ReferencedAssets[i].PackID + "\x00" + input.ReferencedAssets[i].Locator
		right := string(input.ReferencedAssets[j].Origin) + "\x00" + input.ReferencedAssets[j].PackID + "\x00" + input.ReferencedAssets[j].Locator
		return left < right
	})
	return input, nil
}

func canonicalResolved(input ResolvedDependencies) ([]byte, error) {
	if input.Format != "layerdraw-resolved" || input.FormatVersion != 1 || input.Language != 1 {
		return nil, errors.New("unsupported resolved dependency envelope")
	}
	document := resolvedDocument{Format: input.Format, FormatVersion: input.FormatVersion, Language: input.Language, Installs: map[string]resolvedPack{}}
	for _, pack := range input.Installs {
		if pack.InstallName == "" || pack.RegistrySource == "" || document.Installs[pack.InstallName].CanonicalID != "" {
			return nil, errors.New("invalid resolved Pack metadata")
		}
		converted := resolvedPack{
			CanonicalID: pack.CanonicalID, Version: pack.Version, Digest: pack.Digest, Path: pack.Path,
			Entry: pack.Entry, Files: map[string]string{}, Dependencies: map[string]string{}, RegistrySource: pack.RegistrySource,
		}
		for _, file := range pack.Files {
			if file.Path == "" || converted.Files[file.Path] != "" {
				return nil, errors.New("duplicate resolved Pack file")
			}
			converted.Files[file.Path] = file.Digest
		}
		for _, dependency := range pack.Dependencies {
			if dependency.LocalName == "" || converted.Dependencies[dependency.LocalName] != "" {
				return nil, errors.New("duplicate resolved Pack dependency")
			}
			converted.Dependencies[dependency.LocalName] = dependency.InstallName
		}
		document.Installs[pack.InstallName] = converted
	}
	return canonicalArtifact(document)
}

type containerIndex struct {
	Format        string        `json:"format"`
	SchemaVersion int           `json:"schema_version"`
	SourceMap     SourceMap     `json:"source_map"`
	SemanticIndex SemanticIndex `json:"semantic_index"`
}

func canonicalIndex(snapshot Snapshot) ([]byte, error) {
	return canonicalArtifact(containerIndex{Format: "layerdraw-index", SchemaVersion: 1, SourceMap: snapshot.SourceMap, SemanticIndex: snapshot.SemanticIndex})
}

func validateContainerState(ctx context.Context, snapshot Snapshot, files map[string][]byte, redaction *PackageRedaction) (map[string]StateQuerySnapshot, error) {
	result := map[string]StateQuerySnapshot{}
	hasRedaction := false
	for name, value := range files {
		if !strings.HasPrefix(name, "state/") {
			continue
		}
		if !strings.HasPrefix(name, "state/query-snapshots/") || !strings.HasSuffix(name, ".json") {
			return nil, layerdrawFailure(LayerdrawErrorStateSnapshot, name, nil)
		}
		state, err := decodeStateSnapshot(value)
		if err != nil {
			return nil, layerdrawFailure(LayerdrawErrorStateSnapshot, name, err)
		}
		_, diagnostics, validationErr := validateStateQuerySnapshotForDefinition(ctx, snapshot.QueryDefinitionIdentity(), *snapshot.TypedAST.Graph, state)
		if validationErr != nil || hasErrorDiagnostics(diagnostics) {
			return nil, layerdrawFailure(LayerdrawErrorStateSnapshot, name, validationErr)
		}
		hash, err := stateQuerySnapshotHash(state)
		if err != nil || name != "state/query-snapshots/"+strings.TrimPrefix(hash, "sha256:")+".json" {
			return nil, layerdrawFailure(LayerdrawErrorStateSnapshot, name, err)
		}
		if len(state.InaccessibleFieldPaths) != 0 {
			hasRedaction = true
		}
		for _, subject := range state.Subjects {
			hasRedaction = hasRedaction || len(subject.RedactedFieldPaths) != 0
		}
		result[name] = state
	}
	if hasRedaction && redaction == nil {
		return nil, layerdrawFailure(LayerdrawErrorStateSnapshot, "manifest.json", nil)
	}
	return result, nil
}

func canonicalStateSnapshot(ctx context.Context, snapshot Snapshot, state StateQuerySnapshot) ([]byte, string, bool, error) {
	_, diagnostics, err := validateStateQuerySnapshotForDefinition(ctx, snapshot.QueryDefinitionIdentity(), *snapshot.TypedAST.Graph, state)
	if err != nil || hasErrorDiagnostics(diagnostics) {
		return nil, "", false, layerdrawFailure(LayerdrawErrorStateSnapshot, "", err)
	}
	hash, err := stateQuerySnapshotHash(state)
	if err != nil {
		return nil, "", false, layerdrawFailure(LayerdrawErrorStateSnapshot, "", err)
	}
	wire, err := stateSnapshotWireValue(state)
	if err != nil {
		return nil, "", false, layerdrawFailure(LayerdrawErrorStateSnapshot, "", err)
	}
	value, err := canonicalArtifact(wire)
	if err != nil {
		return nil, "", false, layerdrawFailure(LayerdrawErrorStateSnapshot, "", err)
	}
	if _, err := decodeStateSnapshot(value); err != nil {
		return nil, "", false, layerdrawFailure(LayerdrawErrorStateSnapshot, "", err)
	}
	hasRedaction := len(state.InaccessibleFieldPaths) != 0
	for _, subject := range state.Subjects {
		hasRedaction = hasRedaction || len(subject.RedactedFieldPaths) != 0
	}
	return value, hash, hasRedaction, nil
}

type stateSnapshotWire struct {
	Format                   string                  `json:"format"`
	SchemaVersion            int                     `json:"schema_version"`
	DefinitionProjectAddress string                  `json:"definition_project_address"`
	DefinitionHash           string                  `json:"definition_hash"`
	GraphHash                string                  `json:"graph_hash"`
	StateVersion             string                  `json:"state_version"`
	CapturedAt               string                  `json:"captured_at"`
	InaccessibleFieldPaths   []string                `json:"inaccessible_field_paths"`
	Subjects                 []stateQuerySubjectWire `json:"subjects"`
}

type stateQuerySubjectWire struct {
	SubjectAddress     string                      `json:"subject_address"`
	OwnSubjectHash     string                      `json:"own_subject_hash"`
	Fields             map[string]recipeScalarWire `json:"fields"`
	RedactedFieldPaths []string                    `json:"redacted_field_paths"`
}

type recipeScalarWire struct {
	Kind         string  `json:"kind"`
	BooleanValue *bool   `json:"boolean_value,omitempty"`
	IntegerValue *string `json:"integer_value,omitempty"`
	NumberValue  *string `json:"number_value,omitempty"`
	StringValue  *string `json:"string_value,omitempty"`
}

func stateSnapshotWireValue(state StateQuerySnapshot) (stateSnapshotWire, error) {
	result := stateSnapshotWire{
		Format: state.Format, SchemaVersion: state.SchemaVersion, DefinitionProjectAddress: state.DefinitionProject,
		DefinitionHash: state.DefinitionHash, GraphHash: state.GraphHash, StateVersion: state.StateVersion,
		CapturedAt: state.CapturedAt, InaccessibleFieldPaths: append([]string{}, state.InaccessibleFieldPaths...), Subjects: make([]stateQuerySubjectWire, len(state.Subjects)),
	}
	for index, subject := range state.Subjects {
		mapped := stateQuerySubjectWire{SubjectAddress: subject.SubjectAddress, OwnSubjectHash: subject.OwnSubjectHash, Fields: map[string]recipeScalarWire{}, RedactedFieldPaths: append([]string{}, subject.RedactedFieldPaths...)}
		for field, scalar := range subject.Fields {
			value, err := recipeScalarToWire(scalar)
			if err != nil {
				return stateSnapshotWire{}, err
			}
			mapped.Fields[field] = value
		}
		result.Subjects[index] = mapped
	}
	return result, nil
}

func recipeScalarToWire(value TypedScalar) (recipeScalarWire, error) {
	kind := string(value.Type)
	result := recipeScalarWire{Kind: kind}
	switch value.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		result.StringValue = layerdrawStringPointer(value.String)
	case definition.ScalarInteger:
		result.IntegerValue = layerdrawStringPointer(strconv.FormatInt(value.Int, 10))
	case definition.ScalarNumber:
		result.NumberValue = layerdrawStringPointer(strconv.FormatFloat(value.Float, 'g', -1, 64))
	case definition.ScalarBoolean:
		copy := value.Bool
		result.BooleanValue = &copy
	default:
		return recipeScalarWire{}, fmt.Errorf("unsupported scalar kind %q", kind)
	}
	return result, nil
}

func decodeStateSnapshot(value []byte) (StateQuerySnapshot, error) {
	var wire stateSnapshotWire
	if err := decodeJSON(value, &wire, true); err != nil {
		return StateQuerySnapshot{}, err
	}
	if wire.InaccessibleFieldPaths == nil || wire.Subjects == nil {
		return StateQuerySnapshot{}, errors.New("missing state snapshot collections")
	}
	result := StateQuerySnapshot{
		Format: wire.Format, SchemaVersion: wire.SchemaVersion, DefinitionProject: wire.DefinitionProjectAddress,
		DefinitionHash: wire.DefinitionHash, GraphHash: wire.GraphHash, StateVersion: wire.StateVersion,
		CapturedAt: wire.CapturedAt, InaccessibleFieldPaths: append([]string{}, wire.InaccessibleFieldPaths...), Subjects: make([]StateQuerySubject, len(wire.Subjects)),
	}
	for index, subject := range wire.Subjects {
		if subject.Fields == nil || subject.RedactedFieldPaths == nil {
			return StateQuerySnapshot{}, errors.New("missing state subject collections")
		}
		mapped := StateQuerySubject{SubjectAddress: subject.SubjectAddress, OwnSubjectHash: subject.OwnSubjectHash, Fields: map[string]TypedScalar{}, RedactedFieldPaths: append([]string{}, subject.RedactedFieldPaths...)}
		for field, value := range subject.Fields {
			scalar, err := recipeScalarFromWire(value)
			if err != nil {
				return StateQuerySnapshot{}, err
			}
			mapped.Fields[field] = scalar
		}
		result.Subjects[index] = mapped
	}
	return result, nil
}

func recipeScalarFromWire(input recipeScalarWire) (TypedScalar, error) {
	switch input.Kind {
	case "string", "enum", "date", "datetime":
		if input.StringValue == nil || input.BooleanValue != nil || input.IntegerValue != nil || input.NumberValue != nil {
			return TypedScalar{}, errors.New("missing string scalar")
		}
		return TypedScalar{Type: definition.ScalarType(input.Kind), String: *input.StringValue}, nil
	case "integer":
		if input.IntegerValue == nil || input.BooleanValue != nil || input.NumberValue != nil || input.StringValue != nil {
			return TypedScalar{}, errors.New("missing integer scalar")
		}
		value, err := strconv.ParseInt(*input.IntegerValue, 10, 64)
		return TypedScalar{Type: definition.ScalarInteger, Int: value}, err
	case "number":
		if input.NumberValue == nil || input.BooleanValue != nil || input.IntegerValue != nil || input.StringValue != nil {
			return TypedScalar{}, errors.New("missing number scalar")
		}
		value, err := strconv.ParseFloat(*input.NumberValue, 64)
		return TypedScalar{Type: definition.ScalarNumber, Float: value}, err
	case "boolean":
		if input.BooleanValue == nil || input.IntegerValue != nil || input.NumberValue != nil || input.StringValue != nil {
			return TypedScalar{}, errors.New("missing boolean scalar")
		}
		return TypedScalar{Type: definition.ScalarBoolean, Bool: *input.BooleanValue}, nil
	default:
		return TypedScalar{}, fmt.Errorf("unsupported scalar kind %q", input.Kind)
	}
}

func validatePortableFiles(files map[string][]byte, secrets [][]byte, limits LayerdrawLimits) error {
	entryCount := int64(len(files))
	if entryCount > limits.MaxEntries {
		return layerdrawFailure(LayerdrawErrorEntryCountExceeded, "", nil)
	}
	seen := map[string]string{}
	var total int64
	for name, value := range files {
		if strings.HasSuffix(name, "/") {
			return layerdrawFailure(LayerdrawErrorUnsafeEntry, name, nil)
		}
		if int64(len(value)) > limits.MaxEntryBytes {
			return layerdrawFailure(LayerdrawErrorEntrySizeExceeded, name, nil)
		}
		if int64(len(value)) > limits.MaxTotalBytes-total {
			return layerdrawFailure(LayerdrawErrorTotalSizeExceeded, name, nil)
		}
		total += int64(len(value))
		if err := validateContainerPath(name, limits); err != nil {
			return err
		}
		key := cases.Fold().String(norm.NFC.String(name))
		if prior, exists := seen[key]; exists {
			return layerdrawFailure(LayerdrawErrorUnsafeEntry, prior+"|"+name, nil)
		}
		seen[key] = name
		lower := strings.ToLower(name)
		if forbiddenPortablePath(lower) {
			return layerdrawFailure(LayerdrawErrorForbiddenPortable, name, nil)
		}
		for _, secret := range secrets {
			if len(secret) != 0 && bytes.Contains(value, secret) {
				return layerdrawFailure(LayerdrawErrorForbiddenPortable, name, nil)
			}
		}
		if strings.HasSuffix(lower, ".json") {
			if err := validatePortableJSON(value); err != nil {
				return layerdrawFailure(LayerdrawErrorForbiddenPortable, name, err)
			}
		}
	}
	return nil
}

func forbiddenPortablePath(lower string) bool {
	base := path.Base(lower)
	if base == "layerdraw.backend.json" || base == "project.ldbackend.json" || strings.HasSuffix(base, ".ldbackend.json") || strings.HasSuffix(base, ".ldstate.json") {
		return true
	}
	for _, segment := range strings.Split(lower, "/") {
		switch segment {
		case "leases", "presence", "history", "sessions", "autosave", "recovery":
			return true
		}
	}
	return false
}

var forbiddenPortableKeys = map[string]bool{
	"credential": true, "credentials": true, "access_token": true, "refresh_token": true,
	"api_key": true, "client_secret": true, "secret": true, "password": true,
	"lease": true, "lease_id": true, "lease_token": true, "fencing_token": true,
	"presence": true, "current_session": true, "backend_binding": true, "backend_config": true,
	"backend_locator": true, "document_id": true, "host_document_id": true,
}

func validatePortableJSON(value []byte) error {
	if err := validateJSONStructure(value); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return err
	}
	return walkPortableJSON(root, "")
}

func walkPortableJSON(value any, key string) error {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(childKey, "-", "_"))
			if forbiddenPortableKeys[normalized] || strings.HasSuffix(normalized, "_credential") || strings.HasSuffix(normalized, "_secret") || strings.HasSuffix(normalized, "_password") || strings.HasSuffix(normalized, "_token") {
				return fmt.Errorf("forbidden portable key %q", childKey)
			}
			if err := walkPortableJSON(child, normalized); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := walkPortableJSON(child, key); err != nil {
				return err
			}
		}
	case string:
		lower := strings.ToLower(typed)
		pathLike := strings.Contains(key, "path") || strings.Contains(key, "locator") || strings.Contains(key, "uri")
		if strings.HasPrefix(lower, "file://") || (pathLike && (strings.HasPrefix(typed, "/") || strings.HasPrefix(typed, "\\\\") || (len(typed) >= 3 && typed[1] == ':' && (typed[2] == '\\' || typed[2] == '/')))) {
			return errors.New("absolute local path in portable JSON")
		}
		if strings.Contains(key, "source") || strings.Contains(key, "uri") || strings.Contains(key, "url") {
			parsed, err := url.Parse(typed)
			if err == nil && parsed.IsAbs() {
				if parsed.User != nil {
					return errors.New("credential-bearing URL in portable JSON")
				}
				for queryKey := range parsed.Query() {
					normalized := strings.ToLower(strings.ReplaceAll(queryKey, "-", "_"))
					if forbiddenPortableKeys[normalized] || strings.HasSuffix(normalized, "_token") || strings.HasSuffix(normalized, "_secret") || strings.HasSuffix(normalized, "_password") {
						return errors.New("credential-bearing URL query in portable JSON")
					}
				}
			}
		}
	}
	return nil
}

func writeCanonicalZip(ctx context.Context, files map[string][]byte) ([]byte, error) {
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, name := range sortedKeys(files) {
		if err := layerdrawContext(ctx); err != nil {
			_ = writer.Close()
			return nil, err
		}
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o644)
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		header.CreatorVersion = 3 << 8
		header.ReaderVersion = 20
		entry, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, layerdrawFailure(LayerdrawErrorInvariant, name, err)
		}
		if _, err := entry.Write(files[name]); err != nil {
			_ = writer.Close()
			return nil, layerdrawFailure(LayerdrawErrorInvariant, name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, layerdrawFailure(LayerdrawErrorInvariant, "", err)
	}
	return out.Bytes(), nil
}

func canonicalArtifact(value any) ([]byte, error) {
	encoded, err := materialize.Canonicalize(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func canonicalJSONBytes(value []byte) ([]byte, error) {
	if err := validateJSONStructure(value); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return canonicalArtifact(decoded)
}

func decodeJSON(value []byte, output any, disallowUnknown bool) error {
	if err := validateJSONStructure(value); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	if disallowUnknown {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(output); err != nil {
		return err
	}
	return nil
}

func validateJSONStructure(value []byte) error {
	if !utf8.Valid(value) {
		return errors.New("JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 128 {
			return errors.New("JSON nesting exceeds 128")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return errors.New("duplicate or invalid JSON object key")
				}
				seen[key] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return errors.New("invalid JSON object close")
			}
		case '[':
			for decoder.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return errors.New("invalid JSON array close")
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		return nil
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func imageMediaType(name string, value []byte) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".png") && len(value) >= 8 && bytes.Equal(value[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case (strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg")) && len(value) >= 3 && bytes.Equal(value[:3], []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".webp") && len(value) >= 12 && string(value[:4]) == "RIFF" && string(value[8:12]) == "WEBP":
		return "image/webp"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	default:
		return ""
	}
}

func hasErrorDiagnostics(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func rawDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneByteMap(input map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(input))
	for key, value := range input {
		result[key] = bytes.Clone(value)
	}
	return result
}

func layerdrawStringPointer(value string) *string { return &value }

func layerdrawContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return layerdrawFailure(LayerdrawErrorCancelled, "", err)
	}
	return nil
}
