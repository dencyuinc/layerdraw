// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  isCapabilityManifest,
  isDigest,
  type CapabilityManifest,
  type Digest,
} from "@layerdraw/protocol/common";
import {
  encodeViewData,
  isViewDataStateInputRef,
  isViewData,
  isViewRevision,
  type ViewData,
  type ViewDataItemKey,
  type ViewDataSourceRefs,
  type ViewDataStateInputRef,
  type ViewRevision,
} from "@layerdraw/protocol/semantic";
import {
  assertRenderRecipe,
  materializeRenderData,
  RenderContractError,
  type RenderData,
  type RenderLayoutPolicy,
  type RenderMaterializationDiagnostic,
  type RenderRecipe,
  type RenderResourceLimits,
  type RenderShape,
  type RenderSourceBinding,
  type ResolvedAssetDimensions,
  type ResolvedFontMetrics,
  type ResolvedRendererProfile,
} from "@layerdraw/render";

const shapes: readonly RenderShape[] = [
  "context",
  "diagram",
  "diff",
  "flow",
  "matrix",
  "table",
  "tree",
];

export type ViewerJsonValue =
  | null
  | boolean
  | string
  | number
  | ViewerJsonArray
  | ViewerJsonObject;
export interface ViewerJsonArray extends ReadonlyArray<ViewerJsonValue> {}
export interface ViewerJsonObject {
  readonly [key: string]: ViewerJsonValue;
}

export interface ViewerSnapshot {
  readonly viewer_snapshot_schema_version: 1;
  readonly sequence: number;
  readonly complete: boolean;
  readonly view_address: string;
  readonly revision: ViewRevision;
  readonly view_data_hash: Digest;
  readonly state_input: ViewDataStateInputRef;
  readonly view_data: ViewData;
}

export interface ViewDataUpdate {
  readonly viewer_update_schema_version: 1;
  readonly sequence: number;
  readonly previous_sequence: number;
  readonly complete: boolean;
  readonly view_address: string;
  readonly previous_revision: ViewRevision;
  readonly revision: ViewRevision;
  readonly previous_view_data_hash: Digest;
  readonly view_data_hash: Digest;
  readonly previous_state_input: ViewDataStateInputRef;
  readonly state_input: ViewDataStateInputRef;
  readonly view_data: ViewData;
}

export interface ViewerPresentationState {
  readonly selection_keys: readonly string[];
  readonly hover_key?: string;
  readonly focus_key?: string;
  readonly zoom: number;
  readonly pan: Readonly<{ x: number; y: number }>;
  readonly expanded_keys: readonly string[];
  readonly sorting: readonly Readonly<{
    render_key: string;
    direction: "ascending" | "descending";
  }>[];
  readonly display_preferences: Readonly<Record<string, ViewerJsonValue>>;
}

export type ViewerPresentationUpdate = Readonly<{
  selection_keys?: readonly string[];
  hover_key?: string | null;
  focus_key?: string | null;
  zoom?: number;
  pan?: Readonly<{ x: number; y: number }>;
  expanded_keys?: readonly string[];
  sorting?: readonly Readonly<{
    render_key: string;
    direction: "ascending" | "descending";
  }>[];
  display_preferences?: Readonly<Record<string, ViewerJsonValue>>;
}>;

export interface ViewerPublication {
  readonly sequence: number;
  readonly complete: boolean;
  readonly view_address: string;
  readonly revision: ViewRevision;
  readonly view_data_hash: Digest;
  readonly state_input: ViewDataStateInputRef;
  readonly view_data: ViewData;
  readonly render_data: RenderData;
  readonly presentation: ViewerPresentationState;
}

export type ViewerErrorCode =
  | "viewer.input_invalid"
  | "viewer.profile_unsupported"
  | "viewer.profile_incompatible"
  | "viewer.asset_missing"
  | "viewer.font_missing"
  | "viewer.update_gap"
  | "viewer.update_stale"
  | "viewer.update_conflict"
  | "viewer.update_mismatch"
  | "viewer.resource_limit"
  | "viewer.resolver_failed"
  | "viewer.render_failed"
  | "viewer.cancelled"
  | "viewer.disposed";

export interface ViewerError {
  readonly code: ViewerErrorCode;
  readonly recoverable: boolean;
  readonly details: Readonly<Record<string, ViewerJsonValue>>;
  readonly render_diagnostics?: readonly RenderMaterializationDiagnostic[];
}

type WithPrevious = Readonly<{ previous?: ViewerPublication }>;
export type ViewerState =
  | Readonly<{ status: "loading"; operation: "snapshot" | "update" }> &
      WithPrevious
  | Readonly<{ status: "ready"; publication: ViewerPublication }>
  | Readonly<{
      status: "empty";
      reason: "no_snapshot" | "view_empty";
      publication?: ViewerPublication;
    }>
  | Readonly<{
      status: "partial_stream";
      publication: ViewerPublication;
    }>
  | Readonly<{ status: "unsupported_profile"; error: ViewerError }> &
      WithPrevious
  | Readonly<{ status: "missing_asset"; error: ViewerError }> & WithPrevious
  | Readonly<{ status: "missing_font"; error: ViewerError }> & WithPrevious
  | Readonly<{ status: "stale_update"; error: ViewerError }> & WithPrevious
  | Readonly<{ status: "recoverable_error"; error: ViewerError }> &
      WithPrevious
  | Readonly<{ status: "fatal"; error: ViewerError }> & WithPrevious
  | Readonly<{ status: "cancelling" }> & WithPrevious
  | Readonly<{ status: "disposed" }>;

export type ViewerOperationResult =
  | Readonly<{
      ok: true;
      outcome: "published" | "duplicate";
      state: ViewerState;
    }>
  | Readonly<{
      ok: false;
      outcome: "rejected" | "cancelled";
      error: ViewerError;
      state: ViewerState;
    }>;

export interface ViewerSourceInspection {
  readonly render_key: string;
  readonly viewdata_key: ViewDataItemKey;
  readonly view_data_item: Readonly<Record<string, unknown>>;
  readonly source_binding: RenderSourceBinding &
    Readonly<{
      viewdata_key: ViewDataItemKey;
      source_refs: ViewDataSourceRefs;
    }>;
}

export interface ViewerResourceLimits {
  readonly max_queued_updates: number;
  readonly max_snapshot_bytes: number;
  readonly max_snapshot_items: number;
  readonly max_render_items: number;
  readonly max_retained_bytes: number;
  readonly max_presentation_references: number;
  readonly max_display_preferences: number;
  readonly max_display_preference_bytes: number;
  readonly max_event_deliveries: number;
  readonly min_zoom: number;
  readonly max_zoom: number;
  readonly max_pan: number;
}

export interface ViewerAssetResolver {
  resolve(
    digests: readonly Digest[],
    signal: AbortSignal
  ): Promise<readonly ResolvedAssetDimensions[]>;
}

export interface ViewerFontResolver {
  resolve(
    families: readonly string[],
    digests: readonly Digest[],
    signal: AbortSignal
  ): Promise<readonly ResolvedFontMetrics[]>;
}

export type ViewerEvent = Readonly<{
  kind: "state_changed";
  state: ViewerState;
}>;

export interface ViewerEventSink {
  emit(event: ViewerEvent): void;
}

export interface ViewerOptions {
  readonly renderer_profile: ResolvedRendererProfile;
  readonly render_recipes: Readonly<Record<RenderShape, RenderRecipe>>;
  readonly asset_resolver: ViewerAssetResolver;
  readonly font_resolver: ViewerFontResolver;
  readonly capability_manifest: CapabilityManifest;
  readonly layout: RenderLayoutPolicy;
  readonly render_limits: RenderResourceLimits;
  readonly viewer_limits: ViewerResourceLimits;
  readonly event_sink?: ViewerEventSink;
}

export interface Viewer {
  setViewData(snapshot: ViewerSnapshot): Promise<ViewerOperationResult>;
  applyViewDataUpdate(update: ViewDataUpdate): Promise<ViewerOperationResult>;
  updatePresentation(update: ViewerPresentationUpdate): ViewerPresentationState;
  setSelection(selectionKeys: readonly string[]): ViewerPresentationState;
  inspectSource(renderKey: string): ViewerSourceInspection | undefined;
  getState(): ViewerState;
  getPublication(): ViewerPublication | undefined;
  cancel(): Promise<void>;
  dispose(): Promise<void>;
}

export class ViewerContractError extends TypeError {
  constructor(readonly code: ViewerErrorCode, message: string) {
    super(message);
  }
}

type InternalPublication = Omit<ViewerPublication, "presentation"> & {
  presentation: ViewerPresentationState;
};

class ViewerImpl implements Viewer {
  private state: ViewerState = { status: "empty", reason: "no_snapshot" };
  private publication: InternalPublication | undefined;
  private chain: Promise<void> = Promise.resolve();
  private currentAbort: AbortController | undefined;
  private generation = 0;
  private queuedMaterializations = 0;
  private disposed = false;
  private lastUpdateFingerprint: string | undefined;
  private delivering = false;
  private pendingEvent: ViewerEvent | undefined;

  constructor(private readonly options: ViewerOptions) {}

  setViewData(snapshot: ViewerSnapshot): Promise<ViewerOperationResult> {
    if (this.disposed) return Promise.resolve(this.disposedResult());
    const generation = this.generation;
    return this.admitMaterialization(() =>
      this.openSnapshot(snapshot, generation)
    );
  }

  applyViewDataUpdate(update: ViewDataUpdate): Promise<ViewerOperationResult> {
    if (this.disposed) return Promise.resolve(this.disposedResult());
    const generation = this.generation;
    return this.admitMaterialization(() =>
      this.applyUpdate(update, generation)
    );
  }

  updatePresentation(update: ViewerPresentationUpdate): ViewerPresentationState {
    try {
      if (this.disposed)
        throw new ViewerContractError("viewer.disposed", "Viewer is disposed");
      const current = this.publication?.presentation ?? defaultPresentation();
      let next = validatePresentation(
        normalizePresentationUpdate(update, current),
        this.options.viewer_limits
      );
      if (this.publication !== undefined) {
        next = clampPresentationToRender(next, this.publication.render_data);
        this.publication = { ...this.publication, presentation: next };
        this.publishPublicationState();
      }
      return clone(next);
    } catch (cause) {
      if (cause instanceof ViewerContractError) throw cause;
      throw new ViewerContractError(
        "viewer.input_invalid",
        "presentation update is invalid"
      );
    }
  }

  setSelection(selectionKeys: readonly string[]): ViewerPresentationState {
    return this.updatePresentation({ selection_keys: selectionKeys });
  }

  inspectSource(renderKey: string): ViewerSourceInspection | undefined {
    const publication = this.publication;
    if (publication === undefined || typeof renderKey !== "string")
      return undefined;
    const binding = publication.render_data.source_bindings.find(
      (candidate) => candidate.render_key === renderKey
    );
    if (
      binding?.viewdata_key === undefined ||
      binding.source_refs === undefined
    )
      return undefined;
    const item = findViewDataItem(publication.view_data, binding.viewdata_key);
    if (item === undefined) return undefined;
    return clone({
      render_key: renderKey,
      viewdata_key: binding.viewdata_key,
      view_data_item: item,
      source_binding: {
        ...binding,
        viewdata_key: binding.viewdata_key,
        source_refs: binding.source_refs,
      },
    });
  }

  getState(): ViewerState {
    return clone(this.state);
  }

  getPublication(): ViewerPublication | undefined {
    return this.publication === undefined ? undefined : clone(this.publication);
  }

  async cancel(): Promise<void> {
    if (this.disposed) return;
    this.generation += 1;
    this.currentAbort?.abort();
    this.publish({
      status: "cancelling",
      ...(this.publication === undefined
        ? {}
        : { previous: clone(this.publication) }),
    });
    await this.chain;
    if (!this.disposed) this.publishPublicationState();
  }

  async dispose(): Promise<void> {
    if (this.disposed) return;
    this.disposed = true;
    this.generation += 1;
    this.currentAbort?.abort();
    this.publication = undefined;
    this.lastUpdateFingerprint = undefined;
    this.publish({ status: "disposed" });
    await this.chain;
  }

  private enqueue(
    operation: () => Promise<ViewerOperationResult>
  ): Promise<ViewerOperationResult> {
    const result = this.chain.then(operation, operation);
    this.chain = result.then(
      () => undefined,
      () => undefined
    );
    return result;
  }

  private admitMaterialization(
    operation: () => Promise<ViewerOperationResult>
  ): Promise<ViewerOperationResult> {
    if (
      this.queuedMaterializations >=
      this.options.viewer_limits.max_queued_updates
    ) {
      const error = viewerError("viewer.resource_limit", true, {
        resource: "queued_updates",
        limit: this.options.viewer_limits.max_queued_updates,
      });
      this.setFailure("recoverable_error", error);
      return Promise.resolve(this.rejected(error));
    }
    this.queuedMaterializations += 1;
    return this.enqueue(async () => {
      try {
        return await operation();
      } finally {
        this.queuedMaterializations -= 1;
      }
    });
  }

  private async openSnapshot(
    input: ViewerSnapshot,
    generation: number
  ): Promise<ViewerOperationResult> {
    if (this.cancelled(generation)) return this.cancelledResult();
    const checked = validateSnapshot(input, this.options.viewer_limits);
    if (!checked.ok) {
      this.setFailure("recoverable_error", checked.error);
      return this.rejected(checked.error);
    }
    this.publish({
      status: "loading",
      operation: "snapshot",
      ...(this.publication === undefined
        ? {}
        : { previous: clone(this.publication) }),
    });
    const result = await this.materialize(checked.value, generation);
    if (!result.ok) return result.result;
    this.commit(result.publication, undefined);
    return this.published();
  }

  private async applyUpdate(
    raw: ViewDataUpdate,
    generation: number
  ): Promise<ViewerOperationResult> {
    if (this.cancelled(generation)) return this.cancelledResult();
    const checked = validateUpdate(raw, this.options.viewer_limits);
    if (!checked.ok) {
      this.setFailure(
        isOrderingOrIdentityError(checked.error)
          ? "stale_update"
          : "recoverable_error",
        checked.error
      );
      return this.rejected(checked.error);
    }
    const update = checked.value;
    const fingerprint = canonical(update);
    const current = this.publication;
    if (current === undefined) {
      const error = viewerError("viewer.update_mismatch", true, {
        reason: "no_snapshot",
      });
      this.setFailure("stale_update", error);
      return this.rejected(error);
    }
    if (update.sequence === current.sequence) {
      if (fingerprint === this.lastUpdateFingerprint) {
        this.publishPublicationState();
        return { ok: true, outcome: "duplicate", state: this.getState() };
      }
      const error = viewerError("viewer.update_conflict", true, {
        sequence: update.sequence,
      });
      this.setFailure("stale_update", error);
      return this.rejected(error);
    }
    if (update.sequence < current.sequence) {
      const error = viewerError("viewer.update_stale", true, {
        current_sequence: current.sequence,
        received_sequence: update.sequence,
      });
      this.setFailure("stale_update", error);
      return this.rejected(error);
    }
    if (
      update.sequence !== current.sequence + 1 ||
      update.previous_sequence !== current.sequence
    ) {
      const error = viewerError("viewer.update_gap", true, {
        current_sequence: current.sequence,
        received_sequence: update.sequence,
        previous_sequence: update.previous_sequence,
      });
      this.setFailure("stale_update", error);
      return this.rejected(error);
    }
    if (
      update.view_address !== current.view_address ||
      !same(update.previous_revision, current.revision) ||
      update.previous_view_data_hash !== current.view_data_hash ||
      !same(update.previous_state_input, current.state_input)
    ) {
      const error = viewerError("viewer.update_mismatch", true, {
        reason: "previous_identity",
        sequence: update.sequence,
      });
      this.setFailure("stale_update", error);
      return this.rejected(error);
    }
    const snapshot: ViewerSnapshot = {
      viewer_snapshot_schema_version: 1,
      sequence: update.sequence,
      complete: update.complete,
      view_address: update.view_address,
      revision: update.revision,
      view_data_hash: update.view_data_hash,
      state_input: update.state_input,
      view_data: update.view_data,
    };
    this.publish({ status: "partial_stream", publication: clone(current) });
    const result = await this.materialize(snapshot, generation);
    if (!result.ok) return result.result;
    this.commit(result.publication, fingerprint);
    return this.published();
  }

  private async materialize(
    snapshot: ViewerSnapshot,
    generation: number
  ): Promise<
    | Readonly<{ ok: true; publication: InternalPublication }>
    | Readonly<{ ok: false; result: ViewerOperationResult }>
  > {
    const recipe = this.options.render_recipes[snapshot.view_data.kind];
    if (
      !this.options.renderer_profile.supported_shapes.includes(
        snapshot.view_data.kind
      )
    ) {
      const error = viewerError("viewer.profile_unsupported", true, {
        shape: snapshot.view_data.kind,
      });
      this.setFailure("unsupported_profile", error);
      return { ok: false, result: this.rejected(error) };
    }
    const controller = new AbortController();
    this.currentAbort = controller;
    try {
      const [fonts, assets] = await Promise.all([
        abortable(
          this.options.font_resolver.resolve(
            clone(recipe.font_policy.families),
            clone(recipe.font_policy.required_digests),
            controller.signal
          ),
          controller.signal
        ),
        abortable(
          this.options.asset_resolver.resolve(
            clone(recipe.asset_policy.required_digests),
            controller.signal
          ),
          controller.signal
        ),
      ]);
      if (this.cancelled(generation) || controller.signal.aborted)
        return { ok: false, result: this.cancelledResult() };
      if (!Array.isArray(fonts) || !Array.isArray(assets)) {
        const error = viewerError("viewer.resolver_failed", true, {
          reason: "non_array_result",
        });
        this.setFailure("recoverable_error", error);
        return { ok: false, result: this.rejected(error) };
      }
      const rendered = await materializeRenderData({
        view_data: snapshot.view_data,
        view_data_hash: snapshot.view_data_hash,
        recipe,
        resolved_profile: this.options.renderer_profile,
        resolved_fonts: clone(fonts),
        resolved_assets: clone(assets),
        layout: this.options.layout,
        limits: this.options.render_limits,
      });
      if (this.cancelled(generation) || controller.signal.aborted)
        return { ok: false, result: this.cancelledResult() };
      if (!rendered.ok) {
        const mapped = mapRenderFailure(rendered.diagnostics);
        this.setFailure(mapped.status, mapped.error);
        return { ok: false, result: this.rejected(mapped.error) };
      }
      if (
        rendered.data.source_bindings.length >
        this.options.viewer_limits.max_render_items
      ) {
        const error = viewerError("viewer.resource_limit", true, {
          resource: "render_items",
          limit: this.options.viewer_limits.max_render_items,
        });
        this.setFailure("recoverable_error", error);
        return { ok: false, result: this.rejected(error) };
      }
      const presentation = reconcilePresentation(
        this.publication,
        rendered.data,
        snapshot.view_address,
        this.options.viewer_limits
      );
      const publication: InternalPublication = {
        sequence: snapshot.sequence,
        complete: snapshot.complete,
        view_address: snapshot.view_address,
        revision: clone(snapshot.revision),
        view_data_hash: snapshot.view_data_hash,
        state_input: clone(snapshot.state_input),
        view_data: clone(snapshot.view_data),
        render_data: clone(rendered.data),
        presentation,
      };
      const retainedBytes = byteLength(canonical(publication));
      if (retainedBytes > this.options.viewer_limits.max_retained_bytes) {
        const error = viewerError("viewer.resource_limit", true, {
          resource: "retained_bytes",
          limit: this.options.viewer_limits.max_retained_bytes,
          actual: retainedBytes,
        });
        this.setFailure("recoverable_error", error);
        return { ok: false, result: this.rejected(error) };
      }
      return { ok: true, publication };
    } catch (cause) {
      if (this.cancelled(generation) || controller.signal.aborted)
        return { ok: false, result: this.cancelledResult() };
      const error = viewerError("viewer.resolver_failed", true, {
        reason: safeReason(cause),
      });
      this.setFailure("recoverable_error", error);
      return { ok: false, result: this.rejected(error) };
    } finally {
      if (this.currentAbort === controller) this.currentAbort = undefined;
    }
  }

  private commit(
    publication: InternalPublication,
    updateFingerprint: string | undefined
  ): void {
    this.publication = publication;
    this.lastUpdateFingerprint = updateFingerprint;
    this.publishPublicationState();
  }

  private publishPublicationState(): void {
    const publication = this.publication;
    if (publication === undefined) {
      if (!this.disposed)
        this.publish({ status: "empty", reason: "no_snapshot" });
      return;
    }
    const cloned = clone(publication);
    if (!publication.complete) {
      this.publish({ status: "partial_stream", publication: cloned });
    } else if (publication.render_data.source_bindings.length === 0) {
      this.publish({
        status: "empty",
        reason: "view_empty",
        publication: cloned,
      });
    } else {
      this.publish({ status: "ready", publication: cloned });
    }
  }

  private setFailure(
    status:
      | "unsupported_profile"
      | "missing_asset"
      | "missing_font"
      | "stale_update"
      | "recoverable_error"
      | "fatal",
    error: ViewerError
  ): void {
    this.publish({
      status,
      error,
      ...(this.publication === undefined
        ? {}
        : { previous: clone(this.publication) }),
    });
  }

  private publish(state: ViewerState): void {
    this.state = clone(state);
    const event: ViewerEvent = {
      kind: "state_changed",
      state: clone(state),
    };
    const sink = this.options.event_sink;
    if (sink === undefined) return;
    if (this.delivering) {
      this.pendingEvent = event;
      return;
    }
    this.delivering = true;
    try {
      let next: ViewerEvent | undefined = event;
      let delivered = 0;
      while (
        next !== undefined &&
        delivered < this.options.viewer_limits.max_event_deliveries
      ) {
        this.pendingEvent = undefined;
        try {
          sink.emit(clone(next));
        } catch {
          // A host callback is observational and cannot invalidate publication.
        }
        delivered += 1;
        next = this.pendingEvent;
      }
      this.pendingEvent = undefined;
    } finally {
      this.delivering = false;
    }
  }

  private cancelled(generation: number): boolean {
    return this.disposed || generation !== this.generation;
  }

  private published(): ViewerOperationResult {
    return { ok: true, outcome: "published", state: this.getState() };
  }

  private rejected(error: ViewerError): ViewerOperationResult {
    return {
      ok: false,
      outcome: "rejected",
      error: clone(error),
      state: this.getState(),
    };
  }

  private cancelledResult(): ViewerOperationResult {
    const error = viewerError("viewer.cancelled", true, {});
    return {
      ok: false,
      outcome: "cancelled",
      error,
      state: this.getState(),
    };
  }

  private disposedResult(): ViewerOperationResult {
    const error = viewerError("viewer.disposed", false, {});
    return {
      ok: false,
      outcome: "rejected",
      error,
      state: { status: "disposed" },
    };
  }
}

export function createViewer(options: ViewerOptions): Viewer {
  validateOptions(options);
  return new ViewerImpl(captureOptions(options));
}

function captureOptions(options: ViewerOptions): ViewerOptions {
  const assetResolve = options.asset_resolver.resolve.bind(
    options.asset_resolver
  );
  const fontResolve = options.font_resolver.resolve.bind(options.font_resolver);
  const eventEmit = options.event_sink?.emit.bind(options.event_sink);
  return {
    renderer_profile: clone(options.renderer_profile),
    render_recipes: clone(options.render_recipes),
    asset_resolver: { resolve: assetResolve },
    font_resolver: { resolve: fontResolve },
    capability_manifest: clone(options.capability_manifest),
    layout: clone(options.layout),
    render_limits: clone(options.render_limits),
    viewer_limits: clone(options.viewer_limits),
    ...(eventEmit === undefined ? {} : { event_sink: { emit: eventEmit } }),
  };
}

function validateOptions(options: ViewerOptions): void {
  if (options === null || typeof options !== "object")
    contract("viewer.input_invalid", "ViewerOptions must be an object");
  if (!isCapabilityManifest(options.capability_manifest))
    contract("viewer.input_invalid", "capability_manifest is invalid");
  const profile = options.renderer_profile?.renderer_profile;
  if (profile === undefined)
    contract("viewer.input_invalid", "renderer_profile is required");
  const capability = options.capability_manifest.renderer_profiles.find(
    (candidate) =>
      candidate.id === profile.profile_id &&
      candidate.version === profile.profile_version
  );
  if (capability?.enabled !== true)
    contract(
      "viewer.profile_unsupported",
      "renderer profile is not enabled by the capability manifest"
    );
  for (const shape of shapes) {
    const recipe = options.render_recipes?.[shape];
    try {
      assertRenderRecipe(recipe);
    } catch (cause) {
      const reason =
        cause instanceof RenderContractError ? cause.code : "invalid_recipe";
      contract(
        "viewer.profile_incompatible",
        `${shape} render recipe is invalid: ${reason}`
      );
    }
    if (
      recipe.shape.kind !== shape ||
      !same(recipe.renderer_profile, profile)
    )
      contract(
        "viewer.profile_incompatible",
        `${shape} render recipe does not match the resolved profile`
      );
  }
  if (
    typeof options.asset_resolver?.resolve !== "function" ||
    typeof options.font_resolver?.resolve !== "function"
  )
    contract("viewer.input_invalid", "resolvers are required");
  validateViewerLimits(options.viewer_limits);
  if (
    options.event_sink !== undefined &&
    typeof options.event_sink.emit !== "function"
  )
    contract("viewer.input_invalid", "event_sink.emit must be a function");
}

function validateViewerLimits(limits: ViewerResourceLimits): void {
  if (limits === null || typeof limits !== "object")
    contract("viewer.input_invalid", "viewer_limits must be an object");
  const integers = [
    limits.max_queued_updates,
    limits.max_snapshot_bytes,
    limits.max_snapshot_items,
    limits.max_render_items,
    limits.max_retained_bytes,
    limits.max_presentation_references,
    limits.max_display_preferences,
    limits.max_display_preference_bytes,
    limits.max_event_deliveries,
  ];
  if (!integers.every((value) => Number.isSafeInteger(value) && value > 0))
    contract("viewer.input_invalid", "Viewer integer limits must be positive");
  if (
    ![limits.min_zoom, limits.max_zoom, limits.max_pan].every(
      (value) => Number.isFinite(value) && value > 0
    ) ||
    limits.min_zoom > limits.max_zoom
  )
    contract("viewer.input_invalid", "Viewer viewport limits are invalid");
}

type Checked<T> =
  | Readonly<{ ok: true; value: T }>
  | Readonly<{ ok: false; error: ViewerError }>;

function validateSnapshot(
  raw: ViewerSnapshot,
  limits: ViewerResourceLimits
): Checked<ViewerSnapshot> {
  if (!closed(raw, snapshotKeys)) return invalidInput("snapshot_fields");
  if (
    raw.viewer_snapshot_schema_version !== 1 ||
    !validSequence(raw.sequence) ||
    typeof raw.complete !== "boolean" ||
    !isDigest(raw.view_data_hash) ||
    !isViewRevision(raw.revision) ||
    !isViewDataStateInputRef(raw.state_input) ||
    !isViewData(raw.view_data)
  )
    return invalidInput("snapshot_value");
  if (
    raw.view_address !== raw.view_data.view_address ||
    !same(raw.revision, raw.view_data.revision) ||
    !same(raw.state_input, raw.view_data.state_input)
  )
    return {
      ok: false,
      error: viewerError("viewer.update_mismatch", true, {
        reason: "snapshot_envelope",
      }),
    };
  const encoded = encodeViewData(raw.view_data);
  const bytes = byteLength(encoded);
  if (bytes > limits.max_snapshot_bytes)
    return resource("snapshot_bytes", limits.max_snapshot_bytes, bytes);
  const items = countViewDataItems(raw.view_data);
  if (items > limits.max_snapshot_items)
    return resource("snapshot_items", limits.max_snapshot_items, items);
  return { ok: true, value: clone(raw) };
}

function validateUpdate(
  raw: ViewDataUpdate,
  limits: ViewerResourceLimits
): Checked<ViewDataUpdate> {
  if (!closed(raw, updateKeys)) return invalidInput("update_fields");
  if (
    raw.viewer_update_schema_version !== 1 ||
    !validSequence(raw.sequence) ||
    !validSequence(raw.previous_sequence) ||
    typeof raw.complete !== "boolean" ||
    !isDigest(raw.previous_view_data_hash) ||
    !isDigest(raw.view_data_hash) ||
    !isViewRevision(raw.previous_revision) ||
    !isViewRevision(raw.revision) ||
    !isViewDataStateInputRef(raw.previous_state_input) ||
    !isViewDataStateInputRef(raw.state_input) ||
    !isViewData(raw.view_data)
  )
    return invalidInput("update_value");
  const snapshot: ViewerSnapshot = {
    viewer_snapshot_schema_version: 1,
    sequence: raw.sequence,
    complete: raw.complete,
    view_address: raw.view_address,
    revision: raw.revision,
    view_data_hash: raw.view_data_hash,
    state_input: raw.state_input,
    view_data: raw.view_data,
  };
  const checked = validateSnapshot(snapshot, limits);
  return checked.ok ? { ok: true, value: clone(raw) } : checked;
}

const snapshotKeys = [
  "viewer_snapshot_schema_version",
  "sequence",
  "complete",
  "view_address",
  "revision",
  "view_data_hash",
  "state_input",
  "view_data",
] as const;
const updateKeys = [
  "viewer_update_schema_version",
  "sequence",
  "previous_sequence",
  "complete",
  "view_address",
  "previous_revision",
  "revision",
  "previous_view_data_hash",
  "view_data_hash",
  "previous_state_input",
  "state_input",
  "view_data",
] as const;

function reconcilePresentation(
  previous: InternalPublication | undefined,
  renderData: RenderData,
  viewAddress: string,
  limits: ViewerResourceLimits
): ViewerPresentationState {
  if (previous === undefined) return defaultPresentation();
  const compatible =
    previous.view_address === viewAddress &&
    previous.render_data.shape === renderData.shape &&
    same(previous.render_data.renderer_profile, renderData.renderer_profile);
  const keys = new Set(
    renderData.source_bindings.map((binding) => binding.render_key)
  );
  const current = previous.presentation;
  const {
    hover_key: _previousHover,
    focus_key: _previousFocus,
    ...presentationBase
  } = current;
  return validatePresentation(
    {
      ...presentationBase,
      selection_keys: compatible
        ? current.selection_keys.filter((key) => keys.has(key))
        : [],
      ...(compatible && current.hover_key !== undefined && keys.has(current.hover_key)
        ? { hover_key: current.hover_key }
        : {}),
      ...(compatible && current.focus_key !== undefined && keys.has(current.focus_key)
        ? { focus_key: current.focus_key }
        : {}),
      expanded_keys: compatible
        ? current.expanded_keys.filter((key) => keys.has(key))
        : [],
      sorting: compatible
        ? current.sorting.filter((item) => keys.has(item.render_key))
        : [],
    },
    limits
  );
}

function clampPresentationToRender(
  value: ViewerPresentationState,
  renderData: RenderData
): ViewerPresentationState {
  const keys = new Set(
    renderData.source_bindings.map((binding) => binding.render_key)
  );
  const { hover_key: _hover, focus_key: _focus, ...base } = value;
  return clone({
    ...base,
    selection_keys: value.selection_keys.filter((key) => keys.has(key)),
    ...(value.hover_key !== undefined && keys.has(value.hover_key)
      ? { hover_key: value.hover_key }
      : {}),
    ...(value.focus_key !== undefined && keys.has(value.focus_key)
      ? { focus_key: value.focus_key }
      : {}),
    expanded_keys: value.expanded_keys.filter((key) => keys.has(key)),
    sorting: value.sorting.filter((item) => keys.has(item.render_key)),
  });
}

function normalizePresentationUpdate(
  update: ViewerPresentationUpdate,
  current: ViewerPresentationState
): ViewerPresentationState {
  if (
    update === null ||
    typeof update !== "object" ||
    Array.isArray(update) ||
    !onlyKeys(update, [
      "selection_keys",
      "hover_key",
      "focus_key",
      "zoom",
      "pan",
      "expanded_keys",
      "sorting",
      "display_preferences",
    ])
  )
    contract("viewer.input_invalid", "presentation update must be an object");
  const hover =
    update.hover_key === undefined ? current.hover_key : update.hover_key;
  const focus =
    update.focus_key === undefined ? current.focus_key : update.focus_key;
  return {
    selection_keys:
      update.selection_keys === undefined
        ? clone(current.selection_keys)
        : clone(update.selection_keys),
    ...(hover === undefined || hover === null ? {} : { hover_key: hover }),
    ...(focus === undefined || focus === null ? {} : { focus_key: focus }),
    zoom: update.zoom === undefined ? current.zoom : update.zoom,
    pan: clone(update.pan === undefined ? current.pan : update.pan),
    expanded_keys:
      update.expanded_keys === undefined
        ? clone(current.expanded_keys)
        : clone(update.expanded_keys),
    sorting:
      update.sorting === undefined
        ? clone(current.sorting)
        : clone(update.sorting),
    display_preferences:
      update.display_preferences === undefined
        ? clone(current.display_preferences)
        : clone(update.display_preferences),
  };
}

function validatePresentation(
  value: ViewerPresentationState,
  limits: ViewerResourceLimits
): ViewerPresentationState {
  const referenceArrays = [value.selection_keys, value.expanded_keys];
  if (
    !referenceArrays.every(
      (items) =>
        Array.isArray(items) &&
        items.every((item) => typeof item === "string") &&
        new Set(items).size === items.length
    ) ||
    (value.hover_key !== undefined && typeof value.hover_key !== "string") ||
    (value.focus_key !== undefined && typeof value.focus_key !== "string")
  )
    contract("viewer.input_invalid", "presentation references are invalid");
  const referenceCount =
    value.selection_keys.length +
    value.expanded_keys.length +
    value.sorting.length +
    (value.hover_key === undefined ? 0 : 1) +
    (value.focus_key === undefined ? 0 : 1);
  if (referenceCount > limits.max_presentation_references)
    contract("viewer.resource_limit", "too many presentation references");
  if (
    !Number.isFinite(value.zoom) ||
    !Number.isFinite(value.pan.x) ||
    !Number.isFinite(value.pan.y) ||
    !closed(value.pan, ["x", "y"])
  )
    contract("viewer.input_invalid", "viewport values must be finite");
  const preferences = value.display_preferences;
  if (
    preferences === null ||
    typeof preferences !== "object" ||
    Array.isArray(preferences) ||
    !Object.values(preferences).every(isJsonValue)
  )
    contract("viewer.input_invalid", "display preferences must be JSON values");
  if (Object.keys(preferences).length > limits.max_display_preferences)
    contract("viewer.resource_limit", "too many display preferences");
  if (byteLength(canonical(preferences)) > limits.max_display_preference_bytes)
    contract("viewer.resource_limit", "display preferences are too large");
  if (
    !Array.isArray(value.sorting) ||
    !value.sorting.every(
      (item) =>
        item !== null &&
        typeof item === "object" &&
        closed(item, ["render_key", "direction"]) &&
        typeof item.render_key === "string" &&
        typeof item.direction === "string" &&
        ["ascending", "descending"].includes(item.direction)
    )
  )
    contract("viewer.input_invalid", "sorting is invalid");
  return clone({
    ...value,
    zoom: clamp(value.zoom, limits.min_zoom, limits.max_zoom),
    pan: {
      x: clamp(value.pan.x, -limits.max_pan, limits.max_pan),
      y: clamp(value.pan.y, -limits.max_pan, limits.max_pan),
    },
  });
}

function defaultPresentation(): ViewerPresentationState {
  return {
    selection_keys: [],
    zoom: 1,
    pan: { x: 0, y: 0 },
    expanded_keys: [],
    sorting: [],
    display_preferences: {},
  };
}

function mapRenderFailure(diagnostics: readonly RenderMaterializationDiagnostic[]): {
  status:
    | "unsupported_profile"
    | "missing_asset"
    | "missing_font"
    | "recoverable_error"
    | "fatal";
  error: ViewerError;
} {
  const codes = new Set(diagnostics.map((diagnostic) => diagnostic.code));
  if (codes.has("render.profile_unsupported"))
    return mapped("unsupported_profile", "viewer.profile_unsupported", true);
  if (codes.has("render.profile_incompatible"))
    return mapped("unsupported_profile", "viewer.profile_incompatible", true);
  if (codes.has("render.asset_missing"))
    return mapped("missing_asset", "viewer.asset_missing", true);
  if (codes.has("render.font_missing"))
    return mapped("missing_font", "viewer.font_missing", true);
  if (codes.has("render.geometry_invalid"))
    return mapped("fatal", "viewer.render_failed", false);
  if (codes.has("render.resource_limit"))
    return mapped("recoverable_error", "viewer.resource_limit", true);
  if (codes.has("render.input_invalid"))
    return mapped("fatal", "viewer.render_failed", false);
  return mapped("recoverable_error", "viewer.render_failed", true);

  function mapped(
    status:
      | "unsupported_profile"
      | "missing_asset"
      | "missing_font"
      | "recoverable_error"
      | "fatal",
    code: ViewerErrorCode,
    recoverable: boolean
  ) {
    return {
      status,
      error: {
        ...viewerError(code, recoverable, {}),
        render_diagnostics: clone(diagnostics),
      },
    };
  }
}

function findViewDataItem(
  value: unknown,
  key: ViewDataItemKey
): Readonly<Record<string, unknown>> | undefined {
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = findViewDataItem(item, key);
      if (found !== undefined) return found;
    }
    return undefined;
  }
  if (value === null || typeof value !== "object") return undefined;
  const record = value as Record<string, unknown>;
  if (record.key === key) return record;
  for (const item of Object.values(record)) {
    const found = findViewDataItem(item, key);
    if (found !== undefined) return found;
  }
  return undefined;
}

function countViewDataItems(value: unknown): number {
  if (Array.isArray(value))
    return value.reduce((total, item) => total + countViewDataItems(item), 0);
  if (value === null || typeof value !== "object") return 0;
  const record = value as Record<string, unknown>;
  let count =
    typeof record.key === "string" && record.key.startsWith("vdi:") ? 1 : 0;
  for (const item of Object.values(record)) count += countViewDataItems(item);
  return count;
}

function invalidInput(reason: string): Checked<never> {
  return {
    ok: false,
    error: viewerError("viewer.input_invalid", true, { reason }),
  };
}

function resource(
  name: string,
  limit: number,
  actual: number
): Checked<never> {
  return {
    ok: false,
    error: viewerError("viewer.resource_limit", true, {
      resource: name,
      limit,
      actual,
    }),
  };
}

function viewerError(
  code: ViewerErrorCode,
  recoverable: boolean,
  details: Readonly<Record<string, ViewerJsonValue>>
): ViewerError {
  return { code, recoverable, details: clone(details) };
}

function isOrderingOrIdentityError(error: ViewerError): boolean {
  return [
    "viewer.update_gap",
    "viewer.update_stale",
    "viewer.update_conflict",
    "viewer.update_mismatch",
  ].includes(error.code);
}

function contract(code: ViewerErrorCode, message: string): never {
  throw new ViewerContractError(code, message);
}

function closed(
  value: unknown,
  keys: readonly string[]
): value is Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value))
    return false;
  const actual = Object.keys(value);
  return actual.length === keys.length && actual.every((key) => keys.includes(key));
}

function onlyKeys(value: object, keys: readonly string[]): boolean {
  return Object.keys(value).every((key) => keys.includes(key));
}

function validSequence(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) >= 0;
}

function same(left: unknown, right: unknown): boolean {
  return canonical(left) === canonical(right);
}

function canonical(value: unknown): string {
  if (value === undefined) return "undefined";
  if (value === null || typeof value === "boolean" || typeof value === "string")
    return JSON.stringify(value);
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new TypeError("non-finite value");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, item]) => item !== undefined)
    .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0));
  return `{${entries
    .map(([key, item]) => `${JSON.stringify(key)}:${canonical(item)}`)
    .join(",")}}`;
}

function clone<T>(value: T): T {
  return structuredClone(value);
}

function byteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

function safeReason(cause: unknown): string {
  if (cause instanceof Error && cause.name === "AbortError") return "aborted";
  return cause instanceof Error ? cause.name : "unknown";
}

function abortable<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise<T>((resolve, reject) => {
    const abort = (): void => reject(abortError());
    signal.addEventListener("abort", abort, { once: true });
    promise.then(
      (value) => {
        signal.removeEventListener("abort", abort);
        resolve(value);
      },
      (cause) => {
        signal.removeEventListener("abort", abort);
        reject(cause);
      }
    );
  });
}

function abortError(): Error {
  const error = new Error("Viewer operation aborted");
  error.name = "AbortError";
  return error;
}

// Kept as a runtime guard for values received from host-owned preference maps.
function isJsonValue(value: unknown): value is ViewerJsonValue {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "boolean"
  )
    return true;
  if (typeof value === "number") return Number.isFinite(value);
  if (Array.isArray(value)) return value.every(isJsonValue);
  return (
    typeof value === "object" &&
    Object.values(value as Record<string, unknown>).every(isJsonValue)
  );
}
