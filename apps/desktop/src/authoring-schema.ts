// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { SemanticSubject } from "@layerdraw/protocol/semantic";

export class AuthoringSchemaError extends Error {}

/** One listed subject: address plus the display metadata the authoring UI needs. */
export interface AuthoringSubject {
  readonly address: SemanticSubject["address"];
  readonly display_name: string;
  readonly kind: SemanticSubject["kind"];
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

/** The trailing address segment names the subject; display-name enrichment
 * arrives with declaration reads. */
function labelOf(address: string): string {
  const segment = address.split(":").at(-1) ?? address;
  return segment;
}

/** Groups Engine-compiled semantic subjects into the authoring schema. The
 * subjects come straight from the Engine semantic index; nothing is derived
 * client-side beyond grouping and labeling. */
export function groupAuthoringSchema(subjects: readonly SemanticSubject[]): AuthoringSchema {
  const byKind = (kind: SemanticSubject["kind"]): readonly AuthoringSubject[] =>
    subjects.filter((subject) => subject.kind === kind)
      .map((subject) => Object.freeze({ address: subject.address, display_name: labelOf(subject.address), kind: subject.kind }));
  return Object.freeze({
    layers: byKind("layer"),
    entityTypes: byKind("entity_type"),
    relationTypes: byKind("relation_type"),
    entities: byKind("entity"),
    views: byKind("view"),
    queries: byKind("query"),
  });
}
