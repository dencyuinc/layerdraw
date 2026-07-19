// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type {
  RegistryArtifactRelease, RegistryAuthConnectionInput, RegistryAuthoringInput,
  RegistryClient, RegistryCommitInput, RegistryFailure, RegistryInstallPlan, RegistryPlanInput,
  RegistryResult, RegistrySearchInput, RegistrySource, RegistryTransaction,
} from "./index.js";

export interface RegistryHostBinding {
  invoke(operation: string, input: unknown, signal?: AbortSignal): Promise<unknown>;
}

const operations = Object.freeze({
  sources: "registry.list_sources", configure: "registry.configure_source",
  connect: "registry.connect_source", disconnect: "registry.disconnect_source",
  search: "registry.search", plan: "registry.plan_install", commit: "registry.commit_plan",
  transaction: "registry.get_transaction", author: "registry.author_artifact",
});

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function hostResult<T>(value: unknown): RegistryResult<T> {
  if (!isRecord(value) || typeof value.ok !== "boolean") {
    return { ok: false, failure: { code: "registry.unavailable", subject: "invalid_host_response", actionable: true } };
  }
  if (value.ok === true && "value" in value) return { ok: true, value: structuredClone(value.value) as T };
  if (value.ok === false && isRecord(value.failure) && typeof value.failure.code === "string" && typeof value.failure.subject === "string" && typeof value.failure.actionable === "boolean") {
    const failure: RegistryFailure = {
      code: value.failure.code, subject: value.failure.subject, actionable: value.failure.actionable,
      ...(typeof value.failure.correlation_id === "string" ? { correlation_id: value.failure.correlation_id } : {}),
    };
    return { ok: false, failure };
  }
  return { ok: false, failure: { code: "registry.unavailable", subject: "invalid_host_response", actionable: true } };
}

export function createHostRegistryClient(binding: RegistryHostBinding): RegistryClient {
  if (!binding || typeof binding.invoke !== "function") throw new TypeError("registry host binding is required");
  const call = async <T>(operation: string, input: unknown, signal?: AbortSignal): Promise<RegistryResult<T>> => {
    if (signal?.aborted) return { ok: false, failure: { code: "registry.cancelled", subject: operation, actionable: true } };
    try { return hostResult<T>(await binding.invoke(operation, structuredClone(input), signal)); }
    catch { return { ok: false, failure: { code: "registry.unavailable", subject: operation, actionable: true } }; }
  };
  return Object.freeze({
    listSources: (signal?: AbortSignal) => call<readonly RegistrySource[]>(operations.sources, {}, signal),
    configureSource: (source: Omit<RegistrySource, "connected">, signal?: AbortSignal) => call<RegistrySource>(operations.configure, { source }, signal),
    connectSource: (input: RegistryAuthConnectionInput, signal?: AbortSignal) => call<RegistrySource>(operations.connect, input, signal),
    disconnectSource: (sourceId: string, signal?: AbortSignal) => call<RegistrySource>(operations.disconnect, { source_id: sourceId }, signal),
    search: (input: RegistrySearchInput, signal?: AbortSignal) => call<readonly RegistryArtifactRelease[]>(operations.search, input, signal),
    plan: (input: RegistryPlanInput, signal?: AbortSignal) => call<RegistryInstallPlan>(operations.plan, input, signal),
    commit: (input: RegistryCommitInput, signal?: AbortSignal) => call<RegistryTransaction>(operations.commit, input, signal),
    getTransaction: (transactionId: string, signal?: AbortSignal) => call<RegistryTransaction>(operations.transaction, { transaction_id: transactionId }, signal),
    authorArtifact: (input: RegistryAuthoringInput, signal?: AbortSignal) => call<RegistryArtifactRelease>(operations.author, input, signal),
  });
}
