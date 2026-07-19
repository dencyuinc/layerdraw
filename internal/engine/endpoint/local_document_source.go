// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

// LocalProjectInput is a closed, host-supplied local project tree. Runtime
// hosts provide dependency and asset closure explicitly; this facade does not
// read providers or infer lock metadata.
type LocalProjectInput struct {
	EntryPath            string
	ProjectSourceTree    map[string][]byte
	InstalledPackTree    map[string][]byte
	ResolvedDependencies engine.ResolvedDependencies
	ReferencedAssets     []engine.AssetInput
	ResourceLimits       engine.ResourceLimits
}

type (
	LocalResolvedDependencies = engine.ResolvedDependencies
	LocalAssetInput           = engine.AssetInput
	LocalResourceLimits       = engine.ResourceLimits
)

// LocalSource is a validated closed Engine input with portable identity.
// Its Engine representation remains private to this sole handwritten mapping
// boundary.
type LocalSource struct {
	input          engine.CompileInput
	PortableID     string
	DefinitionHash protocolcommon.Digest
	GraphHash      protocolcommon.Digest
	subjectHashes  []semantic.SubjectHash
}

type LocalDocumentEngine struct{ engine engine.Engine }

func NewLocalDocumentEngine() *LocalDocumentEngine {
	return &LocalDocumentEngine{engine: engine.New(engine.BuildInfo{})}
}

func (e *LocalDocumentEngine) NewRuntimeEngineBridge(endpointID protocolcommon.EndpointInstanceID) *RuntimeEngineBridge {
	return NewRuntimeEngineBridge(e.engine, endpointID)
}

func (e *LocalDocumentEngine) CompileProject(ctx context.Context, input LocalProjectInput) (LocalSource, error) {
	closed := engine.CompileInput{Mode: engine.CompileProject, EntryPath: input.EntryPath, ProjectSourceTree: input.ProjectSourceTree, InstalledPackTree: input.InstalledPackTree, ResolvedDependencies: input.ResolvedDependencies, ReferencedAssets: input.ReferencedAssets, ResourceLimits: input.ResourceLimits}
	return e.validate(ctx, closed)
}

func (e *LocalDocumentEngine) ReadContainer(ctx context.Context, data []byte) (LocalSource, error) {
	document, err := e.engine.ReadLayerdraw(ctx, engine.LayerdrawReadInput{Bytes: data})
	if err != nil {
		return LocalSource{}, err
	}
	return sourceFromSnapshot(document.CompileInput, document.Compilation)
}

func (e *LocalDocumentEngine) ReadEncodedInput(ctx context.Context, data []byte) (LocalSource, error) {
	input, err := DecodeLocalCompileInput(data)
	if err != nil {
		return LocalSource{}, err
	}
	return e.validate(ctx, input)
}

func (e *LocalDocumentEngine) WithProjectTree(ctx context.Context, source LocalSource, tree map[string][]byte) (LocalSource, error) {
	input := cloneCompileInput(source.input)
	input.ProjectSourceTree = cloneByteMap(tree)
	return e.validate(ctx, input)
}

func (e *LocalDocumentEngine) validate(ctx context.Context, input engine.CompileInput) (LocalSource, error) {
	compiled, err := e.engine.Compile(ctx, input)
	if err != nil {
		return LocalSource{}, err
	}
	return sourceFromSnapshot(input, compiled.Snapshot())
}

func sourceFromSnapshot(input engine.CompileInput, snapshot engine.Snapshot) (LocalSource, error) {
	if len(snapshot.Diagnostics) != 0 || snapshot.NormalizedDocument == nil || snapshot.GraphHash == nil {
		return LocalSource{}, errors.New("source is not a valid publishable project")
	}
	subjectHashes := make([]semantic.SubjectHash, len(snapshot.SubjectSemanticHashes))
	for index, subject := range snapshot.SubjectSemanticHashes {
		subjectHashes[index] = semantic.SubjectHash{Address: semantic.StableAddress(subject.Address), Kind: semantic.SubjectKind(subject.Kind), Hash: protocolcommon.Digest(subject.Hash)}
	}
	return LocalSource{input: cloneCompileInput(input), PortableID: snapshot.NormalizedDocument.Project.Address, DefinitionHash: protocolcommon.Digest(snapshot.DefinitionHash), GraphHash: protocolcommon.Digest(*snapshot.GraphHash), subjectHashes: subjectHashes}, nil
}

func (s LocalSource) Digest() protocolcommon.Digest { return digestJSON(s.input) }

func (s LocalSource) SubjectHashes() []semantic.SubjectHash {
	return append([]semantic.SubjectHash(nil), s.subjectHashes...)
}

// ProjectSourceTree returns the Engine-owned canonical project projection.
// Callers receive a deep copy and do not gain access to CompileInput internals.
func (s LocalSource) ProjectSourceTree() map[string][]byte {
	return cloneByteMap(s.input.ProjectSourceTree)
}

// WriteContainer delegates canonical container construction to Engine; hosts
// never synthesize the container representation themselves.
func (e *LocalDocumentEngine) WriteContainer(ctx context.Context, source LocalSource) ([]byte, error) {
	result, err := e.engine.WriteLayerdraw(ctx, engine.LayerdrawWriteInput{CompileInput: cloneCompileInput(source.input)})
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), result...), nil
}

func (s LocalSource) EncodedInput() ([]byte, protocolcommon.BlobRef, error) {
	data, err := EncodeLocalCompileInput(s.input)
	if err != nil {
		return nil, protocolcommon.BlobRef{}, err
	}
	return data, LocalCompileInputRef(data), nil
}

func digestJSON(value any) protocolcommon.Digest {
	data, _ := json.Marshal(value)
	return digestBytes(data)
}
