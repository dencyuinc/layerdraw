// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";

import Ajv2020 from "ajv/dist/2020.js";

const schemaRoot = new URL("../../../schemas/", import.meta.url);

async function readJSON(path) {
  return JSON.parse(await readFile(new URL(path, schemaRoot), "utf8"));
}

function protocolVersion(value) {
  const match = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.exec(value);
  if (match === null) return undefined;
  const version = [Number(match[1]), Number(match[2])];
  return version.every((part) => Number.isSafeInteger(part) && part <= 0xffffffff) ? version : undefined;
}

function compareVersion(left, right) {
  return left[0] === right[0] ? left[1] - right[1] : left[0] - right[0];
}

function protocolRange(value) {
  const parts = value.split("..");
  if (parts.length !== 2) return undefined;
  const lower = protocolVersion(parts[0]);
  const upper = protocolVersion(parts[1]);
  return lower !== undefined && upper !== undefined && lower[0] === upper[0] && compareVersion(lower, upper) <= 0 ? [lower, upper] : undefined;
}

function canonicalInteger(value, minimum, maximum, pattern) {
  if (!pattern.test(value)) return false;
  try { const parsed = BigInt(value); return parsed >= minimum && parsed <= maximum; } catch { return false; }
}

function canonicalBinary64(value, positive) {
  if (!/^-?(0|[1-9][0-9]*)(?:\.[0-9]+)?(?:e[+-][1-9][0-9]*)?$/.test(value)) return false;
  const parsed = Number(value);
  return Number.isFinite(parsed) && !Object.is(parsed, -0) && (!positive || parsed > 0) && String(parsed) === value;
}

function canonicalSourcePath(value) {
  return value !== "" && !value.startsWith("/") && !value.includes("\\") && !value.includes("\0") &&
    value.split("/").every((segment) => segment !== "" && segment !== "." && segment !== "..");
}

function stableAddressTuple(value) {
  const parts = value.split(":");
  if (parts.length < 3 || parts[0] !== "ldl") return undefined;
  let origin;
  let components;
  let pathStart;
  if (parts[1] === "project") {
    origin = 0; components = [parts[2]]; pathStart = 3;
  } else if (parts[1] === "pack" && parts.length >= 4) {
    origin = 1; components = [parts[2], parts[3]]; pathStart = 4;
  } else return undefined;
  if ((parts.length - pathStart) % 2 !== 0) return undefined;
  const path = [];
  for (let index = pathStart; index < parts.length; index += 2) path.push([parts[index], parts[index + 1]]);
  return {origin, components, path};
}

function compareStableAddresses(left, right) {
  const leftTuple = stableAddressTuple(left);
  const rightTuple = stableAddressTuple(right);
  if (leftTuple === undefined || rightTuple === undefined) return undefined;
  if (leftTuple.origin !== rightTuple.origin) return leftTuple.origin - rightTuple.origin;
  for (let index = 0; index < Math.min(leftTuple.components.length, rightTuple.components.length); index++) {
    if (leftTuple.components[index] !== rightTuple.components[index]) return leftTuple.components[index] < rightTuple.components[index] ? -1 : 1;
  }
  if (leftTuple.components.length !== rightTuple.components.length) return leftTuple.components.length - rightTuple.components.length;
  if (leftTuple.path.length !== rightTuple.path.length) return leftTuple.path.length - rightTuple.path.length;
  const ranks = new Map([["entity-type", 0], ["relation-type", 1], ["layer", 2], ["entity", 3], ["relation", 4], ["query", 5], ["view", 6], ["reference", 7], ["column", 8], ["constraint", 9], ["row", 10], ["parameter", 11], ["table-column", 12], ["export", 13]]);
  for (let index = 0; index < leftTuple.path.length; index++) {
    const leftSegment = leftTuple.path[index];
    const rightSegment = rightTuple.path[index];
    const kind = ranks.get(leftSegment[0]) - ranks.get(rightSegment[0]);
    if (kind !== 0) return kind;
    if (leftSegment[1] !== rightSegment[1]) return leftSegment[1] < rightSegment[1] ? -1 : 1;
  }
  return 0;
}

function canonicalLocalIdentifier(value) {
  return typeof value === "string" && /^[a-z][a-z0-9_]*$/.test(value);
}

function compareUnicodeScalars(left, right) {
  const leftScalars = Array.from(left, (item) => item.codePointAt(0));
  const rightScalars = Array.from(right, (item) => item.codePointAt(0));
  for (let index = 0; index < Math.min(leftScalars.length, rightScalars.length); index++) {
    if (leftScalars[index] !== rightScalars[index]) return leftScalars[index] - rightScalars[index];
  }
  return leftScalars.length - rightScalars.length;
}

function compareCanonicalUnsignedDecimals(left, right) {
  if (!/^(0|[1-9][0-9]*)$/.test(left) || !/^(0|[1-9][0-9]*)$/.test(right)) return undefined;
  if (left.length !== right.length) return left.length - right.length;
  return left < right ? -1 : left > right ? 1 : 0;
}

function orderedPair(data, rule) {
  const own = (key) => Object.prototype.hasOwnProperty.call(data, key);
  if (!own(rule.lower) || !own(rule.upper)) return true;
  const lower = data[rule.lower];
  const upper = data[rule.upper];
  if (typeof lower !== "string" || typeof upper !== "string") return false;
  if (rule.comparison === "unsigned_decimal") {
    const compared = compareCanonicalUnsignedDecimals(lower, upper);
    return compared !== undefined && compared <= 0;
  }
  if (rule.comparison === "finite_binary64") {
    const lowerValue = Number(lower);
    const upperValue = Number(upper);
    return Number.isFinite(lowerValue) && Number.isFinite(upperValue) && lowerValue <= upperValue;
  }
  return false;
}

function validExportRecipe(value) {
  const extensions = new Map([
    ["json", ".json"], ["yaml", ".yaml"], ["svg", ".svg"], ["png", ".png"], ["pdf", ".pdf"],
    ["html", ".html"], ["csv", ".csv"], ["tsv", ".tsv"], ["xlsx", ".xlsx"], ["markdown", ".md"],
    ["pptx", ".pptx"], ["docx", ".docx"], ["mermaid", ".mmd"], ["bpmn", ".bpmn"], ["drawio", ".drawio"],
  ]);
  const extension = extensions.get(value.format);
  return extension !== undefined && value.extension === extension && value.options?.kind === value.format &&
    value.exporter_profile?.format === value.format && typeof value.filename === "string" &&
    value.filename !== "" && value.filename !== "." && value.filename !== ".." &&
    !/[\\/\0]/.test(value.filename) && value.filename.endsWith(extension) && value.filename.slice(0, -extension.length).length > 0;
}

function hasDirectStableAddressOwner(owner, child) {
  const parts = child.split(":");
  return parts.length >= 2 && parts.slice(0, -2).join(":") === owner;
}

function validViewRecipe(value) {
  const {address, shape, reserved_table_column_ids: reservedValues} = value;
  if (typeof address !== "string" || shape === null || typeof shape !== "object" || Array.isArray(shape) ||
      !Array.isArray(reservedValues) || !reservedValues.every((item) => typeof item === "string")) return false;
  if (shape.kind !== "table") return true;
  const table = shape.table;
  if (table === null || typeof table !== "object" || Array.isArray(table) || !Array.isArray(table.columns)) return false;
  const reserved = new Set(reservedValues);
  return table.columns.every((column) => column !== null && typeof column === "object" && !Array.isArray(column) &&
    typeof column.address === "string" && typeof column.id === "string" &&
    hasDirectStableAddressOwner(address, column.address) && !reserved.has(column.id));
}

function realUTCDateTime(value) {
  const match = /^([0-9]{4})-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.[0-9]{1,9})?Z$/.exec(value);
  if (match === null) return false;
  const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  return day <= [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1];
}

function hasScalarUnicode(value) {
  for (let index = 0; index < value.length; index++) {
    const unit = value.charCodeAt(index);
    if (unit >= 0xdc00 && unit <= 0xdfff) return false;
    if (unit < 0xd800 || unit > 0xdbff) continue;
    if (index + 1 >= value.length) return false;
    const low = value.charCodeAt(index + 1);
    if (low < 0xdc00 || low > 0xdfff) return false;
    index++;
  }
  return true;
}

function hasScalarUnicodeTree(root) {
  const pending = [root];
  const seen = new Set();
  while (pending.length > 0) {
    const value = pending.pop();
    if (typeof value === "string") {
      if (!hasScalarUnicode(value)) return false;
      continue;
    }
    if (value === null || typeof value !== "object" || seen.has(value)) continue;
    seen.add(value);
    if (Array.isArray(value)) {
      pending.push(...value);
      continue;
    }
    for (const key of Object.keys(value)) {
      if (!hasScalarUnicode(key)) return false;
      pending.push(value[key]);
    }
  }
  return true;
}

function utf8ByteLength(value) {
  let bytes = 0;
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code <= 0x7f) bytes++;
    else if (code <= 0x7ff) bytes += 2;
    else if (code >= 0xd800 && code <= 0xdbff) {
      const low = value.charCodeAt(index + 1);
      if (!(low >= 0xdc00 && low <= 0xdfff)) throw new TypeError("protocol JSON contains an unpaired high surrogate");
      bytes += 4;
      index++;
    } else if (code >= 0xdc00 && code <= 0xdfff) throw new TypeError("protocol JSON contains an unpaired low surrogate");
    else bytes += 3;
  }
  return bytes;
}

function hasContainerDepth(root, maximum) {
  const active = new Set();
  const visit = (value, depth) => {
    if (value === null || typeof value !== "object") return true;
    if (active.has(value) || depth >= maximum) return false;
    active.add(value);
    const children = Array.isArray(value) ? value : Object.values(value);
    const valid = children.every((child) => visit(child, depth + 1));
    active.delete(value);
    return valid;
  };
  return visit(root, 0);
}

function fitsCanonicalJSONBytes(value, maximum) {
  try {
    const encoded = JSON.stringify(value);
    if (encoded === undefined || !hasScalarUnicodeTree(value)) return false;
    const canonicalEscapes = encoded.replace(/[\u2028\u2029]/g, (character) => character === "\u2028" ? "\\u2028" : "\\u2029");
    return utf8ByteLength(canonicalEscapes) <= maximum;
  } catch {
    return false;
  }
}

function skipJSONWhitespace(input, start) {
  let index = start;
  while (index < input.length && /[ \t\r\n]/.test(input[index])) index++;
  return index;
}

function scanJSONToken(input, start) {
  let index = start;
  while (index < input.length && !/[{}\[\],:\s]/.test(input[index])) index++;
  return index;
}

function parseHexCodeUnit(input, start) {
  const text = input.slice(start, start + 4);
  if (!/^[0-9a-fA-F]{4}$/.test(text)) throw new TypeError("protocol JSON string has an invalid Unicode escape");
  return Number.parseInt(text, 16);
}

function scanJSONString(input, start) {
  for (let index = start + 1; index < input.length; index++) {
    const code = input.charCodeAt(index);
    if (code === 0x22) return index;
    if (code < 0x20) throw new TypeError("protocol JSON string contains an unescaped control character");
    if (code >= 0xd800 && code <= 0xdbff) {
      const low = input.charCodeAt(index + 1);
      if (!(low >= 0xdc00 && low <= 0xdfff)) throw new TypeError("protocol JSON string has an unpaired high surrogate");
      index++;
      continue;
    }
    if (code >= 0xdc00 && code <= 0xdfff) throw new TypeError("protocol JSON string has an unpaired low surrogate");
    if (code !== 0x5c) continue;
    index++;
    if (index >= input.length) throw new TypeError("protocol JSON string has a truncated escape");
    if (input[index] !== "u") continue;
    const unit = parseHexCodeUnit(input, index + 1);
    index += 4;
    if (unit >= 0xdc00 && unit <= 0xdfff) throw new TypeError("protocol JSON string has an unpaired low surrogate");
    if (unit < 0xd800 || unit > 0xdbff) continue;
    if (input[index + 1] !== "\\" || input[index + 2] !== "u") throw new TypeError("protocol JSON string has an unpaired high surrogate");
    const low = parseHexCodeUnit(input, index + 3);
    if (low < 0xdc00 || low > 0xdfff) throw new TypeError("protocol JSON string has an invalid surrogate pair");
    index += 6;
  }
  throw new TypeError("protocol JSON string is unterminated");
}

function scanUniqueJSONValue(input, start) {
  let index = skipJSONWhitespace(input, start);
  const character = input[index];
  if (character === '"') return scanJSONString(input, index) + 1;
  if (character === "[") {
    index = skipJSONWhitespace(input, index + 1);
    if (input[index] === "]") return index + 1;
    for (;;) {
      index = skipJSONWhitespace(input, scanUniqueJSONValue(input, index));
      if (input[index] === "]") return index + 1;
      if (input[index] !== ",") throw new TypeError("protocol JSON array is malformed");
      index++;
    }
  }
  if (character === "{") {
    const keys = new Set();
    index = skipJSONWhitespace(input, index + 1);
    if (input[index] === "}") return index + 1;
    for (;;) {
      if (input[index] !== '"') throw new TypeError("protocol JSON object key must be a string");
      const keyEnd = scanJSONString(input, index);
      const key = JSON.parse(input.slice(index, keyEnd + 1));
      if (typeof key !== "string") throw new TypeError("protocol JSON object key must be a string");
      if (keys.has(key)) throw new TypeError(`protocol JSON contains duplicate object key ${key}`);
      keys.add(key);
      index = skipJSONWhitespace(input, keyEnd + 1);
      if (input[index] !== ":") throw new TypeError("protocol JSON object is missing a colon");
      index = skipJSONWhitespace(input, scanUniqueJSONValue(input, index + 1));
      if (input[index] === "}") return index + 1;
      if (input[index] !== ",") throw new TypeError("protocol JSON object is malformed");
      index = skipJSONWhitespace(input, index + 1);
    }
  }
  const end = scanJSONToken(input, index);
  if (end === index) throw new TypeError("protocol JSON value is malformed");
  return end;
}

function validateCanonicalJSONNumber(value) {
  if (!/^(0|-[1-9][0-9]*|[1-9][0-9]*)$/.test(value)) throw new TypeError(`protocol JSON number ${value} is not a canonical integer`);
  const parsed = BigInt(value);
  if (parsed < -9007199254740991n || parsed > 9007199254740991n) throw new TypeError(`protocol JSON number ${value} is outside the portable safe range`);
}

function parseWireJSON(input, maximumBytes, maximumDepth) {
  if (typeof input !== "string") throw new TypeError("protocol JSON input must be a string");
  if (utf8ByteLength(input) > maximumBytes) throw new TypeError(`protocol JSON exceeds ${maximumBytes} UTF-8 bytes`);
  let depth = 0;
  for (let index = 0; index < input.length; index++) {
    const character = input[index];
    if (character === '"') {
      index = scanJSONString(input, index);
      continue;
    }
    if (character === "{" || character === "[") {
      depth++;
      if (depth > maximumDepth) throw new TypeError(`protocol JSON exceeds depth ${maximumDepth}`);
      continue;
    }
    if (character === "}" || character === "]") {
      depth--;
      continue;
    }
    if (character === "-" || (character >= "0" && character <= "9")) {
      const end = scanJSONToken(input, index);
      validateCanonicalJSONNumber(input.slice(index, end));
      index = end - 1;
    }
  }
  const end = scanUniqueJSONValue(input, skipJSONWhitespace(input, 0));
  if (skipJSONWhitespace(input, end) !== input.length) throw new TypeError("protocol JSON must contain exactly one value");
  return JSON.parse(input);
}

function dialectKeywordSchemaType(meta, schema) {
  let resolved = schema;
  if (typeof schema.$ref === "string" && schema.$ref.startsWith("#/$defs/")) {
    resolved = meta.$defs[schema.$ref.slice("#/$defs/".length)];
  }
  assert.ok(resolved && typeof resolved.type === "string", `keyword schema has no concrete type: ${JSON.stringify(schema)}`);
  return resolved.type === "integer" ? "number" : resolved.type;
}

function addLayerDrawFormats(ajv) {
  const signed = /^(0|-[1-9][0-9]*|[1-9][0-9]*)$/;
  const unsigned = /^(0|[1-9][0-9]*)$/;
  const positive = /^[1-9][0-9]*$/;
  ajv.addFormat("int64-decimal", {type: "string", validate: (value) => canonicalInteger(value, -(2n ** 63n), (2n ** 63n) - 1n, signed)});
  ajv.addFormat("positive-int64-decimal", {type: "string", validate: (value) => canonicalInteger(value, 1n, (2n ** 63n) - 1n, positive)});
  ajv.addFormat("nonnegative-int64-decimal", {type: "string", validate: (value) => canonicalInteger(value, 0n, (2n ** 63n) - 1n, unsigned)});
  ajv.addFormat("uint64-decimal", {type: "string", validate: (value) => canonicalInteger(value, 0n, (2n ** 64n) - 1n, unsigned)});
  ajv.addFormat("safe-integer-decimal", {type: "string", validate: (value) => canonicalInteger(value, -(2n ** 53n) + 1n, (2n ** 53n) - 1n, signed)});
  ajv.addFormat("positive-safe-integer-decimal", {type: "string", validate: (value) => canonicalInteger(value, 1n, (2n ** 53n) - 1n, positive)});
  ajv.addFormat("nonnegative-safe-integer-decimal", {type: "string", validate: (value) => canonicalInteger(value, 0n, (2n ** 53n) - 1n, unsigned)});
  ajv.addFormat("finite-binary64-decimal", {type: "string", validate: (value) => canonicalBinary64(value, false)});
  ajv.addFormat("positive-finite-binary64-decimal", {type: "string", validate: (value) => canonicalBinary64(value, true)});
  ajv.addFormat("protocol-version", {type: "string", validate: (value) => protocolVersion(value) !== undefined});
  ajv.addFormat("protocol-version-range", {type: "string", validate: (value) => protocolRange(value) !== undefined});
  ajv.addFormat("protocol-version-or-range", {type: "string", validate: (value) => protocolVersion(value) !== undefined || protocolRange(value) !== undefined});
  ajv.addFormat("canonical-source-path", {type: "string", validate: canonicalSourcePath});
  ajv.addFormat("date-time", {type: "string", validate: realUTCDateTime});
}

function addLayerDrawVocabulary(ajv, meta) {
  const registrations = new Map();
  const register = (definition) => {
    assert.equal(registrations.has(definition.keyword), false, `duplicate LayerDraw keyword implementation ${definition.keyword}`);
    registrations.set(definition.keyword, definition);
  };
  register({keyword: "x-layerdraw-go-package", schemaType: "string", errors: false, validate: () => true});
  register({keyword: "x-layerdraw-max-json-bytes", schemaType: "number", rootValidate: (value) => Number.isSafeInteger(value) && value > 0, errors: false, validate: (maximum, data) => fitsCanonicalJSONBytes(data, maximum)});
  register({keyword: "x-layerdraw-max-json-depth", schemaType: "number", rootValidate: (value) => Number.isSafeInteger(value) && value > 0, errors: false, validate: (maximum, data) => hasContainerDepth(data, maximum)});
  register({keyword: "x-layerdraw-ts-module", schemaType: "string", errors: false, validate: () => true});
  register({keyword: "x-layerdraw-scalar-unicode", schemaType: "boolean", rootValidate: (value) => value === true, errors: false, validate(enabled, data) {
    return !enabled || hasScalarUnicodeTree(data);
  }});
  register({keyword: "x-layerdraw-canonical-identifier-order", schemaType: "boolean", type: "array", errors: false, validate(enabled, data) {
    if (!enabled) return true;
    return data.every(canonicalLocalIdentifier) && data.every((item, index) => index === 0 || data[index - 1] < item);
  }});
  register({keyword: "x-layerdraw-canonical-enum-order", schemaType: "boolean", type: "array", errors: false, validate(enabled, data, parentSchema) {
    if (!enabled) return true;
    const values = parentSchema?.items?.enum;
    if (!Array.isArray(values)) return false;
    const ranks = new Map(values.map((value, index) => [value, index]));
    return data.every((item, index) => index === 0 || ranks.has(item) && ranks.has(data[index - 1]) && ranks.get(data[index - 1]) < ranks.get(item));
  }});
  register({keyword: "x-layerdraw-unicode-scalar-order", schemaType: "boolean", type: "array", errors: false, validate(enabled, data) {
    if (!enabled) return true;
    return data.every((item) => typeof item === "string") && data.every((item, index) => index === 0 || compareUnicodeScalars(data[index - 1], item) < 0);
  }});
  register({keyword: "x-layerdraw-stable-address-order", schemaType: "string", type: "array", errors: false, validate(selector, data) {
    const address = (item) => selector === "$item" ? item : item?.[selector];
    for (let index = 1; index < data.length; index++) {
      const left = address(data[index - 1]);
      const right = address(data[index]);
      if (typeof left !== "string" || typeof right !== "string" || compareStableAddresses(left, right) >= 0) return false;
    }
    return true;
  }});
  register({keyword: "x-layerdraw-address-owners", schemaType: "array", type: "object", errors: false, validate(rules, data) {
    if (data === null || Array.isArray(data)) return true;
    return rules.every((rule) => {
      if (!Object.prototype.hasOwnProperty.call(data, rule.owner)) return true;
      const owner = data[rule.owner];
      if (typeof owner !== "string") return false;
      const rawChildren = data[rule.children];
      let children;
      if (rule.selector === "$value") children = [rawChildren];
      else if (rule.selector === "$propertyNames") {
        if (rawChildren === null || typeof rawChildren !== "object" || Array.isArray(rawChildren)) return false;
        children = Object.keys(rawChildren);
      } else {
        if (!Array.isArray(rawChildren)) return false;
        children = rawChildren.map((item) => item?.[rule.selector]);
      }
      return children.every((child) => typeof child === "string" && hasDirectStableAddressOwner(owner, child));
    });
  }});
  register({keyword: "x-layerdraw-address-terminal-id", schemaType: "object", type: "object", errors: false, validate(rule, data) {
    if (data === null || Array.isArray(data)) return true;
    return typeof data[rule.address] === "string" && typeof data[rule.id] === "string" && data[rule.address].split(":").at(-1) === data[rule.id];
  }});
  register({keyword: "x-layerdraw-disjoint-arrays", schemaType: "array", errors: false, validate(rules, data) {
    if (data === null || typeof data !== "object" || Array.isArray(data)) return true;
    return rules.every((rule) => {
      const leftValues = Object.prototype.hasOwnProperty.call(data, rule.left) ? data[rule.left] : [];
      const rightValues = Object.prototype.hasOwnProperty.call(data, rule.right) ? data[rule.right] : [];
      if (!Array.isArray(leftValues) || !Array.isArray(rightValues)) return false;
      const left = new Set(leftValues);
      return rightValues.every((item) => !left.has(item));
    });
  }});
  register({keyword: "x-layerdraw-disjoint-array-keys", schemaType: "array", type: "object", errors: false, validate(rules, data) {
    if (data === null || Array.isArray(data)) return true;
    return rules.every((rule) => {
      const items = data[rule.array];
      const strings = data[rule.strings];
      if (!Array.isArray(items) || !Array.isArray(strings) || !strings.every((item) => typeof item === "string")) return false;
      const reserved = new Set(strings);
      return items.every((item) => item !== null && typeof item === "object" && typeof item[rule.property] === "string" && !reserved.has(item[rule.property]));
    });
  }});
  register({keyword: "x-layerdraw-tagged-union", schemaType: "object", errors: false, validate(rule, data) {
    if (data === null || typeof data !== "object" || Array.isArray(data)) return true;
    const variant = rule.variants[String(data[rule.property])];
    if (variant === undefined) return false;
    const own = (key) => Object.prototype.hasOwnProperty.call(data, key);
    return (variant.required ?? []).every(own) && (variant.forbidden ?? []).every((key) => !own(key)) &&
      (variant.empty ?? []).every((key) => Array.isArray(data[key]) && data[key].length === 0) &&
      (variant.non_empty ?? []).every((key) => Array.isArray(data[key]) && data[key].length > 0) &&
      Object.entries(variant.allowed_values ?? {}).every(([key, values]) => !own(key) || values.includes(data[key]));
  }});
  register({keyword: "x-layerdraw-diff-source", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object" || Array.isArray(data) || data.kind !== "diff") return true;
    return typeof data.before === "string" && data.before.length > 0 && typeof data.after === "string" && data.after.length > 0 && data.before !== data.after &&
      (Object.prototype.hasOwnProperty.call(data, "query_address") || (data.arguments !== null && typeof data.arguments === "object" && !Array.isArray(data.arguments) && Object.keys(data.arguments).length === 0));
  }});
  register({keyword: "x-layerdraw-export-recipe", schemaType: "boolean", type: "object", errors: false, validate(enabled, data) {
    return !enabled || data === null || Array.isArray(data) || validExportRecipe(data);
  }});
  register({keyword: "x-layerdraw-view-recipe", schemaType: "boolean", type: "object", errors: false, validate(enabled, data) {
    return !enabled || data === null || Array.isArray(data) || validViewRecipe(data);
  }});
  register({keyword: "x-layerdraw-outcome-envelope", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object" || Array.isArray(data)) return true;
    const own = (key) => Object.prototype.hasOwnProperty.call(data, key);
    if (data.outcome === "success") return own("payload") && !own("failure");
    if (data.outcome === "rejected") return !own("payload") && !own("failure") && Array.isArray(data.diagnostics) && data.diagnostics.length > 0;
    if (data.outcome === "failed" || data.outcome === "cancelled") return !own("payload") && own("failure");
    return true;
  }});
  register({keyword: "x-layerdraw-ordered-range", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object") return true;
    try { return BigInt(data.start_byte) <= BigInt(data.end_byte); } catch { return false; }
  }});
  register({keyword: "x-layerdraw-ordered-pairs", schemaType: "array", type: "object", errors: false, validate(rules, data) {
    if (data === null || Array.isArray(data)) return true;
    return rules.every((rule) => orderedPair(data, rule));
  }});
  register({keyword: "x-layerdraw-operator-value", schemaType: "object", errors: false, validate(rule, data) {
    if (data === null || typeof data !== "object" || typeof data[rule.operator] !== "string") return true;
    const present = Object.prototype.hasOwnProperty.call(data, rule.value);
    return rule.valueless.includes(data[rule.operator]) ? !present : present;
  }});
  register({keyword: "x-layerdraw-protocol-offer", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object") return true;
    const range = protocolRange(data.supported_range);
    if (range === undefined || !Array.isArray(data.versions)) return false;
    const seen = new Set();
    return data.versions.every((binding) => {
      const version = protocolVersion(binding.version);
      if (version === undefined || seen.has(binding.version) || compareVersion(version, range[0]) < 0 || compareVersion(version, range[1]) > 0) return false;
      seen.add(binding.version);
      return true;
    });
  }});
  register({keyword: "x-layerdraw-limit-capability", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object") return true;
    try { return BigInt(data.default_value) <= BigInt(data.hard_maximum) && BigInt(data.effective_maximum) <= BigInt(data.hard_maximum); } catch { return false; }
  }});
  register({keyword: "x-layerdraw-unique-array-keys", schemaType: "array", errors: false, validate(rules, data) {
    if (data === null || typeof data !== "object") return true;
    return rules.every((rule) => {
      const seen = new Set();
      return Array.isArray(data[rule.array]) && data[rule.array].every((item) => !seen.has(item[rule.property]) && Boolean(seen.add(item[rule.property])));
    });
  }});
  const inventory = Object.keys(meta.properties).filter((keyword) => keyword.startsWith("x-layerdraw-")).sort();
  assert.deepEqual([...registrations.keys()].sort(), inventory, "Ajv LayerDraw keyword implementations must exactly match the published dialect inventory");
  const rootRequirements = new Map();
  for (const keyword of inventory) {
    const registration = registrations.get(keyword);
    const schemaType = dialectKeywordSchemaType(meta, meta.properties[keyword]);
    assert.equal(registration.schemaType, schemaType, `Ajv schema type for ${keyword} must derive from the published dialect`);
    const {rootValidate, ...definition} = registration;
    ajv.addKeyword({...definition, schemaType});
    if (rootValidate !== undefined) rootRequirements.set(keyword, rootValidate);
  }
  return rootRequirements;
}

async function authority() {
  const meta = await readJSON("meta/layerdraw-protocol-schema-v1.json");
  const documents = await Promise.all([
    readJSON("protocol-common/v1.schema.json"),
    readJSON("semantic/v1.schema.json"),
    readJSON("engine-protocol/v1.schema.json"),
  ]);
  const ajv = new Ajv2020({allErrors: true, strict: true, validateFormats: true});
  const rootRequirements = addLayerDrawVocabulary(ajv, meta);
  addLayerDrawFormats(ajv);
  ajv.addMetaSchema(meta);
  for (const document of documents) {
    for (const [keyword, validate] of rootRequirements) assert.equal(validate(document[keyword]), true, `${document.$id} must assert a valid ${keyword}`);
    ajv.addSchema(document);
  }
  const byID = new Map(documents.map((document) => [document.$id, document]));
  const compile = (id, name) => ajv.compile({allOf: [{$ref: id}, {$ref: `${id}#/$defs/${name}`}]});
  compile.wire = (id, name) => {
    const document = byID.get(id);
    assert.ok(document, `unknown schema resource ${id}`);
    const validate = compile(id, name);
    return (input) => {
      try {
        const value = parseWireJSON(input, document["x-layerdraw-max-json-bytes"], document["x-layerdraw-max-json-depth"]);
        return validate(value);
      } catch {
        return false;
      }
    };
  };
  return compile;
}

test("Ajv registration fails closed against the published dialect inventory and shapes", async () => {
  const published = await readJSON("meta/layerdraw-protocol-schema-v1.json");
  const missing = structuredClone(published);
  delete missing.properties["x-layerdraw-diff-source"];
  assert.throws(
    () => addLayerDrawVocabulary(new Ajv2020({strict: true}), missing),
    /must exactly match the published dialect inventory/,
  );
  const mistyped = structuredClone(published);
  mistyped.properties["x-layerdraw-diff-source"] = {type: "string"};
  assert.throws(
    () => addLayerDrawVocabulary(new Ajv2020({strict: true}), mistyped),
    /must derive from the published dialect/,
  );
});

test("normalized schema and document authority require request lifetime and the exact byte profile", async () => {
  const engine = await readJSON("engine-protocol/v1.schema.json");
  const readme = await readFile(new URL("README.md", schemaRoot), "utf8");
  const descriptions = {
    NormalizedPackArtifactBlobRef: "The public normalized Pack artifact bytes contain exactly the same JSON value as the canonical normalized Pack document; the canonical RFC 8785 UTF-8 bytes have no trailing LF, and these public bytes are exactly those canonical bytes followed by one LF.",
    NormalizedPackCanonicalBlobRef: "The canonical normalized Pack document bytes: RFC 8785 UTF-8 with no trailing LF.",
    NormalizedProjectArtifactBlobRef: "The public normalized Project artifact bytes contain exactly the same JSON value as the canonical normalized Project document; the canonical RFC 8785 UTF-8 bytes have no trailing LF, and these public bytes are exactly those canonical bytes followed by one LF.",
    NormalizedProjectCanonicalBlobRef: "The canonical normalized Project document bytes: RFC 8785 UTF-8 with no trailing LF.",
  };
  for (const [name, description] of Object.entries(descriptions)) {
    assert.equal(engine.$defs[name].description, description);
    assert.deepEqual(engine.$defs[name].properties.lifetime, {type: "string", const: "request"});
  }
  for (const authorityText of [JSON.stringify(engine), readme]) {
    assert.doesNotMatch(authorityText, /\b(?:may be identical|may equal canonical)\b/i);
  }
  assert.match(readme, /Canonical and public artifact\s+roles contain exactly the same JSON value\. Canonical bytes are RFC 8785 UTF-8\s+with no trailing LF; public artifact bytes are exactly those canonical bytes\s+followed by one LF\./);
});

test("published dialect requires format assertion and every codec-critical format agrees with authority vectors", async (context) => {
  const meta = await readJSON("meta/layerdraw-protocol-schema-v1.json");
  assert.equal(meta.$vocabulary["https://json-schema.org/draft/2020-12/vocab/format-assertion"], true);
  const compile = await authority();
  const corpus = await readJSON("fixtures/conformance/formats-v1.json");
  for (const vector of corpus.vectors) await context.test(vector.name, () => {
    assert.equal(compile(vector.schema_id, vector.type)(vector.value), vector.valid);
  });
});

test("published LayerDraw schema vocabulary asserts unions, outcomes, ranges, and offers", async () => {
  const compile = await authority();
  const common = "https://schemas.layerdraw.dev/protocol-common/v1";
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  const engine = "https://schemas.layerdraw.dev/engine-protocol/v1";

  const total = compile(common, "TotalItems");
  assert.equal(total({known: true, exact: "1"}), true);
  assert.equal(total({known: false}), true);
  assert.equal(total({known: true}), false);
  assert.equal(total({known: false, exact: "1"}), false);

  const offer = compile(common, "ProtocolOffer");
  const digest = `sha256:${"a".repeat(64)}`;
  assert.equal(offer({name: "engine", supported_range: "1.0..1.2", versions: [{version: "1.0", schema_digest: digest}, {version: "1.2", schema_digest: digest}]}), true);
  assert.equal(offer({name: "engine", supported_range: "1.2..1.0", versions: [{version: "1.0", schema_digest: digest}]}), false);
  assert.equal(offer({name: "engine", supported_range: "1.9..2.0", versions: [{version: "1.9", schema_digest: digest}]}), false);
  assert.equal(offer({name: "engine", supported_range: "1.0..1.2", versions: [{version: "1.3", schema_digest: digest}]}), false);

  const handshake = compile(common, "HandshakeRequest");
  const handshakeBase = {client_release: "1.0.0", protocols: [{name: "engine", supported_range: "1.0..1.0", versions: [{version: "1.0", schema_digest: digest}]}], required_capabilities: ["engine.compile"], optional_capabilities: ["renderer.svg"]};
  assert.equal(handshake(handshakeBase), true);
  assert.equal(handshake({...handshakeBase, required_capabilities: ["engine.compile", "engine.compile"]}), false);
  assert.equal(handshake({...handshakeBase, optional_capabilities: ["engine.compile"]}), false);

  const stableAddress = compile(semantic, "StableAddress");
  assert.equal(stableAddress("ldl:project:p:entity-type:t:column:c"), true);
  assert.equal(stableAddress("ldl:pack:publisher:pack:entity:e"), false);
  assert.equal(stableAddress("ldl:project:p:entity-type:t:row:r"), false);

  const projectRoot = compile(semantic, "ProjectRootAddress");
  assert.equal(projectRoot("ldl:project:p"), true);
  assert.equal(projectRoot("ldl:pack:publisher:pack"), false);
  assert.equal(projectRoot("ldl:project:p:view:v"), false);
  const packRoot = compile(semantic, "PackRootAddress");
  assert.equal(packRoot("ldl:pack:publisher:pack"), true);
  assert.equal(packRoot("ldl:project:p"), false);
  assert.equal(packRoot("ldl:pack:publisher:pack:view:v"), false);
  const publicationRef = (media_type) => ({blob_id: "b", digest, lifetime: "request", media_type, size: "1"});
  const normalizedProject = compile(engine, "NormalizedProjectArtifact");
  const projectPublication = {project_address: "ldl:project:p", canonical_json: publicationRef("application/vnd.layerdraw.normalized-project.v1+json"), artifact_json: publicationRef("application/vnd.layerdraw.project.v1+json")};
  assert.equal(normalizedProject(projectPublication), true);
  assert.equal(normalizedProject({...projectPublication, project_address: "ldl:pack:publisher:pack"}), false);
  assert.equal(normalizedProject({...projectPublication, project_address: "ldl:project:p:view:v"}), false);
  const normalizedPack = compile(engine, "NormalizedPackArtifact");
  const packPublication = {pack_address: "ldl:pack:publisher:pack", canonical_json: publicationRef("application/vnd.layerdraw.normalized-pack.v1+json"), artifact_json: publicationRef("application/vnd.layerdraw.pack.v1+json")};
  assert.equal(normalizedPack(packPublication), true);
  assert.equal(normalizedPack({...packPublication, pack_address: "ldl:project:p"}), false);
  assert.equal(normalizedPack({...packPublication, pack_address: "ldl:pack:publisher:pack:view:v"}), false);

  const columnSource = compile(semantic, "ViewTableColumnSource");
  for (const value of [
    {kind: "field", field: "tags"},
    {kind: "attribute", column_addresses: ["ldl:project:p:entity-type:t:column:c"]},
    {kind: "relation_endpoint", endpoint: "from", field: "display_name"},
    {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:relation-type:r"]},
    {kind: "state", field_path: "system.updated_at"},
  ]) assert.equal(columnSource(value), true);
  for (const value of [
    {kind: "field"},
    {kind: "field", field: "not_a_field"},
    {kind: "attribute"},
    {kind: "attribute", column_addresses: [], field: "id"},
    {kind: "relation_endpoint", endpoint: "from"},
    {kind: "relation_endpoint", endpoint: "from", field: "description"},
    {kind: "derived_count"},
    {kind: "derived_count", direction: "both", field_path: "system.updated_at"},
    {kind: "state"},
    {kind: "state", field_path: "system.updated_at", relation_type_addresses: []},
    {kind: "state", field_path: "review.status"},
  ]) assert.equal(columnSource(value), false);

  const viewSource = compile(semantic, "ViewRecipeSource");
  const parameter = "ldl:project:p:query:q:parameter:x";
  const query = "ldl:project:p:query:q";
  const argumentsWithValue = {[parameter]: {kind: "string", string_value: "x"}};
  assert.equal(viewSource({kind: "diff", before: "base", after: "head", arguments: {}}), true);
  assert.equal(viewSource({kind: "diff", before: "base", after: "head", query_address: query, arguments: argumentsWithValue}), true);
  assert.equal(viewSource({kind: "diff", before: "", after: "head", arguments: {}}), false);
  assert.equal(viewSource({kind: "diff", before: "base", after: "", arguments: {}}), false);
  assert.equal(viewSource({kind: "diff", before: "same", after: "same", arguments: {}}), false);
  assert.equal(viewSource({kind: "diff", before: "base", after: "head", arguments: argumentsWithValue}), false);

  const predicate = compile(semantic, "RecipePredicate");
  assert.equal(predicate({kind: "field", field: "id", operator: "eq", value: {kind: "scalar", scalar_value: {kind: "string", string_value: "x"}}}), true);
  assert.equal(predicate({kind: "field"}), false);
  assert.equal(predicate({kind: "field", field: "id", operator: "exists", value: {kind: "scalar", scalar_value: {kind: "string", string_value: "x"}}}), false);

  const compileInput = compile(engine, "CompileInput");
  const base = {entry_path: "main.ldl", installed_pack_tree: [], mode: "project", project_source_tree: [{path: "main.ldl", blob: {blob_id: "b", digest, lifetime: "request", media_type: "text/plain", size: "1"}}], referenced_assets: [], resolved_dependencies: {format: "layerdraw-resolved", format_version: 1, installs: [], language: 1}, resource_limits: {}};
  assert.equal(compileInput(base), true);
  assert.equal(compileInput({...base, mode: "pack", root_pack_id: "publisher/pack"}), false);

  for (const [name, mediaType] of [["QueryRecipeBlobRef", "query"], ["ViewRecipeBlobRef", "view"], ["ExportRecipeBlobRef", "export"]]) {
    const recipeRef = compile(engine, name);
    const value = {blob_id: mediaType, digest, lifetime: "request", media_type: `application/vnd.layerdraw.${mediaType}-recipe.v1+json`, size: "1"};
    assert.equal(recipeRef(value), true);
    assert.equal(recipeRef({...value, lifetime: "session"}), false);
    assert.equal(recipeRef({...value, lifetime: "persistent"}), false);
  }
});

test("independent schema authority matches every shared View source vector", async (context) => {
  const compile = await authority();
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  const corpus = await readJSON("fixtures/conformance/view-sources-v1.json");
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 30);
  assert.equal(corpus.rejection_cases.length, 59);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} accepted`, () => {
    assert.equal(compile.wire(semantic, vector.type)(vector.input), true);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejected`, () => {
    assert.equal(compile.wire(semantic, vector.type)(vector.input), false);
  });
});

test("independent schema authority enforces exact raw wire bounds and hostile object syntax", async (context) => {
  const common = "https://schemas.layerdraw.dev/protocol-common/v1";
  const document = await readJSON("protocol-common/v1.schema.json");
  const corpus = await readJSON("fixtures/conformance/v1.json");
  const compile = await authority();
  const validateValue = compile(common, "JsonValue");
  const validateWire = compile.wire(common, "JsonValue");
  const maximumBytes = document["x-layerdraw-max-json-bytes"];
  const maximumDepth = document["x-layerdraw-max-json-depth"];
  assert.equal(maximumBytes, corpus.max_json_bytes);
  assert.equal(maximumDepth, corpus.max_json_depth);

  const exactBytes = `"${"a".repeat(maximumBytes - 2)}"`;
  const excessiveBytes = `"${"a".repeat(maximumBytes - 1)}"`;
  let exactDepth = '"x"';
  for (let depth = 0; depth < maximumDepth; depth++) exactDepth = `[${exactDepth}]`;
  const excessiveDepth = `[${exactDepth}]`;
  for (const [name, input, valid] of [
    ["max JSON bytes", exactBytes, true],
    ["max JSON bytes plus one", excessiveBytes, false],
    ["depth 128", exactDepth, true],
    ["depth 129", excessiveDepth, false],
  ]) await context.test(name, () => {
    assert.equal(validateWire(input), valid);
  });

  assert.equal(validateValue(JSON.parse(exactBytes)), true, "max-byte keyword exact boundary");
  assert.equal(validateValue(JSON.parse(excessiveBytes)), false, "max-byte keyword rejects rather than annotates");
  assert.equal(validateValue(JSON.parse(exactDepth)), true, "max-depth keyword exact boundary");
  assert.equal(validateValue(JSON.parse(excessiveDepth)), false, "max-depth keyword rejects rather than annotates");

  const hostileNames = new Set(["duplicate_object_key", "escaped_equivalent_duplicate_object_key", "nested_duplicate_object_key", "unpaired_high_surrogate", "unpaired_low_surrogate"]);
  const hostile = corpus.rejection_cases.filter((vector) => hostileNames.has(vector.name));
  assert.equal(hostile.length, hostileNames.size);
  for (const vector of hostile) await context.test(vector.name, () => {
    assert.equal(validateWire(vector.input), false);
  });
  assert.equal(validateWire(`"${String.fromCharCode(0xd800)}"`), false, "raw unpaired high surrogate");
  assert.equal(validateWire(`{"${String.fromCharCode(0xdc00)}":null}`), false, "raw unpaired low surrogate member name");
});

test("independent schema authority matches the complete View and Export semantic corpus", async (context) => {
  const compile = await authority();
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  const corpus = await readJSON("fixtures/conformance/view-export-semantics-v1.json");
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 34);
  assert.equal(corpus.rejection_cases.length, 94);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} accepted`, () => {
    assert.equal(compile(semantic, vector.type)(vector.value), true);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejected`, () => {
    assert.equal(compile(semantic, vector.type)(vector.value), false);
  });
});

test("independent schema authority matches the complete Query authority corpus", async (context) => {
  const compile = await authority();
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  const corpus = await readJSON("fixtures/conformance/query-authority-v1.json");
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 20);
  assert.equal(corpus.rejection_cases.length, 55);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} accepted`, () => {
    assert.equal(compile(semantic, vector.type)(vector.value), true);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejected`, () => {
    assert.equal(compile(semantic, vector.type)(vector.value), false);
  });
});

test("independent schema authority matches the cross-cutting semantic root corpus", async (context) => {
  const compile = await authority();
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  const corpus = await readJSON("fixtures/conformance/semantic-root-authority-v1.json");
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 2);
  assert.equal(corpus.rejection_cases.length, 5);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} accepted`, () => {
    assert.equal(compile(semantic, vector.type)(vector.value), true);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejected`, () => {
    assert.equal(compile(semantic, vector.type)(vector.value), false);
  });
});

test("independent schema authority enforces the published recursive scalar-Unicode profile", async (context) => {
  const meta = await readJSON("meta/layerdraw-protocol-schema-v1.json");
  assert.deepEqual(meta.properties["x-layerdraw-scalar-unicode"], {type: "boolean", const: true});
  const compile = await authority();
  const corpus = await readJSON("fixtures/conformance/unicode-scalars-v1.json");
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 2);
  assert.equal(corpus.rejection_cases.length, 9);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} accepted`, () => {
    assert.equal(compile.wire(vector.schema_id, vector.type)(vector.input), true);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejected`, () => {
    assert.equal(compile.wire(vector.schema_id, vector.type)(vector.input), false);
  });
});
