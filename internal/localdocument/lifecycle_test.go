// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/adapter/local"
	"github.com/dencyuinc/layerdraw/internal/engine"
	runtimehost "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type countingReader struct {
	mu    sync.Mutex
	value byte
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range p {
		r.value++
		p[i] = r.value
	}
	return len(p), nil
}

type fakeScheduler struct {
	mu   sync.Mutex
	jobs []*fakeJob
}
type fakeJob struct {
	fn        func()
	cancelled bool
}

func (s *fakeScheduler) Schedule(_ time.Duration, fn func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := &fakeJob{fn: fn}
	s.jobs = append(s.jobs, job)
	return func() { s.mu.Lock(); job.cancelled = true; s.mu.Unlock() }
}
func (s *fakeScheduler) fireLast() {
	s.mu.Lock()
	job := s.jobs[len(s.jobs)-1]
	cancelled := job.cancelled
	s.mu.Unlock()
	if !cancelled {
		job.fn()
	}
}

func newTestHost(t *testing.T, root string, edit func(*Config)) *Host {
	t.Helper()
	config := Config{Root: root, Clock: &fakeClock{now: time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)}, Random: &countingReader{}}
	if edit != nil {
		edit(&config)
	}
	host, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func writeProject(t *testing.T, root, source string) string {
	t.Helper()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return project
}

func durableFileSnapshot(t *testing.T, root string) string {
	t.Helper()
	files := map[string][]byte{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative], err = os.ReadFile(path)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func omitContainerEntry(t *testing.T, archive []byte, omitted string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range reader.File {
		if file.Name == omitted {
			continue
		}
		if err := writer.Copy(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func projectInput(source string) engine.CompileInput {
	return engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}
}

func allPreconditions(t *testing.T, source string) engineprotocol.EngineEditPreconditions {
	t.Helper()
	return inputPreconditions(t, projectInput(source))
}

func inputPreconditions(t *testing.T, input engine.CompileInput) engineprotocol.EngineEditPreconditions {
	t.Helper()
	result, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := result.Snapshot()
	pre := engineprotocol.EngineEditPreconditions{DocumentGeneration: engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "placeholder", Value: "document_placeholder_123456"}, Value: "1"}, ExpectedSubjectHashes: []engineprotocol.ExpectedHash{}, ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{}, ExpectedChildSets: []engineprotocol.ExpectedChildSet{}}
	for _, value := range snapshot.SubjectSemanticHashes {
		pre.ExpectedSubjectHashes = append(pre.ExpectedSubjectHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(value.Address), Hash: protocolcommon.Digest(value.Hash)})
	}
	for _, value := range snapshot.SubtreeHashes {
		pre.ExpectedSubtreeHashes = append(pre.ExpectedSubtreeHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(value.OwnerAddress), Hash: protocolcommon.Digest(value.Hash)})
	}
	for _, value := range snapshot.ChildSetHashes {
		pre.ExpectedChildSets = append(pre.ExpectedChildSets, engineprotocol.ExpectedChildSet{OwnerAddress: semantic.StableAddress(value.OwnerAddress), ChildKind: semantic.SubjectKind(value.ChildKind), Hash: protocolcommon.Digest(value.Hash)})
	}
	sources := []engineprotocol.ExpectedSourceDigest{}
	for _, file := range snapshot.SourceMap.Files {
		origin := semantic.SourceOrigin{Kind: semantic.OriginKind(file.Origin.Kind)}
		if file.Origin.PackAddress != "" {
			value := semantic.PackRootAddress(file.Origin.PackAddress)
			origin.PackAddress = &value
		}
		sources = append(sources, engineprotocol.ExpectedSourceDigest{Module: semantic.ModuleRef{Origin: origin, ModulePath: file.ModulePath}, Digest: protocolcommon.Digest(file.Digest)})
	}
	pre.ExpectedSourceDigests = &sources
	return pre
}

func createLayerBatch(t *testing.T, id string) engineprotocol.SemanticOperationBatch {
	t.Helper()
	data := fmt.Sprintf(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":%q,"fields":{"display_name":"Layer","order":"1"}}]}`, id)
	value, err := engineprotocol.DecodeSemanticOperationBatch([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func sessionPreconditions(t *testing.T, host *Host, session *Session) engineprotocol.EngineEditPreconditions {
	t.Helper()
	revision, err := host.documents.ReadRevision(context.Background(), port.ReadRevisionInput{Scope: session.Open.Session.Scope, RevisionID: session.Open.CommittedRevision.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := host.documents.ReadSourceBlobs(context.Background(), port.ReadSourceBlobsInput{Scope: session.Open.Session.Scope, Revision: revision.Revision, Blobs: revision.SourceBlobs})
	if err != nil || len(blobs.Blobs) != 1 {
		t.Fatalf("current sources=%+v err=%v", blobs, err)
	}
	var input engine.CompileInput
	if err := json.Unmarshal(blobs.Blobs[0].Contents, &input); err != nil {
		t.Fatal(err)
	}
	return inputPreconditions(t, input)
}

func TestProjectOpenSaveDuplicateRestartExternalChangeAndClose(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Session.PortableID != "ldl:project:p" || len(opened.History.Items) != 1 {
		t.Fatalf("open = %+v", opened)
	}
	repeated, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Session.Open.Session.Scope.DocumentID != opened.Session.Open.Session.Scope.DocumentID || repeated.Session.Open.Session.RuntimeSessionID == opened.Session.Open.Session.RuntimeSessionID {
		t.Fatal("repeated open did not resolve one document into distinct sessions")
	}
	if err := host.Close(context.Background(), repeated.Session); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(context.Background(), repeated.Session); err != nil {
		t.Fatal(err)
	}

	save := SaveInput{Session: opened.Session, Operations: createLayerBatch(t, "layer1"), Preconditions: allPreconditions(t, source), OperationID: "save_one", IdempotencyKey: "idempotency_save_one", Trigger: runtimeprotocol.CommitTriggerExplicitSave}
	committed, err := host.Save(context.Background(), save)
	if err != nil {
		t.Fatal(err)
	}
	if committed.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted {
		t.Fatalf("save = %+v", committed)
	}
	materialized, err := os.ReadFile(filepath.Join(project, "document.ldl"))
	if err != nil || !bytes.Contains(materialized, []byte("layer1")) {
		t.Fatalf("explicit save did not materialize Engine source: %q err=%v", materialized, err)
	}
	duplicate, err := host.Save(context.Background(), save)
	if err != nil || duplicate.OperationResult.ResultDigest != committed.OperationResult.ResultDigest {
		t.Fatalf("duplicate = %+v %v", duplicate, err)
	}
	changed := save
	changed.Operations = createLayerBatch(t, "other")
	if _, err := host.Save(context.Background(), changed); err == nil {
		t.Fatal("changed idempotent retry was accepted")
	}
	wrongKey := save
	wrongKey.IdempotencyKey = "idempotency_wrong_key"
	if _, err := host.Save(context.Background(), wrongKey); err == nil {
		t.Fatal("changed idempotency key was accepted")
	}
	status, err := host.GetOperationStatus(context.Background(), opened.Session, save.OperationID)
	if err != nil || status.OperationResult == nil || status.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted {
		t.Fatalf("status = %+v %v", status, err)
	}
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	restarted := newTestHost(t, filepath.Join(root, "data"), nil)
	afterRestart, err := restarted.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart.Session.Open.Session.Scope.DocumentID != opened.Session.Open.Session.Scope.DocumentID || len(afterRestart.History.Items) != 2 {
		t.Fatalf("restart = %+v", afterRestart)
	}
	retry := save
	retry.Session = afterRestart.Session
	if replay, err := restarted.Save(context.Background(), retry); err != nil || replay.OperationResult.ResultDigest != committed.OperationResult.ResultDigest {
		t.Fatalf("restart duplicate=%+v err=%v", replay, err)
	}
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project p \"Externally changed\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Save(context.Background(), SaveInput{Session: afterRestart.Session, Operations: createLayerBatch(t, "blocked"), Preconditions: allPreconditions(t, source)}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("external save error = %v", err)
	}
	preview, err := restarted.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExternalChange == nil || !preview.ExternalChange.RequiresReview {
		t.Fatalf("external preview = %+v", preview.ExternalChange)
	}
}

func TestSuccessfulSavesExceedRecoveryBoundWithoutAccumulatingStages(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	host := newTestHost(t, filepath.Join(root, "data"), func(c *Config) { c.MaxRecoveryItems = 1 })
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		result, err := host.Save(context.Background(), SaveInput{Session: opened.Session, Operations: createLayerBatch(t, fmt.Sprintf("bounded_%d", i)), Preconditions: sessionPreconditions(t, host, opened.Session), OperationID: runtimeprotocol.OperationID(fmt.Sprintf("bounded_save_%d", i)), IdempotencyKey: runtimeprotocol.IdempotencyKey(fmt.Sprintf("idempotency_bounded_%d", i)), Trigger: runtimeprotocol.CommitTriggerExplicitSave})
		if err != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted {
			t.Fatalf("save %d=%+v err=%v", i, result, err)
		}
		stages, err := host.documents.ListStaged(context.Background(), opened.Session.Open.Session.Scope, 1)
		if err != nil || len(stages) != 0 {
			t.Fatalf("save %d stages=%+v err=%v", i, stages, err)
		}
	}
	if recovered, err := host.Recover(context.Background(), opened.Session.Open.Session.Scope.DocumentID); err != nil || len(recovered) != 0 {
		t.Fatalf("terminal records consumed recovery bound: %+v err=%v", recovered, err)
	}
	if status, err := host.GetOperationStatus(context.Background(), opened.Session, "bounded_save_0"); err != nil || status.OperationResult == nil {
		t.Fatalf("terminal lookup=%+v err=%v", status, err)
	}
	if _, err := host.Save(context.Background(), SaveInput{Session: opened.Session, Trigger: runtimeprotocol.CommitTrigger("unsupported")}); err == nil {
		t.Fatal("unsupported save trigger was accepted")
	}
}

func TestOperationIdentityIsScopedPerDocument(t *testing.T) {
	root := t.TempDir()
	firstRoot := writeProject(t, filepath.Join(root, "first"), "project p \"First\" {}\n")
	secondRoot := writeProject(t, filepath.Join(root, "second"), "project p \"Second\" {}\n")
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	first, err := host.OpenProject(context.Background(), OpenProjectInput{Root: firstRoot})
	if err != nil {
		t.Fatal(err)
	}
	second, err := host.OpenProject(context.Background(), OpenProjectInput{Root: secondRoot})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		session *Session
		source  string
		layer   string
	}{{first.Session, "project p \"First\" {}\n", "one"}, {second.Session, "project p \"Second\" {}\n", "two"}} {
		result, err := host.Save(context.Background(), SaveInput{Session: fixture.session, Operations: createLayerBatch(t, fixture.layer), Preconditions: allPreconditions(t, fixture.source), OperationID: "same_operation", IdempotencyKey: "same_idempotency_value", Trigger: runtimeprotocol.CommitTriggerExplicitSave})
		if err != nil || result.OperationResult.CommittedRevision == nil || result.OperationResult.CommittedRevision.DocumentID != fixture.session.Open.Session.Scope.DocumentID {
			t.Fatalf("scoped save=%+v err=%v", result, err)
		}
	}
}

func TestMetadataRejectsTrailingJSONAndMalformedBindings(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root, "project p \"P\" {}\n")
	dataRoot := filepath.Join(root, "data")
	host := newTestHost(t, dataRoot, nil)
	if _, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(host.metadataPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.metadataPath(), append(data, []byte(` {}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Root: dataRoot}); err == nil {
		t.Fatal("trailing metadata JSON was accepted")
	}
	var metadata lifecycleMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	for key, binding := range metadata.Bindings {
		delete(metadata.Bindings, key)
		binding.Locator = "relative"
		metadata.Bindings[key] = binding
		break
	}
	malformed, _ := json.Marshal(metadata)
	if err := os.WriteFile(host.metadataPath(), malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Root: dataRoot}); err == nil {
		t.Fatal("malformed metadata binding was accepted")
	}
}

func TestContainerValidationImportIdentityAndNoPartialPublication(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	instance := engine.New(engine.BuildInfo{})
	archive, err := instance.WriteLayerdraw(context.Background(), engine.LayerdrawWriteInput{CompileInput: projectInput("project p \"P\" {}\n")})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "document.layerdraw")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	host := newTestHost(t, dataRoot, nil)
	first, err := host.OpenContainer(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	containerSave, err := host.Save(context.Background(), SaveInput{Session: first.Session, Operations: createLayerBatch(t, "container_saved"), Preconditions: allPreconditions(t, "project p \"P\" {}\n"), OperationID: "container_save", IdempotencyKey: "idempotency_container_save", Trigger: runtimeprotocol.CommitTriggerExplicitSave})
	if err != nil || containerSave.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted {
		t.Fatalf("container save=%+v err=%v", containerSave, err)
	}
	materializedContainer, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	containerSource, err := host.engine.ReadContainer(context.Background(), materializedContainer)
	if err != nil || !bytes.Contains(containerSource.ProjectSourceTree()["document.ldl"], []byte("container_saved")) {
		t.Fatalf("container was not materialized through Engine: %q err=%v", containerSource.ProjectSourceTree()["document.ldl"], err)
	}
	imported, err := host.ImportContainer(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Session.PortableID != imported.Session.PortableID || first.Session.Open.Session.Scope.DocumentID == imported.Session.Open.Session.Scope.DocumentID {
		t.Fatal("import identity contract failed")
	}
	importedID := imported.Session.Open.Session.Scope.DocumentID
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := newTestHost(t, dataRoot, nil)
	reopened, err := restarted.OpenDocument(context.Background(), importedID)
	if err != nil || reopened.Session.PortableID != imported.Session.PortableID {
		t.Fatalf("import reopen=%+v %v", reopened, err)
	}
	host = restarted
	before := len(host.metadata.Bindings)
	corrupt := filepath.Join(root, "corrupt.layerdraw")
	if err := os.WriteFile(corrupt, archive[:len(archive)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.OpenContainer(context.Background(), corrupt); err == nil {
		t.Fatal("truncated container accepted")
	}
	if len(host.metadata.Bindings) != before {
		t.Fatal("partially validated container was published")
	}
}

func TestOpenRejectsUnavailableAssetsWithoutPartialPublication(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	source := "project p \"P\" {}\nentity_type service \"Service\" {\n  image \"assets/icon.svg\"\n  representation shape rect\n}\n"
	project := writeProject(t, filepath.Join(root, "project-input"), source)
	host := newTestHost(t, dataRoot, nil)
	before := durableFileSnapshot(t, dataRoot)

	if _, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project, ReferencedAssets: []engine.AssetInput{}}); err == nil {
		t.Fatal("project with an unavailable referenced asset was accepted")
	}
	if got := durableFileSnapshot(t, dataRoot); got != before {
		t.Fatal("unavailable project asset created a binding, head, or history")
	}

	asset := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(asset))
	input := projectInput(source)
	input.ReferencedAssets = []engine.AssetInput{{Origin: engine.SourceOriginProject, Locator: "assets/icon.svg", Bytes: asset, Digest: digest, MediaType: "image/svg+xml", ByteLength: int64(len(asset))}}
	archive, err := engine.New(engine.BuildInfo{}).WriteLayerdraw(context.Background(), engine.LayerdrawWriteInput{CompileInput: input})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "missing-asset.layerdraw")
	if err := os.WriteFile(path, omitContainerEntry(t, archive, "assets/icon.svg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.OpenContainer(context.Background(), path); err == nil {
		t.Fatal("container with an unavailable referenced asset was accepted")
	}
	if got := durableFileSnapshot(t, dataRoot); got != before {
		t.Fatal("unavailable container asset created a binding, head, or history")
	}
	if len(host.metadata.Bindings) != 0 || len(host.sessions) != 0 {
		t.Fatalf("failed asset opens published bindings=%d sessions=%d", len(host.metadata.Bindings), len(host.sessions))
	}
}

func TestAutosaveUsesInjectedSchedulerAndCloseCancels(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	scheduler := &fakeScheduler{}
	host := newTestHost(t, filepath.Join(root, "data"), func(c *Config) { c.Scheduler = scheduler })
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan AutosaveResult, 1)
	input := SaveInput{Session: opened.Session, Operations: createLayerBatch(t, "auto"), Preconditions: allPreconditions(t, source), OperationID: "autosave_one", IdempotencyKey: "idempotency_auto_one"}
	if err := host.ScheduleAutosave(context.Background(), input, results); err != nil {
		t.Fatal(err)
	}
	scheduler.fireLast()
	result := <-results
	if result.Err != nil || result.Result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted {
		t.Fatalf("autosave = %+v", result)
	}
	autosaved, err := os.ReadFile(filepath.Join(project, "document.ldl"))
	if err != nil || !bytes.Contains(autosaved, []byte("auto")) {
		t.Fatalf("autosave did not materialize Engine source: %q err=%v", autosaved, err)
	}
	second, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.ScheduleAutosave(context.Background(), SaveInput{Session: second.Session, Operations: createLayerBatch(t, "cancelled"), Preconditions: allPreconditions(t, source)}, results); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(context.Background(), second.Session); err != nil {
		t.Fatal(err)
	}
	scheduler.fireLast()
	select {
	case value := <-results:
		t.Fatalf("cancelled autosave ran: %+v", value)
	default:
	}
}

func TestRecoveryConvergesPublishedHeadAfterJournalFailure(t *testing.T) {
	fault := localPersistenceFault(t, "published_journal_failure")
	if fault.Injection != "recovery_fourth_write" {
		t.Fatalf("unsupported lifecycle fault injection %q", fault.Injection)
	}
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	writes := 0
	failed := false
	host := newTestHost(t, filepath.Join(root, "data"), func(c *Config) {
		c.AdapterOptions = local.Options{Fault: func(operation, path string) error {
			if operation == "write" && strings.Contains(path, "recovery") {
				writes++
				if writes == 4 && !failed {
					failed = true
					return io.ErrClosedPipe
				}
			}
			return nil
		}}
	})
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	documentID := opened.Session.Open.Session.Scope.DocumentID
	commit, err := host.Save(context.Background(), SaveInput{Session: opened.Session, Operations: createLayerBatch(t, "recovered"), Preconditions: allPreconditions(t, source), OperationID: "recovery_save", IdempotencyKey: "idempotency_recover", Trigger: runtimeprotocol.CommitTriggerExplicitSave})
	if err != nil {
		t.Fatalf("published commit was reported as rejected: %v", err)
	}
	if commit.OperationResult.Status != runtimeprotocol.OperationResultStatus(fault.ExpectedStatus) || commit.OperationResult.CommittedRevision == nil || commit.OperationResult.ExternalMaterialization == nil || commit.OperationResult.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStatePending {
		t.Fatalf("post-publication journal failure result = %+v", commit)
	}
	restarted := newTestHost(t, filepath.Join(root, "data"), nil)
	results, err := restarted.Recover(context.Background(), documentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Converged || results[0].Status.OperationResult == nil {
		t.Fatalf("recovery = %+v", results)
	}
	status := results[0].Status.OperationResult.Status
	allowed := false
	for _, expected := range fault.ExpectedRecoveryStatus {
		allowed = allowed || status == runtimeprotocol.OperationResultStatus(expected)
	}
	if !allowed {
		t.Fatalf("recovery status = %s", status)
	}
	openedAgain, err := restarted.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if len(openedAgain.History.Items) != 2 {
		t.Fatalf("history after recovery = %+v", openedAgain.History)
	}
}

type localPersistenceFaultCase struct {
	ID                     string   `json:"id"`
	Surface                string   `json:"surface"`
	Injection              string   `json:"injection"`
	Phase                  string   `json:"phase"`
	Publish                bool     `json:"publish"`
	AdvanceAfterPublish    bool     `json:"advance_after_publish"`
	ExtraAdvances          int      `json:"extra_advances"`
	ExpectedStatus         string   `json:"expected_status"`
	ExpectedRecoveryStatus []string `json:"expected_recovery_status"`
	ExpectedExternalState  string   `json:"expected_external_state"`
}

func localPersistenceFault(t *testing.T, id string) localPersistenceFaultCase {
	t.Helper()
	for _, fault := range localPersistenceFaults(t, "local_lifecycle") {
		if fault.ID == id {
			return fault
		}
	}
	t.Fatalf("persistence fault corpus has no local_lifecycle case %q", id)
	return localPersistenceFaultCase{}
}

func localPersistenceFaults(t *testing.T, surface string) []localPersistenceFaultCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "conformance", "testdata", "local_runtime_persistence_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int                         `json:"schema_version"`
		FaultMatrix   []localPersistenceFaultCase `json:"fault_matrix"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil || corpus.SchemaVersion != 1 {
		t.Fatalf("invalid persistence fault corpus: version=%d err=%v", corpus.SchemaVersion, err)
	}
	result := make([]localPersistenceFaultCase, 0, len(corpus.FaultMatrix))
	for _, fault := range corpus.FaultMatrix {
		if fault.Surface == surface {
			result = append(result, fault)
		}
	}
	if len(result) == 0 {
		t.Fatalf("persistence fault corpus has no %s cases", surface)
	}
	return result
}

func stageRecoveryFixture(t *testing.T, host *Host, session *Session, source, suffix string, publish bool, advanceAfterPublish bool) (port.RecoveryRecord, port.StagedRevision) {
	t.Helper()
	ctx := host.accessContext(context.Background(), session)
	operation := runtimeprotocol.OperationID("recovery_" + suffix)
	key := runtimeprotocol.IdempotencyKey("idempotency_" + suffix + "_value")
	base := session.Open.CommittedRevision
	pre := allPreconditions(t, source)
	pre.DocumentGeneration = engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "local-document-runtime", Value: session.working.Handle}, Value: protocolcommon.CanonicalUint64(session.working.Generation)}
	prepared, err := host.workbench.Preview(ctx, port.PreviewWorkingDocumentInput{Document: session.working, Batch: createLayerBatch(t, suffix), Preconditions: pre, MaxOperations: "4096"})
	if err != nil {
		t.Fatal(err)
	}
	grant, _, _ := host.authority.ResolveGrant(ctx, session.Open.Session.Scope)
	decision, rejection := host.runtime.Authorize(ctx, runtimehost.AuthorizationRequest{Scope: session.Open.Session.Scope, CurrentRevision: base, Evaluation: accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "apply"}})
	if rejection != nil {
		t.Fatal(rejection)
	}
	payload := digestJSON(struct {
		Operation runtimeprotocol.OperationID `json:"operation"`
	}{operation})
	record, err := host.recovery.CreatePending(ctx, port.CreatePendingRecordInput{Scope: session.Open.Session.Scope, OperationID: operation, IdempotencyKey: key, PayloadDigest: payload, BaseRevision: base})
	if err != nil {
		t.Fatal(err)
	}
	preview := runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}
	staged, err := host.documents.StageRevision(ctx, port.StageRevisionInput{Scope: session.Open.Session.Scope, OperationID: operation, IdempotencyKey: key, BaseRevision: base, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash, SourceBlobs: prepared.Sources, Manifest: prepared.Manifest, DecisionDigest: decision.DecisionDigest, EvaluationDigest: decision.EvaluationDigest, Actor: grant.ActorRef, Trigger: runtimeprotocol.CommitTriggerAutosave, PreviewEvaluation: &preview})
	if err != nil {
		t.Fatal(err)
	}
	record, err = host.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: session.Open.Session.Scope, OperationID: operation, ExpectedPhase: runtimeprotocol.RecoveryPhasePending, NextPhase: runtimeprotocol.RecoveryPhaseStaged, EvaluationDigest: &decision.EvaluationDigest, DecisionDigest: &decision.DecisionDigest, PreviewEvaluation: &preview})
	if err != nil {
		t.Fatal(err)
	}
	if publish || advanceAfterPublish {
		record, err = host.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: session.Open.Session.Scope, OperationID: operation, ExpectedPhase: runtimeprotocol.RecoveryPhaseStaged, NextPhase: runtimeprotocol.RecoveryPhasePublicationPending})
		if err != nil {
			t.Fatal(err)
		}
	}
	if publish {
		head, _ := host.documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: session.Open.Session.Scope})
		result, err := host.documents.PublishHead(ctx, port.PublishDocumentHeadInput{Scope: session.Open.Session.Scope, StageID: staged.StageID, ExpectedRevision: head.Revision.RevisionID, ExpectedDefinitionHash: head.Revision.DefinitionHash, ExpectedProviderVersion: head.ProviderVersion, FencingToken: head.FencingToken})
		if err != nil || !result.Published {
			t.Fatalf("publish: %+v %v", result, err)
		}
		if advanceAfterPublish {
			record, err = host.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: session.Open.Session.Scope, OperationID: operation, ExpectedPhase: runtimeprotocol.RecoveryPhasePublicationPending, NextPhase: runtimeprotocol.RecoveryPhasePublished, PublishedRevision: &result.Revision})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	return record, staged
}

func TestRestartRecoveryReauthorizesDelegatedExternalPublication(t *testing.T) {
	for _, test := range []struct {
		name    string
		revoke  bool
		expired bool
	}{
		{name: "live"},
		{name: "revoked", revoke: true},
		{name: "expired", expired: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := "project p \"P\" {}\n"
			project := writeProject(t, root, source)
			dataRoot := filepath.Join(root, "data")
			host := newTestHost(t, dataRoot, nil)
			owner, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
			if err != nil {
				t.Fatal(err)
			}
			batch := runtimeprotocol.RuntimeOperationBatch{DocumentID: owner.Session.Open.CommittedRevision.DocumentID, BaseRevision: owner.Session.Open.CommittedRevision, ExpectedDefinitionHash: owner.Session.Open.CommittedRevision.DefinitionHash, Operations: createLayerBatch(t, "delegated_recovery"), Preconditions: allPreconditions(t, source)}
			preview, err := host.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: owner.Session.Open.Session, OperationBatch: batch})
			if err != nil {
				t.Fatal(err)
			}
			now := host.config.Clock.Now()
			record, err := host.DelegateAgent(context.Background(), owner.Session, accesscore.Delegation{
				ID: "recovery-" + test.name, ParentActor: accessprotocol.ActorRef{ActorID: "local-owner", Kind: "user"}, Agent: accessprotocol.ActorRef{ActorID: "recovery-agent", Kind: "agent"},
				DocumentID: string(batch.DocumentID), LocalScopeID: owner.Session.Open.Session.Scope.LocalScopeID,
				AuthoringCapabilities: append([]semantic.AuthoringCapability(nil), preview.PreviewEvaluation.AuthoringImpact.RequiredCapabilities...), Permissions: accesscore.AgentPermissions{Read: true, Propose: true, Apply: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
			})
			if err != nil {
				t.Fatal(err)
			}
			delegated, err := host.OpenDelegatedDocument(context.Background(), batch.DocumentID, record.ID)
			if err != nil {
				t.Fatal(err)
			}
			journal, _ := externalRecoveryFixture(t, host, delegated.Session, source, "delegated_"+test.name, runtimeprotocol.RecoveryPhaseExternalPending)
			if test.revoke {
				if err := host.RevokeDelegation(record.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := host.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			restarted := newTestHost(t, dataRoot, func(config *Config) {
				if test.expired {
					config.Clock = &fakeClock{now: now.Add(2 * time.Hour)}
				}
			})
			results, recoverErr := restarted.Recover(context.Background(), journal.Scope.DocumentID)
			inspection, err := restarted.external.Inspect(context.Background(), port.InspectExternalFileInput{Scope: journal.Scope, OperationID: journal.Status.OperationID, IdempotencyKey: journal.Status.IdempotencyKey})
			if !test.revoke && !test.expired {
				if recoverErr != nil || len(results) != 1 || err != nil || inspection.Receipt == nil {
					t.Fatalf("live delegated recovery=%+v recoverErr=%v inspection=%+v inspectErr=%v", results, recoverErr, inspection, err)
				}
			} else {
				if !errors.Is(recoverErr, accesscore.ErrGrantStale) {
					t.Fatalf("unauthorized external recovery=%v", recoverErr)
				}
				if err != nil || inspection.Stage == nil || inspection.Receipt != nil {
					t.Fatalf("external publication escaped authorization: inspection=%+v err=%v", inspection, err)
				}
			}
		})
	}
}

func externalRecoveryFixture(t *testing.T, host *Host, session *Session, source, suffix string, phase runtimeprotocol.RecoveryPhase) (port.RecoveryRecord, port.StagedRevision) {
	t.Helper()
	ctx := context.Background()
	record, staged := stageRecoveryFixture(t, host, session, source, suffix, false, false)
	var encoded []byte
	for _, blob := range stagedInputFor(t, host, record.Scope, record.Status.OperationID).SourceBlobs.Blobs {
		if blob.Ref == stagedInputFor(t, host, record.Scope, record.Status.OperationID).Manifest {
			encoded = blob.Contents
			break
		}
	}
	if encoded == nil {
		t.Fatal("staged Engine source is unavailable")
	}
	candidate, err := host.engine.ReadEncodedInput(ctx, encoded)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]port.ExternalProjectFile, 0, len(candidate.ProjectSourceTree()))
	for path, contents := range candidate.ProjectSourceTree() {
		files = append(files, port.ExternalProjectFile{Path: path, Contents: contents})
	}
	externalHead, err := host.external.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: record.Scope})
	if err != nil {
		t.Fatal(err)
	}
	externalStage, err := host.external.Prepare(ctx, port.PrepareExternalFileInput{Scope: record.Scope, OperationID: record.Status.OperationID, IdempotencyKey: record.Status.IdempotencyKey, RevisionID: staged.Revision.RevisionID, ExpectedProviderVersion: externalHead.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: files}})
	if err != nil {
		t.Fatal(err)
	}
	record, err = host.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: record.Scope, OperationID: record.Status.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhaseStaged, NextPhase: runtimeprotocol.RecoveryPhasePublicationPending, ExternalStage: &externalStage, ExpectedExternalProviderVersion: &externalHead.ProviderVersion})
	if err != nil {
		t.Fatal(err)
	}
	documentHead, err := host.documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: record.Scope})
	if err != nil {
		t.Fatal(err)
	}
	published, err := host.documents.PublishHead(ctx, port.PublishDocumentHeadInput{Scope: record.Scope, StageID: staged.StageID, ExpectedRevision: documentHead.Revision.RevisionID, ExpectedDefinitionHash: documentHead.Revision.DefinitionHash, ExpectedProviderVersion: documentHead.ProviderVersion, FencingToken: documentHead.FencingToken})
	if err != nil || !published.Published {
		t.Fatalf("publish external recovery fixture=%+v err=%v", published, err)
	}
	if phase == runtimeprotocol.RecoveryPhasePublicationPending {
		return record, staged
	}
	record, err = host.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: record.Scope, OperationID: record.Status.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePublicationPending, NextPhase: runtimeprotocol.RecoveryPhasePublished, PublishedRevision: &published.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if phase == runtimeprotocol.RecoveryPhasePublished {
		return record, staged
	}
	record, err = host.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: record.Scope, OperationID: record.Status.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePublished, NextPhase: runtimeprotocol.RecoveryPhaseExternalPending, PublishedRevision: &published.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if phase == runtimeprotocol.RecoveryPhaseExternalPending {
		return record, staged
	}
	if phase == runtimeprotocol.RecoveryPhaseExternalFailed {
		failure := runtimeprotocol.ExternalMaterializationFailureConflict
		record, err = host.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: record.Scope, OperationID: record.Status.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhaseExternalPending, NextPhase: phase, PublishedRevision: &published.Revision, ExternalFailure: &failure})
	} else {
		receipt, publishErr := host.external.Publish(ctx, port.PublishExternalFileInput{Scope: record.Scope, OperationID: record.Status.OperationID, IdempotencyKey: record.Status.IdempotencyKey, StageID: externalStage.StageID, ExpectedProviderVersion: externalHead.ProviderVersion})
		if publishErr != nil {
			t.Fatal(publishErr)
		}
		record, err = host.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: record.Scope, OperationID: record.Status.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhaseExternalPending, NextPhase: runtimeprotocol.RecoveryPhaseExternalPublished, PublishedRevision: &published.Revision, ExternalReceipt: &receipt})
	}
	if err != nil {
		t.Fatal(err)
	}
	return record, staged
}

func stagedInputFor(t *testing.T, host *Host, scope runtimeprotocol.RuntimeScope, operation runtimeprotocol.OperationID) port.StageRevisionInput {
	t.Helper()
	stages, err := host.documents.ListStaged(context.Background(), scope, 16)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range stages {
		if stage.Input.OperationID == operation {
			return stage.Input
		}
	}
	t.Fatal("staged revision input not found")
	return port.StageRevisionInput{}
}

func TestRecoveryConvergesEveryExternalPublicationPhase(t *testing.T) {
	for _, fault := range localPersistenceFaults(t, "external_recovery_phase") {
		t.Run(fault.ID, func(t *testing.T) {
			phase := runtimeprotocol.RecoveryPhase(fault.Phase)
			root := t.TempDir()
			source := "project p \"P\" {}\n"
			project := writeProject(t, root, source)
			host := newTestHost(t, filepath.Join(root, "data"), nil)
			opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
			if err != nil {
				t.Fatal(err)
			}
			record, _ := externalRecoveryFixture(t, host, opened.Session, source, strings.ReplaceAll(string(phase), "_", ""), phase)
			results, err := host.Recover(context.Background(), record.Scope.DocumentID)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Status.Phase != runtimeprotocol.RecoveryPhaseFinal || results[0].Status.OperationResult == nil || results[0].Status.OperationResult.ExternalMaterialization == nil {
				t.Fatalf("recovery result=%+v", results)
			}
			external := results[0].Status.OperationResult.ExternalMaterialization
			wantState := runtimeprotocol.ExternalMaterializationState(fault.ExpectedExternalState)
			if external.State != wantState {
				t.Fatalf("external state=%s want=%s", external.State, wantState)
			}
			if results[0].Status.OperationResult.Status != runtimeprotocol.OperationResultStatus(fault.ExpectedStatus) {
				t.Fatalf("state duty precedence=%s", results[0].Status.OperationResult.Status)
			}
			if phase == runtimeprotocol.RecoveryPhaseExternalFailed {
				_, inspectErr := host.external.Inspect(context.Background(), port.InspectExternalFileInput{Scope: record.Scope, OperationID: record.Status.OperationID, IdempotencyKey: record.Status.IdempotencyKey})
				if !errors.Is(inspectErr, port.ErrNotFound) {
					t.Fatalf("failed external stage was retained: %v", inspectErr)
				}
			}
		})
	}
}

func TestRecoveryReconcilesExternalConflictAndRetryableReceiptFailure(t *testing.T) {
	t.Run("conflict", func(t *testing.T) {
		root := t.TempDir()
		source := "project p \"P\" {}\n"
		project := writeProject(t, root, source)
		host := newTestHost(t, filepath.Join(root, "data"), nil)
		opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
		if err != nil {
			t.Fatal(err)
		}
		record, _ := externalRecoveryFixture(t, host, opened.Session, source, "external_conflict_recovery", runtimeprotocol.RecoveryPhaseExternalPending)
		if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project p \"External\" {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		results, err := host.Recover(context.Background(), record.Scope.DocumentID)
		if err != nil || len(results) != 1 || results[0].Status.OperationResult == nil || results[0].Status.OperationResult.ExternalMaterialization == nil || results[0].Status.OperationResult.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStateFailed {
			t.Fatalf("conflict recovery=%+v err=%v", results, err)
		}
	})

	t.Run("receipt retry", func(t *testing.T) {
		root := t.TempDir()
		source := "project p \"P\" {}\n"
		project := writeProject(t, root, source)
		host := newTestHost(t, filepath.Join(root, "data"), nil)
		opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
		if err != nil {
			t.Fatal(err)
		}
		record, _ := externalRecoveryFixture(t, host, opened.Session, source, "external_receipt_retry", runtimeprotocol.RecoveryPhaseExternalPending)
		injected := errors.New("receipt unavailable")
		faulting, err := local.NewExternalFileStore(host.config.Root, local.ExternalFileOptions{Fault: func(point string) error {
			if point == "before_external_receipt" {
				return injected
			}
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		host.external = faulting
		if _, err := host.Recover(context.Background(), record.Scope.DocumentID); !errors.Is(err, injected) {
			t.Fatalf("retryable external error=%v", err)
		}
		clean, err := local.NewExternalFileStore(host.config.Root, local.ExternalFileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		host.external = clean
		results, err := host.Recover(context.Background(), record.Scope.DocumentID)
		if err != nil || len(results) != 1 || results[0].Status.OperationResult == nil || results[0].Status.OperationResult.ExternalMaterialization == nil || results[0].Status.OperationResult.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStatePublished {
			t.Fatalf("receipt retry recovery=%+v err=%v", results, err)
		}
	})
}

func TestSourceBaselineHelpersFailClosedAndPersistCanonicalDigest(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root, "project p \"P\" {}\n")
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.acceptSessionSourceBaseline(nil, digestJSON("valid")); err == nil {
		t.Fatal("nil session baseline was accepted")
	}
	if err := host.acceptSessionSourceBaseline(opened.Session, "invalid"); err == nil {
		t.Fatal("invalid session digest was accepted")
	}
	if err := host.acceptDocumentSourceBaseline(opened.Session.Open.Session.Scope.DocumentID, "invalid"); err == nil {
		t.Fatal("invalid document digest was accepted")
	}
	if err := host.acceptDocumentSourceBaseline("doc_missing_baseline", digestJSON("missing")); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("missing baseline=%v", err)
	}
	next := digestJSON("next baseline")
	if err := host.acceptDocumentSourceBaseline(opened.Session.Open.Session.Scope.DocumentID, next); err != nil {
		t.Fatal(err)
	}
	if err := host.acceptDocumentSourceBaseline(opened.Session.Open.Session.Scope.DocumentID, next); err != nil {
		t.Fatalf("idempotent baseline=%v", err)
	}
	reloaded, err := host.loadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range reloaded.Bindings {
		if binding.DocumentID == opened.Session.Open.Session.Scope.DocumentID {
			found = binding.SourceDigest == next
		}
	}
	if !found {
		t.Fatalf("baseline was not persisted: %+v", reloaded)
	}
	priorRoot := host.config.Root
	prior := next
	failed := digestJSON("failed persistence")
	host.config.Root = filepath.Join(root, "missing-metadata-root")
	if err := host.acceptDocumentSourceBaseline(opened.Session.Open.Session.Scope.DocumentID, failed); err == nil {
		t.Fatal("metadata persistence failure was hidden")
	}
	sessionFailed := digestJSON("failed session persistence")
	if err := host.acceptSessionSourceBaseline(opened.Session, sessionFailed); err == nil {
		t.Fatal("session metadata persistence failure was hidden")
	}
	if opened.Session.SourceDigest != sessionFailed {
		t.Fatalf("published session baseline was discarded: %s", opened.Session.SourceDigest)
	}
	host.config.Root = priorRoot
	for _, binding := range host.metadata.Bindings {
		if binding.DocumentID == opened.Session.Open.Session.Scope.DocumentID && binding.SourceDigest != prior {
			t.Fatalf("failed metadata update was not rolled back: %+v", binding)
		}
	}
	if _, ok := host.workbench.SourceDigest("missing-working-handle"); ok {
		t.Fatal("missing working handle exposed a source baseline")
	}
	if err := host.ScheduleAutosave(context.Background(), SaveInput{}, nil); err == nil {
		t.Fatal("nil-session autosave was accepted")
	}
	if _, err := host.CancelOperation(context.Background(), nil, "operation_nil_session", "cancel_nil_session"); err == nil {
		t.Fatal("nil-session cancellation was accepted")
	}
	if _, err := host.GetOperationStatus(context.Background(), nil, "operation_nil_session"); err == nil {
		t.Fatal("nil-session status lookup was accepted")
	}
	if err := host.Close(context.Background(), nil); err != nil {
		t.Fatalf("nil close=%v", err)
	}
	if err := host.Close(context.Background(), opened.Session); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(context.Background(), opened.Session); err != nil {
		t.Fatalf("idempotent close=%v", err)
	}
	if err := host.ScheduleAutosave(context.Background(), SaveInput{Session: opened.Session}, nil); err == nil {
		t.Fatal("closed-session autosave was accepted")
	}
	if err := host.acceptSessionSourceBaseline(opened.Session, digestJSON("closed")); err == nil {
		t.Fatal("closed session baseline was accepted")
	}
}

func TestRecoveryRejectsPrepublicationAndFinishesPublishedPhases(t *testing.T) {
	for _, fault := range localPersistenceFaults(t, "local_recovery_phase") {
		t.Run(fault.ID, func(t *testing.T) {
			root := t.TempDir()
			source := "project p \"P\" {}\n"
			project := writeProject(t, root, source)
			host := newTestHost(t, filepath.Join(root, "data"), nil)
			opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
			if err != nil {
				t.Fatal(err)
			}
			record, _ := stageRecoveryFixture(t, host, opened.Session, source, fault.ID, fault.Publish, fault.AdvanceAfterPublish)
			phase := runtimeprotocol.RecoveryPhasePublished
			head, _ := host.documents.GetHead(context.Background(), port.GetDocumentHeadInput{Scope: record.Scope})
			for i := 0; i < fault.ExtraAdvances; i++ {
				next := map[runtimeprotocol.RecoveryPhase]runtimeprotocol.RecoveryPhase{runtimeprotocol.RecoveryPhasePublished: runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseStatePending: runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseAuditPending: runtimeprotocol.RecoveryPhaseOutboxReady}[phase]
				updated, err := host.recovery.Advance(context.Background(), port.AdvanceRecoveryRecordInput{Scope: record.Scope, OperationID: record.Status.OperationID, ExpectedPhase: phase, NextPhase: next, PublishedRevision: &head.Revision})
				if err != nil {
					t.Fatal(err)
				}
				record = updated
				phase = next
			}
			if record.Status.Phase != runtimeprotocol.RecoveryPhase(fault.Phase) {
				t.Fatalf("fixture phase=%s want=%s", record.Status.Phase, fault.Phase)
			}
			results, err := host.Recover(context.Background(), record.Scope.DocumentID)
			if err != nil {
				t.Fatal(err)
			}
			want := runtimeprotocol.OperationResultStatus(fault.ExpectedStatus)
			if len(results) != 1 || results[0].Status.OperationResult == nil || results[0].Status.OperationResult.Status != want {
				t.Fatalf("results = %+v", results)
			}
			again, err := host.Recover(context.Background(), record.Scope.DocumentID)
			wantAgain := 0
			if want == runtimeprotocol.OperationResultStatusNeedsReview {
				wantAgain = 1
			}
			if err != nil || len(again) != wantAgain || (wantAgain == 1 && !again[0].Converged) {
				t.Fatalf("repeat recovery = %+v %v", again, err)
			}
		})
	}
}

func TestPublicationPendingSupersededHeadNeedsReview(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	record, candidate := stageRecoveryFixture(t, host, opened.Session, source, "superseded", true, false)
	head, err := host.documents.GetHead(context.Background(), port.GetDocumentHeadInput{Scope: record.Scope})
	if err != nil || !sameStagedHead(head.Revision, candidate.Revision) {
		t.Fatalf("candidate head=%+v err=%v", head, err)
	}
	inspected, err := host.documents.ListStaged(context.Background(), record.Scope, 2)
	if err != nil || len(inspected) != 1 {
		t.Fatalf("candidate stage=%+v err=%v", inspected, err)
	}
	laterInput := inspected[0].Input
	laterInput.OperationID = "later_operation"
	laterInput.IdempotencyKey = "later_idempotency_value"
	laterInput.BaseRevision = head.Revision
	laterInput.SourceBlobs.Revision = head.Revision
	laterInput.PreviewEvaluation = nil
	later, err := host.documents.StageRevision(context.Background(), laterInput)
	if err != nil {
		t.Fatal(err)
	}
	published, err := host.documents.PublishHead(context.Background(), port.PublishDocumentHeadInput{Scope: record.Scope, StageID: later.StageID, ExpectedRevision: head.Revision.RevisionID, ExpectedDefinitionHash: head.Revision.DefinitionHash, ExpectedProviderVersion: head.ProviderVersion, FencingToken: head.FencingToken})
	if err != nil || !published.Published {
		t.Fatalf("later publish=%+v err=%v", published, err)
	}
	results, err := host.Recover(context.Background(), record.Scope.DocumentID)
	if err != nil || len(results) != 1 || results[0].Status.OperationResult == nil || results[0].Status.OperationResult.Status != runtimeprotocol.OperationResultStatusNeedsReview {
		t.Fatalf("superseded recovery=%+v err=%v", results, err)
	}
	stages, err := host.documents.ListStaged(context.Background(), record.Scope, 2)
	if err != nil || len(stages) != 1 || stages[0].Input.OperationID != record.Status.OperationID {
		t.Fatalf("review evidence=%+v err=%v", stages, err)
	}
}

func TestBoundsCancellationAndCorruptionDiagnostics(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	host := newTestHost(t, filepath.Join(root, "data"), func(c *Config) { c.MaxSessions = 1 })
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	cancelledRecovery, cancelRecovery := context.WithCancel(context.Background())
	cancelRecovery()
	if _, err := host.Recover(cancelledRecovery, opened.Session.Open.Session.Scope.DocumentID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery=%v", err)
	}
	if previewFor(port.RecoveryRecord{}, local.StagedInspection{}, false) != nil {
		t.Fatal("missing preview evidence was synthesized")
	}
	if _, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project}); err == nil {
		t.Fatal("session bound ignored")
	}
	if _, err := host.OpenDocument(context.Background(), opened.Session.Open.Session.Scope.DocumentID); err == nil {
		t.Fatal("document-id open ignored session bound")
	}
	cancelled, err := host.CancelOperation(context.Background(), opened.Session, "not_pending", "cancellation_token_1234")
	if err != nil || cancelled.Status != "not_pending" {
		t.Fatalf("cancel = %+v %v", cancelled, err)
	}
	if err := host.Close(context.Background(), opened.Session); err != nil {
		t.Fatal(err)
	}
	if _, err := host.GetOperationStatus(context.Background(), opened.Session, "missing"); err == nil {
		t.Fatal("closed session accepted")
	}
	if _, err := host.OpenProject(context.Background(), OpenProjectInput{Root: filepath.Join(root, "missing")}); err == nil {
		t.Fatal("missing project accepted")
	}
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := host.OpenProject(context.Background(), OpenProjectInput{Root: empty}); err == nil {
		t.Fatal("empty project accepted")
	}
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project}); err == nil {
		t.Fatal("shutdown host accepted open")
	}
	if err := os.WriteFile(filepath.Join(root, "data", "local-document-bindings.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Root: filepath.Join(root, "data")}); err == nil {
		t.Fatal("corrupt bindings accepted")
	}
}

func TestDefaultBoundariesAndRejectedInputs(t *testing.T) {
	if (systemClock{}).Now().IsZero() {
		t.Fatal("system clock returned zero")
	}
	fired := make(chan struct{}, 1)
	cancel := (timerScheduler{}).Schedule(time.Millisecond, func() { fired <- struct{}{} })
	defer cancel()
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("timer scheduler did not fire")
	}
	if _, err := New(Config{Root: "relative"}); err == nil {
		t.Fatal("relative storage root accepted")
	}
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	if _, err := host.OpenDocument(context.Background(), "missing"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("missing document=%v", err)
	}
	if _, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project, EntryPath: "missing.ldl"}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("missing entry = %v", err)
	}
	if _, err := host.Save(context.Background(), SaveInput{}); err == nil {
		t.Fatal("nil save session accepted")
	}
	if err := host.ScheduleAutosave(context.Background(), SaveInput{}, nil); err == nil {
		t.Fatal("nil autosave session accepted")
	}
	if _, err := host.CancelOperation(context.Background(), nil, "op", "cancellation_token_1234"); err == nil {
		t.Fatal("nil cancellation session accepted")
	}
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Save(context.Background(), SaveInput{Session: opened.Session, Operations: createLayerBatch(t, "badtrigger"), Preconditions: allPreconditions(t, source), Trigger: runtimeprotocol.CommitTriggerRestore}); err == nil {
		t.Fatal("unsupported local save trigger accepted")
	}
	if cloned := cloneByteMap(map[string][]byte{"x": []byte("y")}); string(cloned["x"]) != "y" {
		t.Fatal("byte map clone failed")
	}
	if _, ok := host.workbench.Working("missing", opened.Session.Open.CommittedRevision); ok {
		t.Fatal("missing workbench handle resolved")
	}
	if _, ok := host.workbench.Opened(runtimeprotocol.CommittedRevisionRef{DocumentID: "missing", RevisionID: "missing"}); ok {
		t.Fatal("missing revision resolved")
	}
	automatic, err := host.Save(context.Background(), SaveInput{Session: opened.Session, Operations: createLayerBatch(t, "automatic"), Preconditions: allPreconditions(t, source)})
	if err != nil || automatic.OperationResult.OperationID == "" || automatic.OperationResult.IdempotencyKey == "" {
		t.Fatalf("automatic identities = %+v %v", automatic, err)
	}
	if _, err := host.GetOperationStatus(context.Background(), nil, "op"); err == nil {
		t.Fatal("nil status session accepted")
	}
	link := filepath.Join(project, "linked.ldl")
	if err := os.Symlink(filepath.Join(project, "document.ldl"), link); err == nil {
		if _, err := readProjectTree(context.Background(), project, 10, 1024); err == nil {
			t.Fatal("project symlink accepted")
		}
		_ = os.Remove(link)
	}
	if _, err := readProjectTree(context.Background(), project, 0, 1); err == nil {
		t.Fatal("project bounds ignored")
	}
	denied := accessprotocol.EvaluateAuthoringInput{GrantSnapshot: accessprotocol.AuthoringGrantSnapshot{AccessFingerprint: digestJSON("scope"), ActorRef: accessprotocol.ActorRef{ActorID: "x", Kind: "user"}, GrantedCapabilities: []semantic.AuthoringCapability{}, HostDocumentID: "doc", IssuedAt: "2026-07-19T00:00:00Z", LocalScopeID: "local", MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{}}, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "apply", AuthoringImpact: &semantic.AuthoringImpact{BaseDefinitionHash: digestJSON("a"), ResultingDefinitionHash: digestJSON("b"), SemanticDiffHash: digestJSON("c"), SourceDiffHash: digestJSON("d"), ImpactDigest: digestJSON("e"), Entries: []semantic.AuthoringImpactEntry{}, RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}}}
	decision, err := host.authority.Evaluate(context.Background(), denied)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeDeny || len(decision.MissingCapabilities) != 1 {
		t.Fatalf("deny = %+v %v", decision, err)
	}
}

func TestConfiguredLocalActorIsStableAcrossAuthorityRestart(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)}
	actor := accessprotocol.ActorRef{ActorID: "platform-user-501", Kind: "user"}
	first := newLocalAuthorityForActor(clock, nil, actor)
	firstScope := first.add("doc_actor")
	firstGrant, _, err := first.ResolveGrant(context.Background(), firstScope)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Hour)
	second := newLocalAuthorityForActor(clock, nil, actor)
	secondScope := second.add("doc_actor")
	secondGrant, _, err := second.ResolveGrant(context.Background(), secondScope)
	if err != nil {
		t.Fatal(err)
	}
	if firstScope != secondScope || firstGrant.ActorRef != actor || secondGrant.ActorRef != actor || firstGrant.ActorRef.Kind != "user" {
		t.Fatalf("restart actor/scope = %+v %+v %+v %+v", firstScope, secondScope, firstGrant, secondGrant)
	}
	if firstGrant.OrganizationScopeID != nil || secondGrant.OrganizationScopeID != nil {
		t.Fatal("local actor fabricated organization membership")
	}
	legacy := newLocalAuthority(clock, nil).add("doc_actor")
	wantLegacy := digestJSON(struct {
		Document runtimeprotocol.DocumentID `json:"document"`
	}{"doc_actor"})
	if legacy.AccessFingerprint != wantLegacy || legacy.AccessFingerprint == firstScope.AccessFingerprint {
		t.Fatalf("legacy/configured fingerprints = %s %s", legacy.AccessFingerprint, firstScope.AccessFingerprint)
	}
}

func TestContainerExternalChangeAndAdditionalRecoveryStates(t *testing.T) {
	t.Run("container external", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "d.layerdraw")
		instance := engine.New(engine.BuildInfo{})
		first, _ := instance.WriteLayerdraw(context.Background(), engine.LayerdrawWriteInput{CompileInput: projectInput("project p \"P\" {}\n")})
		if err := os.WriteFile(path, first, 0o600); err != nil {
			t.Fatal(err)
		}
		host := newTestHost(t, filepath.Join(root, "data"), nil)
		opened, err := host.OpenContainer(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		second, _ := instance.WriteLayerdraw(context.Background(), engine.LayerdrawWriteInput{CompileInput: projectInput("project p \"Changed\" {}\n")})
		if err := os.WriteFile(path, second, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := host.Save(context.Background(), SaveInput{Session: opened.Session, Operations: createLayerBatch(t, "x"), Preconditions: allPreconditions(t, "project p \"P\" {}\n")}); !errors.Is(err, port.ErrConflict) {
			t.Fatalf("container external = %v", err)
		}
	})
	for _, test := range []struct {
		name    string
		prepare func(*Host, *Session, port.RecoveryRecord, port.StagedRevision)
	}{
		{"missing stage", func(host *Host, session *Session, record port.RecoveryRecord, stage port.StagedRevision) {
			_ = host.documents.AbortStagedRevision(context.Background(), port.AbortStagedRevisionInput{Scope: record.Scope, StageID: stage.StageID})
		}},
		{"recovering", func(host *Host, _ *Session, record port.RecoveryRecord, _ port.StagedRevision) {
			_, err := host.recovery.Advance(context.Background(), port.AdvanceRecoveryRecordInput{Scope: record.Scope, OperationID: record.Status.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePublicationPending, NextPhase: runtimeprotocol.RecoveryPhaseRecovering})
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := "project p \"P\" {}\n"
			project := writeProject(t, root, source)
			host := newTestHost(t, filepath.Join(root, "data"), nil)
			opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
			if err != nil {
				t.Fatal(err)
			}
			record, stage := stageRecoveryFixture(t, host, opened.Session, source, strings.ReplaceAll(test.name, " ", "_"), false, true)
			test.prepare(host, opened.Session, record, stage)
			results, err := host.Recover(context.Background(), record.Scope.DocumentID)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Status.OperationResult == nil {
				t.Fatalf("results=%+v", results)
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestFilesystemAndFailureBranchesStayBounded(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	if _, err := canonicalLocalPath(project, false); err == nil {
		t.Fatal("directory accepted as container")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readProjectTree(ctx, project, 10, 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled tree=%v", err)
	}
	fileRoot := filepath.Join(root, "file-root")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Root: fileRoot}); err == nil {
		t.Fatal("file storage root accepted")
	}
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	if _, err := host.OpenContainer(context.Background(), filepath.Join(root, "missing.layerdraw")); err == nil {
		t.Fatal("missing container accepted")
	}
	if _, err := host.authority.NewID(context.Background(), port.IdentityOperation); err != nil {
		t.Fatal(err)
	}
	broken := newLocalAuthority(host.config.Clock, failingReader{})
	if _, err := broken.NewID(context.Background(), port.IdentityOperation); err == nil {
		t.Fatal("entropy failure hidden")
	}
	if _, err := broken.ResolveScope(context.Background(), "missing"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("scope=%v", err)
	}
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	scheduler := &fakeScheduler{}
	host.config.Scheduler = scheduler
	results := make(chan AutosaveResult, 1)
	first := SaveInput{Session: opened.Session, Operations: createLayerBatch(t, "first"), Preconditions: allPreconditions(t, source), OperationID: "coalesce_first", IdempotencyKey: "idempotency_coalesce_first"}
	second := first
	second.OperationID = "coalesce_second"
	second.IdempotencyKey = "idempotency_coalesce_second"
	if err := host.ScheduleAutosave(context.Background(), first, results); err != nil {
		t.Fatal(err)
	}
	if err := host.ScheduleAutosave(context.Background(), second, results); err != nil {
		t.Fatal(err)
	}
	scheduler.fireLast()
	if value := <-results; value.Err != nil {
		t.Fatal(value.Err)
	}
	scheduler.mu.Lock()
	if !scheduler.jobs[0].cancelled {
		t.Fatal("prior autosave not cancelled")
	}
	scheduler.mu.Unlock()
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project q \"Q\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project}); err == nil {
		t.Fatal("portable identity change accepted")
	}
}

func TestRecoveryWithUnreadableHeadConvergesNeedsReview(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	record, _ := stageRecoveryFixture(t, host, opened.Session, source, "head_unreadable", false, true)
	var headPath string
	_ = filepath.Walk(filepath.Join(root, "data"), func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && info.Name() == "head.json" && strings.Contains(path, "documents") {
			headPath = path
		}
		return nil
	})
	if headPath == "" {
		t.Fatal("head path unavailable")
	}
	if err := os.WriteFile(headPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.OpenDocument(context.Background(), record.Scope.DocumentID); err == nil {
		t.Fatal("corrupt head reopened")
	}
	results, err := host.Recover(context.Background(), record.Scope.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status.OperationResult == nil || results[0].Status.OperationResult.Status != runtimeprotocol.OperationResultStatusNeedsReview {
		t.Fatalf("results=%+v", results)
	}
}

func TestDirectAmbiguousPublishedEvidenceAndMetadataFailure(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	dataRoot := filepath.Join(root, "data")
	host := newTestHost(t, dataRoot, nil)
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	record, stage := stageRecoveryFixture(t, host, opened.Session, source, "ambiguous_published", true, true)
	status, err := host.recoverPublished(context.Background(), record.Scope, record, nil, local.StagedInspection{Stage: stage}, true)
	if err != nil || status.OperationResult == nil || status.OperationResult.Status != runtimeprotocol.OperationResultStatusNeedsReview {
		t.Fatalf("ambiguous=%+v %v", status, err)
	}
	if _, err := host.recoverPublished(context.Background(), record.Scope, port.RecoveryRecord{Status: runtimeprotocol.RuntimeOperationStatus{Phase: runtimeprotocol.RecoveryPhaseOutboxReady}}, nil, local.StagedInspection{}, false); !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("outbox ambiguity=%v", err)
	}
	defaultAuthority := newLocalAuthority(host.config.Clock, nil)
	if _, err := defaultAuthority.NewID(context.Background(), port.IdentityOperation); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dataRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dataRoot, 0o700)
	host.mu.Lock()
	err = host.saveMetadataLocked()
	host.mu.Unlock()
	if err == nil {
		t.Fatal("metadata write permission failure hidden")
	}
}
