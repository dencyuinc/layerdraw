// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import test from "node:test";
import * as engine from "../dist/engine.gen.js";
import * as semantic from "../dist/semantic.gen.js";

// PR #200 / CodeQL alerts #213–217: these short-circuit conditions are
// schema checks, not authorization decisions. An earlier invalid field must
// reject the entire value; a valid earlier field must not bypass later checks.
const cases = [
  {
    module: engine,
    name: "EntityTypeCreateSubjectFields",
    valid: { display_name: "Node", representation: { kind: "shape", shape: "rect" } },
    early: [{ display_name: "" }],
    late: [{ image: { digest: "invalid", media_type: "image/png" } }, { representation: { kind: "unknown" } }],
  },
  {
    module: engine,
    name: "RelationTypeCreateSubjectFields",
    valid: { display_name: "Link", forward_label: "links", semantic_kind: "reference", from: { role: "source" }, to: { role: "target" } },
    early: [{ display_name: "" }, { forward_label: "" }],
    late: [
      { export: { include_endpoints: "yes" } },
      { from: { role: 1 } },
      { to: { role: 1 } },
      { projections: { diagram: { mode: "unknown" } } },
      { render: { edge: { arrow: "unknown" } } },
      { traversal: { default_direction: "unknown" } },
    ],
  },
  {
    module: engine,
    name: "ViewCreateSubjectFields",
    valid: { display_name: "Overview", category: "topology", shape: { kind: "diagram" }, source: { kind: "field", field: "display_name" } },
    early: [{ display_name: "" }],
    late: [{ shape: { kind: "unknown" } }, { source: { kind: "field", field: "unknown" } }],
  },
  {
    module: semantic,
    name: "AuthoredRelationCardinality",
    valid: { from_per_to: { min: 0, max: "many" }, to_per_from: { min: 1, max: 1 } },
    early: [{ from_per_to: { min: 2, max: "many" } }],
    late: [{ to_per_from: { min: 0, max: 2 } }],
  },
];

for (const { module, name, valid, early, late } of cases) {
  test(`${name} rejects invalid fields on either side of a short circuit`, () => {
    const is = module[`is${name}`];
    const encode = module[`encode${name}`];
    const decode = module[`decode${name}`];
    assert.equal(is(valid), true);
    assert.deepEqual(decode(encode(valid)), valid);

    const invalid = [
      ...early,
      ...late,
      ...early.flatMap((first) => late.map((last) => ({ ...first, ...last }))),
    ];
    for (const patch of invalid) {
      const input = { ...valid, ...patch };
      assert.equal(is(input), false, JSON.stringify(patch));
      assert.throws(() => encode(input), TypeError);
      assert.throws(() => decode(JSON.stringify(input)), TypeError);
    }
  });
}
