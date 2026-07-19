// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type {
  CompletedExportArtifactEntry,
  ExportPlan,
  ExportSourceManifest,
  ExporterProfileRef,
  ExternalStateSummary,
  ViewData,
} from "@layerdraw/protocol/semantic";
import type { RenderData, RasterizerProfileRef } from "@layerdraw/render";

export const EXPORT_SERIALIZER_API_VERSION = 1 as const;

export type ExportDiagnosticCode =
  | "export.unsupported_shape_format"
  | "export.fidelity_unavailable"
  | "export.profile_missing"
  | "export.profile_incompatible"
  | "export.render_required"
  | "export.render_input_mismatch"
  | "export.asset_missing"
  | "export.font_missing"
  | "export.source_manifest_invalid"
  | "export.serializer_failed";

export interface ExportDiagnostic {
  readonly code: ExportDiagnosticCode;
  readonly severity: "error";
}

export interface ExportSerializerProfile {
  readonly schema_version: 1;
  readonly ref: ExporterProfileRef;
  readonly limits?: Partial<ExportResourceLimits>;
}

export interface ExportResourceLimits {
  readonly max_input_bytes: number;
  readonly max_artifacts: number;
  readonly max_representations: number;
  readonly max_units: number;
  readonly max_resources: number;
  readonly max_svg_primitives: number;
  readonly max_csv_rows: number;
  readonly max_output_bytes: number;
}

export interface ResolvedExportResource {
  readonly digest: `sha256:${string}`;
  readonly bytes: Uint8Array;
}

export interface RasterizerRequest {
  readonly api_version: 1;
  readonly profile: RasterizerProfileRef;
  readonly svg: Uint8Array;
  readonly width: number;
  readonly height: number;
  readonly density: number;
  readonly background: string;
  readonly assets: readonly ResolvedExportResource[];
  readonly fonts: readonly ResolvedExportResource[];
  readonly signal?: AbortSignal;
}

export interface RasterizerResult {
  readonly bytes: Uint8Array;
  readonly width: number;
  readonly height: number;
  readonly density: number;
  readonly profile: RasterizerProfileRef;
}

export interface RasterizerImplementation {
  readonly api_version: 1;
  readonly environment: "browser" | "node";
  readonly profile: RasterizerProfileRef;
  rasterize(request: RasterizerRequest): Promise<RasterizerResult>;
  dispose?(): void | Promise<void>;
}

export interface ExportClock {
  readonly fixed_rfc3339: string;
}

export interface SerializerInput {
  readonly export_plan: ExportPlan;
  readonly view_data: ViewData;
  readonly render_data?: RenderData;
  readonly serializer_profile?: ExportSerializerProfile;
  readonly rasterizer_profile?: RasterizerProfileRef;
  readonly rasterizer?: RasterizerImplementation;
  readonly assets: readonly ResolvedExportResource[];
  readonly fonts: readonly ResolvedExportResource[];
  readonly state_summary?: ExternalStateSummary;
  readonly clock?: ExportClock;
  readonly signal?: AbortSignal;
}

export interface ExportArtifact {
  readonly entry: CompletedExportArtifactEntry;
  readonly bytes: Uint8Array;
}

export interface ExportSuccess {
  readonly ok: true;
  readonly artifacts: readonly ExportArtifact[];
  readonly source_manifest?: ExportSourceManifest;
  readonly source_manifest_bytes?: Uint8Array;
  readonly source_manifest_path?: string;
  readonly source_manifest_digest?: `sha256:${string}`;
}

export interface ExportFailure {
  readonly ok: false;
  readonly diagnostics: readonly [ExportDiagnostic];
}

export type ExportResult = ExportSuccess | ExportFailure;
