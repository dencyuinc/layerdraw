// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { EngineClient } from "@layerdraw/engine-client";
import type { DocumentGeneration, FindSymbolsInput, SymbolReadItem, WorkbenchLimits } from "@layerdraw/protocol/engine";

export class AuthoringSchemaError extends Error {}

/** One listed subject: address plus the display metadata the authoring UI needs. */
export interface AuthoringSubject {
  readonly address: SymbolReadItem["address"];
  readonly display_name: string;
  readonly kind: SymbolReadItem["kind"];
}

/** Schema snapshot the authoring surfaces render from (layers for placement,
 * types for creation forms, views for the sidebar workflows). */
export interface AuthoringSchema {
  readonly layers: readonly AuthoringSubject[];
  readonly entityTypes: readonly AuthoringSubject[];
  readonly relationTypes: readonly AuthoringSubject[];
  readonly entities: readonly AuthoringSubject[];
  readonly views: readonly AuthoringSubject[];
  readonly queries: readonly AuthoringSubject[];
}

const listLimits: WorkbenchLimits = Object.freeze({ max_items: "500", max_output_bytes: "1000000" });

/** Every stable address begins with "ldl:", so an address-prefix query lists a
 * whole subject kind without reimplementing any Engine matching semantics. */
async function listKind(engine: EngineClient, generation: DocumentGeneration, kinds: FindSymbolsInput["subject_kinds"]): Promise<readonly AuthoringSubject[]> {
  const outcome = await engine.workbench.findSymbols({
    query: "ldl:",
    match_mode: "prefix",
    case_mode: "sensitive",
    document_generation: generation,
    limits: listLimits,
    ...(kinds === undefined ? {} : { subject_kinds: kinds }),
  });
  if (outcome.origin !== "engine") throw new AuthoringSchemaError("engine.find_symbols_cancelled");
  const payload = outcome.response.payload;
  if (outcome.response.outcome !== "success" || payload === undefined) {
    throw new AuthoringSchemaError(outcome.response.failure?.code ?? "engine.find_symbols_failed");
  }
  return payload.items.map((item: SymbolReadItem) => Object.freeze({ address: item.address, display_name: item.display_name, kind: item.kind }));
}

/** Loads the authoring schema for the current document generation. Failures
 * propagate to the caller; surfaces render the closed failure state instead of
 * inventing defaults. */
export async function loadAuthoringSchema(engine: EngineClient, generation: DocumentGeneration): Promise<AuthoringSchema> {
  const [layers, entityTypes, relationTypes, entities, views, queries] = await Promise.all([
    listKind(engine, generation, ["layer"]),
    listKind(engine, generation, ["entity_type"]),
    listKind(engine, generation, ["relation_type"]),
    listKind(engine, generation, ["entity"]),
    listKind(engine, generation, ["view"]),
    listKind(engine, generation, ["query"]),
  ]);
  return Object.freeze({ layers, entityTypes, relationTypes, entities, views, queries });
}
