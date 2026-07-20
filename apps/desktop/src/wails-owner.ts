// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { LibraryController } from "@layerdraw/library";
import type { ReviewApprovalRequest, ReviewCommentRequest, ReviewModel, ReviewProposal, ReviewSnapshot } from "@layerdraw/review";
import type { Viewer } from "@layerdraw/viewer";
import type { DesktopFeatureAvailability, DesktopLifecycleSnapshot } from "./contracts.js";

export interface DesktopOwnerActionDTO {
  readonly project_id: string;
  readonly session_generation: number;
  readonly authoritative_revision_token: string;
}

export interface DesktopSelectViewDTO extends DesktopOwnerActionDTO {
  readonly view_address: string;
}

export type DesktopOwnerResultDTO =
  | Readonly<{ outcome: "success"; publication: DesktopLifecycleSnapshot }>
  | Readonly<{ outcome: "failed" | "rejected" | "cancelled"; failure: Readonly<{ code: string; recoverable: boolean }> }>;

/**
 * Typed adapter required from the project-owning Desktop backend. The operation
 * names intentionally match the Wails methods the Go bridge must generate.
 * BrowserEditor and Viewer are constructed by this trusted adapter; they are
 * never serialized through Wails or reconstructed from global state.
 */
export interface DesktopProjectOwnerBinding {
  ProjectPublication(): Promise<DesktopLifecycleSnapshot>;
  SelectProjectView(input: DesktopSelectViewDTO, signal: AbortSignal): Promise<DesktopOwnerResultDTO>;
  ShowProjectRecoveryOptions(input: DesktopOwnerActionDTO, signal: AbortSignal): Promise<DesktopOwnerResultDTO>;
  DisconnectProjectExternal(input: DesktopOwnerActionDTO, signal: AbortSignal): Promise<DesktopOwnerResultDTO>;
  CreateProjectViewer(): Viewer;
}

export interface DesktopRegistryHostBinding {
  RegistryDispatch(request: DesktopRegistryRequestDTO): Promise<unknown>;
}

export interface DesktopRegistryRequestDTO {
  readonly wire_version: "1.0";
  readonly operation: "registry.list_sources" | "registry.configure_source" | "registry.connect_source" | "registry.disconnect_source" | "registry.search" | "registry.plan_install" | "registry.commit_plan" | "registry.get_transaction" | "registry.recover_transaction" | "registry.author_artifact";
  readonly request_id: string;
  readonly input: unknown;
}

/** Concrete method closure required from the future generated Go bridge. */
export interface DesktopReviewHostBinding {
  ReviewSnapshot(): Promise<ReviewSnapshot>;
  ReviewComment(input: ReviewCommentRequest): Promise<ReviewProposal>;
  ReviewApproveAndApply(input: ReviewApprovalRequest): Promise<ReviewProposal>;
  ReviewWithdraw(input: Readonly<{ proposal_id: string; generation: number }>): Promise<ReviewProposal>;
}

export type DesktopOwnedFeature<T> =
  | Readonly<{ status: "available"; value: T }>
  | Readonly<{ status: "unavailable"; availability: DesktopFeatureAvailability }>;

export type DesktopLibraryFeature = DesktopOwnedFeature<LibraryController>;
export type DesktopReviewFeature = DesktopOwnedFeature<ReviewModel>;
