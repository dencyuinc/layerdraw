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

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function hasOwn(value, key) {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function realDate(value) {
  const match = /^([0-9]{4})-([0-9]{2})-([0-9]{2})$/.exec(value);
  if (match === null || match[1] === "0000") return false;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1];
}

function validRecipeScalar(value) {
  if (value.kind === "date") return typeof value.string_value === "string" && realDate(value.string_value);
  if (value.kind === "datetime") {
    if (typeof value.string_value !== "string") return false;
    const match = /^([0-9]{4}-[0-9]{2}-[0-9]{2})T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.([0-9]{1,3}))?Z$/.exec(value.string_value);
    return match !== null && realDate(match[1]) && (match[2] === undefined || !match[2].endsWith("0"));
  }
  return value.kind !== "enum" || typeof value.string_value === "string" && value.string_value.length > 0;
}

function canonicalHostname(value) {
  return value.length > 0 && value.length <= 253 && value === value.toLowerCase() && !value.endsWith(".") &&
    value.split(".").every((label) => label.length > 0 && label.length <= 63 && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label));
}

function parseCanonicalIPv4(value) {
  const parts = value.split(".");
  return parts.length === 4 && parts.every((part) => /^(0|[1-9][0-9]{0,2})$/.test(part) && Number(part) <= 255) ? parts.map(Number) : undefined;
}

function parseIPv6(value) {
  if (value.includes("%")) return undefined;
  let expanded = value;
  if (value.includes(".")) {
    const colon = value.lastIndexOf(":");
    const ipv4 = colon < 0 ? undefined : parseCanonicalIPv4(value.slice(colon + 1));
    if (ipv4 === undefined) return undefined;
    expanded = value.slice(0, colon + 1) + ((ipv4[0] << 8) | ipv4[1]).toString(16) + ":" + ((ipv4[2] << 8) | ipv4[3]).toString(16);
  }
  if (!/^[0-9A-Fa-f:]+$/.test(expanded) || expanded.split("::").length > 2) return undefined;
  const hasElision = expanded.includes("::");
  const halves = expanded.split("::");
  const left = halves[0] === "" ? [] : halves[0].split(":");
  const right = !hasElision || halves[1] === "" ? [] : halves[1].split(":");
  if (![...left, ...right].every((part) => /^[0-9A-Fa-f]{1,4}$/.test(part))) return undefined;
  const omitted = 8 - left.length - right.length;
  if (hasElision ? omitted < 1 : omitted !== 0) return undefined;
  const words = [...left.map((part) => Number.parseInt(part, 16)), ...Array(omitted).fill(0), ...right.map((part) => Number.parseInt(part, 16))];
  return words.flatMap((word) => [word >>> 8, word & 255]);
}

function formatIPv6(bytes) {
  if (bytes.slice(0, 10).every((value) => value === 0) && bytes[10] === 255 && bytes[11] === 255) return `::ffff:${bytes.slice(12).join(".")}`;
  const words = Array.from({length: 8}, (_, index) => (bytes[index * 2] << 8) | bytes[index * 2 + 1]);
  let bestStart = -1;
  let bestLength = 0;
  for (let index = 0; index < words.length;) {
    if (words[index] !== 0) { index++; continue; }
    let end = index;
    while (end < words.length && words[end] === 0) end++;
    if (end - index > bestLength && end - index >= 2) { bestStart = index; bestLength = end - index; }
    index = end;
  }
  let result = "";
  for (let index = 0; index < words.length;) {
    if (index === bestStart) { result += "::"; index += bestLength; continue; }
    if (result !== "" && !result.endsWith(":")) result += ":";
    result += words[index].toString(16);
    index++;
  }
  return result;
}

function canonicalIP(value) {
  const ipv4 = parseCanonicalIPv4(value);
  if (ipv4 !== undefined) return {bytes: ipv4, bits: 32};
  const ipv6 = parseIPv6(value);
  return ipv6 !== undefined && formatIPv6(ipv6) === value ? {bytes: ipv6, bits: 128} : undefined;
}

function canonicalCIDR(value) {
  const parts = value.split("/");
  if (parts.length !== 2 || !/^(0|[1-9][0-9]*)$/.test(parts[1])) return false;
  const address = canonicalIP(parts[0]);
  const prefix = Number(parts[1]);
  if (address === undefined || !Number.isSafeInteger(prefix) || prefix > address.bits) return false;
  return address.bytes.every((byte, index) => {
    const remaining = prefix - index * 8;
    const mask = remaining >= 8 ? 255 : remaining <= 0 ? 0 : (255 << (8 - remaining)) & 255;
    return (byte & mask) === byte;
  });
}

const uriAlpha = (value) => /^[A-Za-z]$/.test(value);
const uriDigit = (value) => /^[0-9]$/.test(value);
const uriHex = (value) => /^[0-9A-Fa-f]$/.test(value);
const uriUnreserved = (value) => uriAlpha(value) || uriDigit(value) || "-._~".includes(value);
function validURIComponent(value, allowEmpty, extra) {
  if (value === "") return allowEmpty;
  for (let index = 0; index < value.length; index++) {
    const character = value[index];
    if (character === "%") {
      if (index + 2 >= value.length || !uriHex(value[index + 1]) || !uriHex(value[index + 2])) return false;
      index += 2;
    } else if (!uriUnreserved(character) && !("!$&'()*+,;=" + extra).includes(character)) return false;
  }
  return true;
}
function validIPLiteral(value) {
  if (parseIPv6(value) !== undefined) return true;
  if (value.length < 4 || value[0] !== "v" && value[0] !== "V") return false;
  const dot = value.indexOf(".");
  return dot >= 2 && Array.from(value.slice(1, dot)).every(uriHex) && value.slice(dot + 1) !== "" && validURIComponent(value.slice(dot + 1), false, ":");
}
function validURIAuthority(value) {
  if (value.split("@").length > 2) return false;
  let hostPort = value;
  const at = value.indexOf("@");
  if (at >= 0) { if (!validURIComponent(value.slice(0, at), true, ":")) return false; hostPort = value.slice(at + 1); }
  if (hostPort.startsWith("[")) {
    const close = hostPort.indexOf("]");
    if (close <= 1) return false;
    const rest = hostPort.slice(close + 1);
    return validIPLiteral(hostPort.slice(1, close)) && (rest === "" || rest.startsWith(":") && Array.from(rest.slice(1)).every(uriDigit));
  }
  if (hostPort.includes("[") || hostPort.includes("]")) return false;
  let host = hostPort;
  const colon = hostPort.lastIndexOf(":");
  if (colon >= 0) { host = hostPort.slice(0, colon); if (host.includes(":") || !Array.from(hostPort.slice(colon + 1)).every(uriDigit)) return false; }
  return validURIComponent(host, true, "");
}
function validAbsoluteURI(value) {
  const colon = value.indexOf(":");
  if (colon <= 0 || !/^[A-Za-z][A-Za-z0-9+.-]*$/.test(value.slice(0, colon)) || Array.from(value).some((character) => character.codePointAt(0) >= 128 || character.codePointAt(0) < 32 || character.codePointAt(0) === 127) || value.includes("\\")) return false;
  for (let index = 0; index < value.length; index++) {
    const character = value[index];
    if (character === "%") { if (index + 2 >= value.length || !uriHex(value[index + 1]) || !uriHex(value[index + 2])) return false; index += 2; }
    else if (!uriUnreserved(character) && !":/?#[]@!$&'()*+,;=%".includes(character)) return false;
  }
  const remainder = value.slice(colon + 1);
  if (remainder.split("#").length > 2) return false;
  const hash = remainder.indexOf("#");
  const beforeFragment = hash < 0 ? remainder : remainder.slice(0, hash);
  if (hash >= 0 && !validURIComponent(remainder.slice(hash + 1), true, "/?:@")) return false;
  const question = beforeFragment.indexOf("?");
  const hierarchical = question < 0 ? beforeFragment : beforeFragment.slice(0, question);
  if (question >= 0 && !validURIComponent(beforeFragment.slice(question + 1), true, "/?:@")) return false;
  if (hierarchical.startsWith("//")) {
    const authorityAndPath = hierarchical.slice(2);
    const slash = authorityAndPath.indexOf("/");
    return validURIAuthority(slash < 0 ? authorityAndPath : authorityAndPath.slice(0, slash)) && validURIComponent(slash < 0 ? "" : authorityAndPath.slice(slash), true, "/:@");
  }
  return validURIComponent(hierarchical, true, "/:@");
}

function validCanonicalStringFormat(format, value) {
  if (format === "hostname") return canonicalHostname(value);
  if (format === "email") {
    const match = /^[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\.[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+)*@([A-Za-z0-9.-]+)$/.exec(value);
    return match !== null && canonicalHostname(match[1].toLowerCase());
  }
  if (format === "ipv4") return parseCanonicalIPv4(value) !== undefined;
  if (format === "ipv6") { const parsed = parseIPv6(value); return parsed !== undefined && formatIPv6(parsed) === value; }
  if (format === "cidr") return canonicalCIDR(value);
  if (format === "uri") return validAbsoluteURI(value);
  return false;
}

function validQueryParameter(value) {
  const {value_type: valueType, reserved_enum_values: reserved} = value;
  if (typeof valueType !== "string" || !Array.isArray(reserved)) return false;
  const hasEnum = hasOwn(value, "enum_values");
  const hasFormat = hasOwn(value, "format");
  const hasMin = hasOwn(value, "min");
  const hasMax = hasOwn(value, "max");
  const hasMinLength = hasOwn(value, "min_length");
  const hasMaxLength = hasOwn(value, "max_length");
  if (valueType === "enum") {
    if (!Array.isArray(value.enum_values) || value.enum_values.length === 0 ||
        !value.enum_values.every((item) => typeof item === "string" && item.length > 0) ||
        !reserved.every((item) => typeof item === "string" && item.length > 0)) return false;
  } else if (hasEnum || reserved.length !== 0) return false;
  if (hasFormat && valueType !== "string" ||
      (hasMin || hasMax) && valueType !== "integer" && valueType !== "number" ||
      (hasMinLength || hasMaxLength) && valueType !== "string") return false;
  if (valueType === "integer") {
    for (const property of ["min", "max"]) {
      if (hasOwn(value, property) && !canonicalInteger(value[property], -(2n ** 53n) + 1n, (2n ** 53n) - 1n, /^(0|-[1-9][0-9]*|[1-9][0-9]*)$/)) return false;
    }
  }
  if (!hasOwn(value, "default")) return true;
  const defaultValue = value.default;
  if (!isObject(defaultValue) || defaultValue.kind !== valueType) return false;
  if (valueType === "enum") return value.enum_values.includes(defaultValue.string_value);
  if (valueType === "string") {
    if (typeof defaultValue.string_value !== "string") return false;
    const length = BigInt(Array.from(defaultValue.string_value).length);
    if (hasMinLength && length < BigInt(value.min_length) || hasMaxLength && length > BigInt(value.max_length)) return false;
    return !hasFormat || validCanonicalStringFormat(value.format, defaultValue.string_value);
  }
  if (valueType === "integer" || valueType === "number") {
    const property = valueType === "integer" ? "integer_value" : "number_value";
    const number = Number(defaultValue[property]);
    return Number.isFinite(number) && (!hasMin || number >= Number(value.min)) && (!hasMax || number <= Number(value.max));
  }
  return true;
}

function stableAddressSubject(address) {
  const parts = address.split(":");
  if (parts.length < 3 || parts[0] !== "ldl") return undefined;
  let pathStart = 3;
  let rootKind = "project";
  if (parts[1] === "pack") {
    if (parts.length < 4) return undefined;
    pathStart = 4;
    rootKind = "pack";
  } else if (parts[1] !== "project") return undefined;
  if (parts.length === pathStart) return {kind: rootKind, owner: ""};
  if ((parts.length - pathStart) % 2 !== 0) return undefined;
  const terminal = parts.at(-2);
  let kind = new Map([
    ["entity-type", "entity_type"], ["relation-type", "relation_type"], ["layer", "layer"], ["entity", "entity"],
    ["relation", "relation"], ["query", "query"], ["view", "view"], ["reference", "reference"],
    ["parameter", "query_parameter"], ["table-column", "view_table_column"], ["export", "view_export"],
  ]).get(terminal);
  if (terminal === "row") kind = parts.at(-4) === "entity" ? "entity_row" : parts.at(-4) === "relation" ? "relation_row" : undefined;
  if (terminal === "column" || terminal === "constraint") {
    const prefix = parts.at(-4) === "entity-type" ? "entity_type" : parts.at(-4) === "relation-type" ? "relation_type" : undefined;
    kind = prefix === undefined ? undefined : `${prefix}_${terminal}`;
  }
  return kind === undefined ? undefined : {kind, owner: parts.slice(0, -2).join(":")};
}

function validStableAddressRoles(value, rules) {
  for (const rule of rules) {
    const expectedKind = value[rule.kind];
    if (typeof expectedKind !== "string") return false;
    const addresses = rule.address === undefined ? value[rule.addresses] : [value[rule.address]];
    if (!Array.isArray(addresses)) return false;
    const owner = rule.owner === undefined ? undefined : value[rule.owner];
    for (const address of addresses) {
      if (typeof address !== "string") return false;
      const subject = stableAddressSubject(address);
      if (subject === undefined || subject.kind !== expectedKind) return false;
      const ownerPresent = typeof owner === "string";
      if (rule.owner_policy === "children" && (!ownerPresent || subject.owner !== owner) ||
          rule.owner_policy === "exact" && ((subject.owner !== "") !== ownerPresent || ownerPresent && subject.owner !== owner) ||
          rule.owner_policy === "if_present" && ownerPresent && subject.owner !== owner ||
          rule.owner_policy === "row_only" && ((subject.kind === "entity_row" || subject.kind === "relation_row") !== ownerPresent || ownerPresent && subject.owner !== owner)) return false;
    }
  }
  return true;
}

const stateFields = [
  "system.created_at", "system.updated_at", "system.created_by.kind", "system.created_by.id", "system.created_by.display_name",
  "system.updated_by.kind", "system.updated_by.id", "system.updated_by.display_name", "system.created_revision", "system.updated_revision",
  "provenance.source.kind", "provenance.source.label", "provenance.source.uri", "provenance.source.external_id",
  "provenance.observed_at", "provenance.verified_at", "provenance.stale_after", "provenance.verified_by.kind",
  "provenance.verified_by.id", "provenance.verified_by.display_name", "provenance.confidence",
];
const stateSubjects = ["entity", "relation", "entity_row", "relation_row"];

function stateFieldValueType(field) {
  if (["system.created_at", "system.updated_at", "provenance.observed_at", "provenance.verified_at", "provenance.stale_after"].includes(field)) return "datetime";
  if (["system.created_by.kind", "system.updated_by.kind", "provenance.source.kind", "provenance.verified_by.kind"].includes(field)) return "enum";
  return field === "provenance.confidence" ? "number" : "string";
}

function validStateRead(value) {
  return typeof value.field_path === "string" && value.value_type === stateFieldValueType(value.field_path);
}

function validStateReadOrder(values) {
  let previous = -1;
  for (const value of values) {
    if (!isObject(value) || !validStateRead(value)) return false;
    const subjectRank = stateSubjects.indexOf(value.subject_kind);
    const fieldRank = stateFields.indexOf(value.field_path);
    const rank = subjectRank * stateFields.length + fieldRank;
    if (subjectRank < 0 || fieldRank < 0 || rank <= previous) return false;
    previous = rank;
  }
  return true;
}

function recipeOperand(raw) {
  if (!isObject(raw) || typeof raw.kind !== "string") return undefined;
  const operand = {
    kind: raw.kind,
    scalarType: typeof raw.scalar_type === "string" ? raw.scalar_type : "",
    addressKind: typeof raw.address_kind === "string" ? raw.address_kind : "",
  };
  return operand.kind === "scalar" && operand.scalarType !== "" || operand.kind === "address" && operand.addressKind !== "" || operand.kind === "string_set" ? operand : undefined;
}

function equalRecipeOperands(left, right) {
  return left.kind === right.kind && left.scalarType === right.scalarType && left.addressKind === right.addressKind;
}

function fieldRecipeOperand(field) {
  if (["id", "display_name", "description"].includes(field)) return {kind: "scalar", scalarType: "string", addressKind: ""};
  if (field === "tags") return {kind: "string_set", scalarType: "", addressKind: ""};
  if (field === "layer") return {kind: "address", scalarType: "", addressKind: "layer"};
  if (field === "from" || field === "to") return {kind: "address", scalarType: "", addressKind: "entity"};
  return undefined;
}

function compareRecipeScalars(left, right) {
  if (left.kind === "boolean") return Number(left.boolean_value) - Number(right.boolean_value);
  if (left.kind === "integer") return BigInt(left.integer_value) < BigInt(right.integer_value) ? -1 : BigInt(left.integer_value) > BigInt(right.integer_value) ? 1 : 0;
  if (left.kind === "number") return Number(left.number_value) - Number(right.number_value);
  return compareUnicodeScalars(String(left.string_value), String(right.string_value));
}

function validRecipeScalarSet(raw, scalarType) {
  return Array.isArray(raw) && raw.every((value) => isObject(value) && value.kind === scalarType);
}

function validRecipePredicateValue(value, operator, operand) {
  if (value.kind === "parameter") return operator !== "in" && operator !== "not_in" &&
    (operand.kind === "scalar" || operand.kind === "string_set" && operator === "contains");
  if (operator === "in" || operator === "not_in") {
    if (operand.kind === "scalar") return value.kind === "scalar_set" && validRecipeScalarSet(value.scalar_values, operand.scalarType);
    if (operand.kind === "address" && value.kind === "address_set" && Array.isArray(value.address_values)) {
      return value.address_values.every((address) => typeof address === "string" && stableAddressSubject(address)?.kind === operand.addressKind);
    }
    return false;
  }
  if (operand.kind === "string_set") {
    if (operator === "eq" || operator === "ne") return value.kind === "scalar_set" && validRecipeScalarSet(value.scalar_values, "string");
    return operator === "contains" && value.kind === "scalar" && isObject(value.scalar_value) && value.scalar_value.kind === "string";
  }
  if (operand.kind === "scalar") return value.kind === "scalar" && isObject(value.scalar_value) && value.scalar_value.kind === operand.scalarType;
  return operand.kind === "address" && value.kind === "address" && typeof value.address_value === "string" && stableAddressSubject(value.address_value)?.kind === operand.addressKind;
}

function validRecipePredicate(value, predicateKind) {
  if (value.kind === "all" || value.kind === "any") return Array.isArray(value.children) && value.children.every((child) => isObject(child) && validRecipePredicate(child, predicateKind));
  if (value.kind === "not") return isObject(value.child) && validRecipePredicate(value.child, predicateKind);
  if (value.kind === "rows") return isObject(value.predicate) && validRecipePredicate(value.predicate, "row");
  if (value.kind !== "field" && value.kind !== "cell" && value.kind !== "state") return true;
  const operand = recipeOperand(value.operand_type);
  if (operand === undefined) return false;
  if (value.kind === "field") {
    const expected = fieldRecipeOperand(String(value.field));
    if (expected !== undefined && !equalRecipeOperands(operand, expected)) return false;
  }
  if (value.kind === "state" && !equalRecipeOperands(operand, {kind: "scalar", scalarType: stateFieldValueType(String(value.field_path)), addressKind: ""})) return false;
  const operator = value.operator;
  let compatible = ["eq", "ne", "exists", "missing"].includes(operator);
  if (["lt", "lte", "gt", "gte"].includes(operator)) compatible = operand.kind === "scalar" && ["integer", "number", "date", "datetime"].includes(operand.scalarType);
  if (["in", "not_in"].includes(operator)) compatible = operand.kind === "scalar" || operand.kind === "address";
  if (operator === "contains") compatible = operand.kind === "string_set" || operand.kind === "scalar" && operand.scalarType === "string";
  if (["starts_with", "ends_with"].includes(operator)) compatible = operand.kind === "scalar" && operand.scalarType === "string";
  return compatible && (["exists", "missing"].includes(operator) || isObject(value.value) && validRecipePredicateValue(value.value, operator, operand));
}

function contextFieldOperand(field, context) {
  if (["id", "display_name", "description"].includes(field)) return {kind: "scalar", scalarType: "string", addressKind: ""};
  if (field === "tags") return {kind: "string_set", scalarType: "", addressKind: ""};
  if (field === "layer") return context === "entity" ? {kind: "address", scalarType: "", addressKind: "layer"} : undefined;
  if (field === "from" || field === "to") return context === "relation" ? {kind: "address", scalarType: "", addressKind: "entity"} : undefined;
  if (field === "address") return context === "entity" ? {kind: "address", scalarType: "", addressKind: "entity"} : context === "relation" ? {kind: "address", scalarType: "", addressKind: "relation"} : undefined;
  if (field === "type") return context === "entity" ? {kind: "address", scalarType: "", addressKind: "entity_type"} : context === "relation" ? {kind: "address", scalarType: "", addressKind: "relation_type"} : undefined;
  return undefined;
}

function queryDependencySets() {
  return {
    layer: new Set(), entity_type: new Set(), relation_type: new Set(), entity: new Set(), relation: new Set(),
    column: new Set(), parameter: new Set(), state: new Map(),
  };
}

function addQueryDependency(sets, kind, address) {
  const target = kind === "entity_type_column" || kind === "relation_type_column" ? sets.column : sets[kind];
  if (!(target instanceof Set)) return false;
  target.add(address);
  return true;
}

function validQueryPredicate(raw, context, queryAddress, parameters, sets) {
  if (!isObject(raw)) return false;
  if (raw.kind === "all" || raw.kind === "any") return Array.isArray(raw.children) && raw.children.every((child) => validQueryPredicate(child, context, queryAddress, parameters, sets));
  if (raw.kind === "not") return validQueryPredicate(raw.child, context, queryAddress, parameters, sets);
  if (raw.kind === "rows") {
    if (!Array.isArray(raw.type_addresses)) return false;
    const expectedKind = context === "entity" ? "entity_type" : "relation_type";
    const rowContext = context === "entity" ? "entity_row" : "relation_row";
    for (const address of raw.type_addresses) {
      if (typeof address !== "string" || stableAddressSubject(address)?.kind !== expectedKind) return false;
      addQueryDependency(sets, expectedKind, address);
    }
    return validQueryPredicate(raw.predicate, rowContext, queryAddress, parameters, sets);
  }
  let operand;
  if (raw.kind === "field") {
    const expected = contextFieldOperand(String(raw.field), context);
    operand = recipeOperand(raw.operand_type);
    if (expected === undefined || operand === undefined || !equalRecipeOperands(expected, operand)) return false;
  } else if (raw.kind === "cell") {
    if (context !== "entity_row" && context !== "relation_row" || !Array.isArray(raw.column_addresses) || raw.column_addresses.length === 0) return false;
    const expectedKind = context === "entity_row" ? "entity_type_column" : "relation_type_column";
    for (const address of raw.column_addresses) {
      if (typeof address !== "string" || stableAddressSubject(address)?.kind !== expectedKind) return false;
      sets.column.add(address);
    }
    operand = recipeOperand(raw.operand_type);
  } else if (raw.kind === "state") {
    const field = String(raw.field_path);
    operand = recipeOperand(raw.operand_type);
    if (operand === undefined || !equalRecipeOperands(operand, {kind: "scalar", scalarType: stateFieldValueType(field), addressKind: ""})) return false;
    sets.state.set(`${context}\0${field}`, {subject_kind: context, field_path: field, value_type: stateFieldValueType(field)});
  } else return false;
  if (isObject(raw.value)) {
    if (raw.value.kind === "parameter") {
      const address = raw.value.parameter_address;
      const expectedType = operand.kind === "string_set" ? "string" : operand.scalarType;
      const subject = typeof address === "string" ? stableAddressSubject(address) : undefined;
      if (subject?.kind !== "query_parameter" || subject.owner !== queryAddress || parameters.get(address) !== expectedType) return false;
      sets.parameter.add(address);
    }
    const addresses = [
      ...(typeof raw.value.address_value === "string" ? [raw.value.address_value] : []),
      ...(Array.isArray(raw.value.address_values) ? raw.value.address_values : []),
    ];
    for (const address of addresses) {
      if (typeof address !== "string") return false;
      const subject = stableAddressSubject(address);
      if (subject === undefined || !addQueryDependency(sets, subject.kind, address)) return false;
    }
  }
  return true;
}

function dependencySetEquals(raw, expected) {
  return Array.isArray(raw) && raw.length === expected.size && raw.every((item) => typeof item === "string" && expected.has(item));
}

function validQueryRecipe(value) {
  if (typeof value.address !== "string" || !Array.isArray(value.parameters)) return false;
  const parameters = new Map();
  for (const parameter of value.parameters) {
    if (!isObject(parameter) || typeof parameter.address !== "string" || typeof parameter.value_type !== "string") return false;
    parameters.set(parameter.address, parameter.value_type);
  }
  const sets = queryDependencySets();
  if (!isObject(value.select)) return false;
  for (const [property, kind] of [["layer_addresses", "layer"], ["entity_type_addresses", "entity_type"], ["relation_type_addresses", "relation_type"], ["root_addresses", "entity"]]) {
    if (!Array.isArray(value.select[property])) continue;
    for (const address of value.select[property]) {
      if (typeof address !== "string" || !addQueryDependency(sets, kind, address)) return false;
    }
  }
  if (isObject(value.traverse) && Array.isArray(value.traverse.relation_type_addresses)) {
    for (const address of value.traverse.relation_type_addresses) {
      if (typeof address !== "string") return false;
      sets.relation_type.add(address);
    }
  }
  if (!validQueryPredicate(value.where, "entity", value.address, parameters, sets) ||
      !validQueryPredicate(value.relation_where, "relation", value.address, parameters, sets)) return false;
  const hasState = sets.state.size !== 0;
  if (hasState !== (value.state_input === "optional" || value.state_input === "required") || !isObject(value.dependencies)) return false;
  for (const property of ["layer", "entity_type", "relation_type", "entity", "relation", "column", "parameter"]) {
    if (!dependencySetEquals(value.dependencies[`${property}_addresses`], sets[property])) return false;
  }
  return Array.isArray(value.dependencies.state_reads) && value.dependencies.state_reads.length === sets.state.size &&
    value.dependencies.state_reads.every((read) => isObject(read) && sets.state.has(`${String(read.subject_kind)}\0${String(read.field_path)}`));
}

function validExportRecipe(value) {
  const extensions = new Map([
    ["json", ".json"], ["yaml", ".yaml"], ["svg", ".svg"], ["png", ".png"], ["pdf", ".pdf"],
    ["html", ".html"], ["csv", ".csv"], ["tsv", ".tsv"], ["xlsx", ".xlsx"], ["markdown", ".md"],
    ["pptx", ".pptx"], ["docx", ".docx"], ["mermaid", ".mmd"], ["bpmn", ".bpmn"], ["drawio", ".drawio"],
  ]);
  const extension = extensions.get(value.format);
  if (extension === undefined || value.extension !== extension || !isObject(value.options) || value.options.kind !== value.format ||
      !isObject(value.exporter_profile) || value.exporter_profile.format !== value.format || typeof value.filename !== "string" ||
      value.filename === "" || value.filename === "." || value.filename === ".." || /[\\/\0]/.test(value.filename) ||
      !value.filename.endsWith(extension) || value.filename.slice(0, -extension.length).length === 0) return false;
  const fixedMaximum = new Map([
    ["json", "lossless"], ["yaml", "lossless"], ["xlsx", "traceable_summary"], ["html", "traceable_summary"],
    ["svg", "visual_only"], ["png", "visual_only"], ["pdf", "visual_only"], ["pptx", "visual_only"],
    ["docx", "visual_only"], ["drawio", "visual_only"], ["bpmn", "lossy"],
  ]).get(value.format);
  if (fixedMaximum !== undefined && value.native_maximum_fidelity !== fixedMaximum) return false;
  if (value.format === "csv" || value.format === "tsv") {
    const maximum = value.options.bundle === true && value.options.header === true && value.options.source_manifest === true ? "traceable_summary" : "lossy";
    if (value.native_maximum_fidelity !== maximum) return false;
  }
  if ((value.format === "markdown" || value.format === "mermaid") && !["lossy", "traceable_summary"].includes(value.native_maximum_fidelity)) return false;
  const embedded = value.format === "xlsx" && value.options.view_data_json === true && value.options.hidden_ids === true;
  if (embedded ? value.fidelity_basis !== "embedded_viewdata" || value.effective_maximum_fidelity !== "lossless" :
    value.fidelity_basis !== "native" || value.effective_maximum_fidelity !== value.native_maximum_fidelity) return false;
  const ranks = new Map([["lossy", 0], ["visual_only", 1], ["traceable_summary", 2], ["lossless", 3]]);
  if (ranks.get(value.fidelity) > ranks.get(value.effective_maximum_fidelity)) return false;
  if ((["lossless", "traceable_summary"].includes(value.fidelity) || value.format === "json" || value.format === "yaml") && value.source_refs !== true) return false;
  const embeddedManifest = value.format === "json" || value.format === "yaml" || value.format === "xlsx" && value.options.view_data_json === true;
  const explicitManifest = ["csv", "tsv", "markdown", "mermaid", "bpmn", "drawio"].includes(value.format) && value.options.source_manifest === true;
  return !(explicitManifest || value.source_refs === true && !embeddedManifest) || value.requires_source_manifest === true;
}

function hasDirectStableAddressOwner(owner, child) {
  const parts = child.split(":");
  return parts.length >= 2 && parts.slice(0, -2).join(":") === owner;
}

function validViewProjection(value, projectionKind) {
  const distinct = (left, right) => typeof value[left] === "string" && typeof value[right] === "string" && value[left] !== value[right];
  if (projectionKind !== "composed") {
    const pair = new Map([
      ["diagram", ["source_endpoint", "target_endpoint"]], ["flow", ["source_endpoint", "target_endpoint"]],
      ["matrix", ["row_endpoint", "column_endpoint"]], ["tree", ["parent_endpoint", "child_endpoint"]],
    ]).get(projectionKind);
    return pair !== undefined && distinct(pair[0], pair[1]);
  }
  const present = (name) => hasOwn(value, name);
  if (value.mode === "nest") return distinct("parent_endpoint", "child_endpoint") && !present("overlay_endpoint") && !present("target_endpoint") && !present("badge_endpoint");
  if (value.mode === "overlay") return distinct("overlay_endpoint", "target_endpoint") && !present("parent_endpoint") && !present("child_endpoint") && !present("badge_endpoint");
  if (value.mode === "badge") return distinct("badge_endpoint", "target_endpoint") && !present("parent_endpoint") && !present("child_endpoint") && !present("overlay_endpoint");
  return (value.mode === "edge" || value.mode === "hide") && ["parent_endpoint", "child_endpoint", "overlay_endpoint", "target_endpoint", "badge_endpoint"].every((name) => !present(name));
}

function viewTableValueMatches(column, kind, scalarType = "", enumValues) {
  if (!isObject(column.value_type) || column.value_type.kind !== kind || kind === "scalar" && column.value_type.scalar_type !== scalarType) return false;
  if (enumValues !== undefined && (!Array.isArray(column.value_type.enum_values) || column.value_type.enum_values.length !== enumValues.length ||
      !column.value_type.enum_values.every((item, index) => item === enumValues[index]))) return false;
  if (kind === "scalar") {
    if (scalarType === "enum") {
      if (enumValues === undefined && (!Array.isArray(column.value_type.enum_values) || column.value_type.enum_values.length === 0)) return false;
    } else if (hasOwn(column.value_type, "enum_values")) return false;
    if (hasOwn(column.value_type, "format") && scalarType !== "string") return false;
  }
  return true;
}

function stateEnumValues(field) {
  if (["system.created_by.kind", "system.updated_by.kind", "provenance.verified_by.kind"].includes(field)) return ["user", "agent", "service_account", "anonymous"];
  return field === "provenance.source.kind" ? ["manual", "import", "api", "agent", "external_system"] : undefined;
}

function hasStateRead(values, expected) {
  return values.some((value) => isObject(value) && value.subject_kind === expected.subject_kind && value.field_path === expected.field_path && value.value_type === expected.value_type);
}

function viewDependencySets() {
  return {query: new Set(), parameter: new Set(), layer: new Set(), entity_type: new Set(), relation_type: new Set(), entity: new Set(), relation: new Set(), column: new Set()};
}

function addViewDependencyValues(raw, sets) {
  const values = typeof raw === "string" ? [raw] : Array.isArray(raw) ? raw : [];
  for (const item of values) {
    if (typeof item !== "string") continue;
    const subject = stableAddressSubject(item);
    if (subject === undefined) continue;
    const property = subject.kind === "query_parameter" ? "parameter" : subject.kind === "entity_type_column" || subject.kind === "relation_type_column" ? "column" : subject.kind;
    if (sets[property] instanceof Set) sets[property].add(item);
  }
}

function collectViewDependencies(raw, sets) {
  if (Array.isArray(raw)) {
    for (const item of raw) collectViewDependencies(item, sets);
    return;
  }
  if (!isObject(raw)) return;
  for (const [property, item] of Object.entries(raw)) {
    if (property === "arguments") {
      if (isObject(item)) for (const address of Object.keys(item)) addViewDependencyValues(address, sets);
      continue;
    }
    if (["query_address", "entity_address", "relation_address", "layer_address", "parameter_address", "branch_value_column_address",
      "layer_addresses", "entity_type_addresses", "relation_type_addresses", "entity_addresses", "relation_addresses",
      "column_addresses", "lane_column_addresses", "attribute_column_addresses"].includes(property)) {
      addViewDependencyValues(item, sets);
      continue;
    }
    collectViewDependencies(item, sets);
  }
}

function validLocallyDerivableViewDependencies(value) {
  const dependencies = value.dependencies;
  if (!isObject(dependencies) || !isObject(value.source) || !isObject(value.shape) || !isObject(value.relation_projection_overrides) || !Array.isArray(value.exports)) return false;
  const sets = viewDependencySets();
  collectViewDependencies(value.source, sets);
  collectViewDependencies(value.shape, sets);
  for (const [address, override] of Object.entries(value.relation_projection_overrides)) {
    addViewDependencyValues(address, sets);
    collectViewDependencies(override, sets);
  }
  if (!dependencySetEquals(dependencies.query_addresses, sets.query)) return false;
  const hasSourceQuery = typeof value.source.query_address === "string";
  for (const property of ["parameter", "layer", "entity_type", "relation_type", "entity", "relation", "column"]) {
    if (!Array.isArray(dependencies[`${property}_addresses`]) ||
        (hasSourceQuery ? [...sets[property]].some((address) => !dependencies[`${property}_addresses`].includes(address)) :
          !dependencySetEquals(dependencies[`${property}_addresses`], sets[property]))) return false;
  }
  return Array.isArray(dependencies.export_addresses) && dependencies.export_addresses.length === value.exports.length &&
    value.exports.every((item, index) => isObject(item) && item.address === dependencies.export_addresses[index]);
}

function validManifestClaim(value, stateRequirement, embedded) {
  if (!isObject(value.options)) return false;
  const explicit = (["csv", "tsv"].includes(value.options.kind) || ["markdown", "mermaid", "bpmn", "drawio"].includes(value.options.kind)) && value.options.source_manifest === true;
  return value.requires_source_manifest === (explicit || stateRequirement !== "none" || value.source_refs === true && !embedded);
}

function validExportInView(value, category, shape, stateRequirement, diff) {
  if (!isObject(value.options)) return false;
  const {format, options} = value;
  if (format === "json" || format === "yaml") {
    return value.native_maximum_fidelity === "lossless" && value.effective_maximum_fidelity === "lossless" && value.fidelity_basis === "native" &&
      validManifestClaim(value, stateRequirement, true) && !(diff && options.state_summary === true);
  }
  const matrix = {
    diagram: {xlsx: "traceable_summary", html: "traceable_summary", csv: "traceable_summary", tsv: "traceable_summary", svg: "visual_only", png: "visual_only", pdf: "visual_only", pptx: "visual_only", docx: "visual_only", drawio: "visual_only", mermaid: "lossy"},
    table: {xlsx: "traceable_summary", csv: "traceable_summary", tsv: "traceable_summary", html: "traceable_summary", pdf: "visual_only", pptx: "visual_only", docx: "visual_only", markdown: "lossy"},
    matrix: {xlsx: "traceable_summary", csv: "traceable_summary", tsv: "traceable_summary", html: "traceable_summary", svg: "visual_only", png: "visual_only", pdf: "visual_only", pptx: "visual_only", docx: "visual_only"},
    tree: {xlsx: "traceable_summary", csv: "traceable_summary", tsv: "traceable_summary", html: "traceable_summary", mermaid: "traceable_summary", svg: "visual_only", png: "visual_only", pdf: "visual_only", pptx: "visual_only", docx: "visual_only", drawio: "visual_only"},
    flow: {xlsx: "traceable_summary", csv: "traceable_summary", tsv: "traceable_summary", html: "traceable_summary", mermaid: "traceable_summary", bpmn: "lossy", svg: "visual_only", png: "visual_only", pdf: "visual_only", pptx: "visual_only", docx: "visual_only", drawio: "visual_only", markdown: "lossy"},
    context: {csv: "traceable_summary", tsv: "traceable_summary", xlsx: "traceable_summary", html: "traceable_summary", markdown: "traceable_summary", pdf: "visual_only", pptx: "visual_only", docx: "visual_only"},
    diff: {csv: "traceable_summary", tsv: "traceable_summary", xlsx: "traceable_summary", html: "traceable_summary", markdown: "traceable_summary", pdf: "visual_only", pptx: "visual_only", docx: "visual_only"},
  };
  let native = matrix[shape]?.[format];
  if (native === undefined) return false;
  if ((format === "csv" || format === "tsv") && !(options.bundle === true && options.header === true && options.source_manifest === true) ||
      (shape === "tree" || shape === "flow") && format === "mermaid" && options.source_manifest !== true) native = "lossy";
  if (value.native_maximum_fidelity !== native) return false;
  const fidelityEmbedded = format === "xlsx" && options.view_data_json === true && options.hidden_ids === true;
  if (fidelityEmbedded ? value.effective_maximum_fidelity !== "lossless" || value.fidelity_basis !== "embedded_viewdata" :
    value.effective_maximum_fidelity !== native || value.fidelity_basis !== "native") return false;
  if (format === "xlsx") {
    const profile = options.profile;
    const compatible = profile === "type_workbook" && shape === "table" ||
      ["diagram_workbook", "composed_diagram_workbook", "diagram_inventory_workbook"].includes(profile) && shape === "diagram" ||
      profile === "matrix_workbook" && shape === "matrix" || profile === "tree_workbook" && shape === "tree" ||
      profile === "flow_workbook" && shape === "flow" || profile === "diff_workbook" && shape === "diff" ||
      profile === "context_workbook" && shape === "context" || profile === "impact_workbook" && category === "impact" && ["diagram", "table", "matrix"].includes(shape);
    if (!compatible) return false;
  }
  return validManifestClaim(value, stateRequirement, format === "xlsx" && options.view_data_json === true);
}

function validViewRecipe(value) {
  const {address, shape, source, reserved_table_column_ids: reservedValues} = value;
  if (typeof address !== "string" || !isObject(shape) || !isObject(source) || !Array.isArray(reservedValues)) return false;
  const diffCount = Number(value.category === "diff") + Number(source.kind === "diff") + Number(shape.kind === "diff");
  if (diffCount !== 0 && diffCount !== 3) return false;
  const stateRanks = new Map([["none", 0], ["optional", 1], ["required", 2]]);
  if (stateRanks.get(value.state_requirement) < stateRanks.get(value.state_input) || diffCount === 3 && value.state_requirement !== "none") return false;
  const directReads = [];
  if (shape.kind === "table") {
    const table = shape.table;
    if (!isObject(table) || !Array.isArray(table.columns)) return false;
    const entityRows = table.row_source === "entity" || table.row_source === "entity_rows";
    if (!entityRows && (table.include_entity_id === true || table.include_type === true || table.include_layer === true || hasOwn(table, "entity_type_addresses"))) return false;
    const available = new Set();
    if (table.include_entity_id === true) available.add("entity_id");
    if (table.include_type === true) available.add("entity_type");
    if (table.include_layer === true) available.add("entity_layer");
    if (!Array.isArray(table.automatic_relation_columns) ||
        table.row_source !== "automatic_relations" && table.automatic_relation_columns.length !== 0 ||
        !table.automatic_relation_columns.every((item) => typeof item === "string")) return false;
    for (const item of table.automatic_relation_columns) available.add(item);
    const reserved = new Set(reservedValues);
    for (const column of table.columns) {
      if (!isObject(column) || typeof column.address !== "string" || typeof column.id !== "string" ||
          !hasDirectStableAddressOwner(address, column.address) || reserved.has(column.id) || available.has(column.id) || !isObject(column.source)) return false;
      available.add(column.id);
      const sourceKind = column.source.kind;
      if (sourceKind === "attribute") {
        if (table.row_source !== "entity_rows" && table.row_source !== "relation_rows" || !Array.isArray(column.source.column_addresses) || column.source.column_addresses.length === 0) return false;
        const expectedKind = table.row_source === "entity_rows" ? "entity_type_column" : "relation_type_column";
        if (!column.source.column_addresses.every((item) => typeof item === "string" && stableAddressSubject(item)?.kind === expectedKind)) return false;
      } else if (sourceKind === "relation_endpoint") {
        if (table.row_source !== "relation" && table.row_source !== "relation_rows") return false;
        const scalar = column.source.field === "id" || column.source.field === "display_name";
        if (!viewTableValueMatches(column, scalar ? "scalar" : "stable_address", scalar ? "string" : "")) return false;
      } else if (sourceKind === "derived_count") {
        if (!entityRows || !viewTableValueMatches(column, "scalar", "integer")) return false;
      } else if (sourceKind === "field") {
        const field = column.source.field;
        if (["id", "display_name", "description"].includes(field) ? !viewTableValueMatches(column, "scalar", "string") :
          field === "tags" ? !viewTableValueMatches(column, "string_set") : !viewTableValueMatches(column, "stable_address")) return false;
      } else if (sourceKind === "state") {
        const field = column.source.field_path;
        const valueType = stateFieldValueType(field);
        if (!viewTableValueMatches(column, "scalar", valueType, stateEnumValues(field))) return false;
        const subjects = table.row_source === "automatic_relations" ? ["relation", "relation_row"] :
          [table.row_source === "entity" ? "entity" : table.row_source === "entity_rows" ? "entity_row" : table.row_source === "relation" ? "relation" : "relation_row"];
        for (const subject_kind of subjects) directReads.push({subject_kind, field_path: field, value_type: valueType});
      } else return false;
      if ((column.aggregate === "count" || column.aggregate === "count_distinct") && !viewTableValueMatches(column, "scalar", "integer") ||
          column.aggregate === "join_unique" && !viewTableValueMatches(column, "scalar", "string") ||
          (column.aggregate === "min" || column.aggregate === "max") && (!isObject(column.value_type) || column.value_type.kind !== "scalar" || !["integer", "number", "date", "datetime", "enum"].includes(column.value_type.scalar_type))) return false;
    }
    if ((directReads.length !== 0) !== (value.state_input === "optional" || value.state_input === "required")) return false;
    if (!Array.isArray(table.sorts) || !table.sorts.every((sort) => isObject(sort) && typeof sort.column_id === "string" && available.has(sort.column_id))) return false;
    if (!isObject(value.dependencies) || !Array.isArray(value.dependencies.state_reads) || directReads.some((read) => !hasStateRead(value.dependencies.state_reads, read))) return false;
  } else if (value.state_input !== "none") return false;
  if (!isObject(value.dependencies) || !Array.isArray(value.dependencies.state_reads) ||
      typeof source.query_address !== "string" && value.dependencies.state_reads.length !== directReads.length) return false;
  return Array.isArray(value.exports) && validLocallyDerivableViewDependencies(value) && value.exports.every((item) => isObject(item) && item.view_address === address && validExportInView(item, value.category, shape.kind, value.state_requirement, diffCount === 3));
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

function protocolCanonicalJSON(value) {
  if (Array.isArray(value)) return `[${value.map(protocolCanonicalJSON).join(",")}]`;
  if (isObject(value)) return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${protocolCanonicalJSON(value[key])}`).join(",")}}`;
  return JSON.stringify(value);
}

function protocolGenerationKey(raw) {
  if (!isObject(raw) || !isObject(raw.document_handle) || typeof raw.document_handle.endpoint_instance_id !== "string" || typeof raw.document_handle.value !== "string" || typeof raw.value !== "string") return undefined;
  try { return [raw.document_handle.endpoint_instance_id, raw.document_handle.value, BigInt(raw.value)]; } catch { return undefined; }
}
function sameProtocolGeneration(left, right) { const a = protocolGenerationKey(left), b = protocolGenerationKey(right); return a !== undefined && b !== undefined && a[0] === b[0] && a[1] === b[1] && a[2] === b[2]; }
function nextProtocolGeneration(left, right) { const a = protocolGenerationKey(left), b = protocolGenerationKey(right); return a !== undefined && b !== undefined && a[0] === b[0] && a[1] === b[1] && b[2] === a[2] + 1n; }
function protocolErrorDiagnostic(raw) { return Array.isArray(raw) && raw.some((item) => isObject(item) && item.severity === "error"); }
function protocolBlobBytes(raw) { if (Array.isArray(raw)) return raw.reduce((sum, item) => sum + protocolBlobBytes(item), 0n); if (!isObject(raw)) return 0n; let total = typeof raw.blob_id === "string" && typeof raw.digest === "string" && typeof raw.size === "string" ? BigInt(raw.size) : 0n; for (const item of Object.values(raw)) total += protocolBlobBytes(item); return total; }
function protocolArrayItems(raw) { if (Array.isArray(raw)) return BigInt(raw.length) + raw.reduce((sum, item) => sum + protocolArrayItems(item), 0n); if (!isObject(raw)) return 0n; return Object.values(raw).reduce((sum, item) => sum + protocolArrayItems(item), 0n); }
function validCreateSubjectExport(fields) {
  const extensions = new Map([
    ["json", ".json"], ["yaml", ".yaml"], ["svg", ".svg"], ["png", ".png"], ["pdf", ".pdf"],
    ["html", ".html"], ["csv", ".csv"], ["tsv", ".tsv"], ["xlsx", ".xlsx"], ["markdown", ".md"],
    ["pptx", ".pptx"], ["docx", ".docx"], ["mermaid", ".mmd"], ["bpmn", ".bpmn"], ["drawio", ".drawio"],
  ]);
  const extension = extensions.get(fields.format);
  if (extension === undefined || typeof fields.filename !== "string" || fields.filename === "" || fields.filename === "." || fields.filename === ".." ||
      /[\\/\u0000]/.test(fields.filename) || !fields.filename.endsWith(extension) || fields.filename.slice(0, -extension.length).length === 0) return false;
  if (hasOwn(fields, "options") && (!isObject(fields.options) || fields.options.kind !== fields.format)) return false;
  return !hasOwn(fields, "exporter_profile") || isObject(fields.exporter_profile) && fields.exporter_profile.format === fields.format;
}
function validProtocolSemanticOperation(value) {
  if (value.operation === "create_subject") { if (typeof value.subject_kind !== "string" || !isObject(value.fields)) return false; if (value.subject_kind === "view") { const source=value.fields.source,shape=value.fields.shape,diff=value.fields.category === "diff"; if (!isObject(source)||!isObject(shape)||(source.kind === "diff")!==diff||(shape.kind === "diff")!==diff) return false; if ((shape.kind === "matrix" && (!isObject(shape.cell)||(shape.cell.display === "attribute_summary")!==hasOwn(shape.cell,"attribute_column_addresses"))) || (shape.kind === "flow" && (shape.lane_by === "attribute")!==hasOwn(shape,"lane_column_addresses"))) return false; } if (value.subject_kind === "view_table_column" && (!isObject(value.fields.source)||value.fields.source.kind === "query"||value.fields.source.kind === "diff")) return false; if (value.subject_kind === "entity_type_column"||value.subject_kind === "relation_type_column"||value.subject_kind === "query_parameter") { const format=value.fields.format; if (format!==undefined&&(typeof format!=="string"||!["cidr","email","hostname","ipv4","ipv6","uri"].includes(format))) return false; if (!validQueryParameter({...value.fields,reserved_enum_values:value.fields.reserved_enum_values??[]})) return false; } if (value.subject_kind === "view_export" && !validCreateSubjectExport(value.fields)) return false; }
  return true;
}
function validProtocolInvariant(profile, value) {
  if (!isObject(value)) return true;
  if (profile === "authoring_impact_entry") { if (value.capability !== "graph:write") return true; return typeof value.subject_address === "string" && isObject(value.graph_facts) && stableAddressSubject(value.subject_address)?.kind === value.subject_kind && Array.isArray(value.graph_facts.action_flags) && value.graph_facts.action_flags.length === 1 && value.graph_facts.action_flags[0] === value.action; }
  if (profile === "authoring_impact") { if (!Array.isArray(value.entries) || !Array.isArray(value.required_capabilities)) return false; const derived = new Set(value.entries.map((entry)=>entry?.capability)); return derived.size === value.required_capabilities.length && value.required_capabilities.every((item)=>derived.has(item)); }
  if (profile === "open_document_result") return isObject(value.document_generation) && sameProtocolGeneration(value.document_generation,{document_handle:value.document_handle,value:value.document_generation.value});
  if (profile === "document_bound_input") { if (hasOwn(value,"document_handle") && (!isObject(value.document_generation) || !sameProtocolGeneration(value.document_generation,{document_handle:value.document_handle,value:value.document_generation.value}))) return false; return !hasOwn(value,"cursor") || isObject(value.cursor) && sameProtocolGeneration(value.document_generation,value.cursor.document_generation); }
  if (profile === "paged_result") { if (!Array.isArray(value.items)||!isObject(value.page)||value.page.returned_items!==String(value.items.length)) return false; if (hasOwn(value.page,"next_cursor")&&(!isObject(value.page.next_cursor)||!sameProtocolGeneration(value.document_generation,value.page.next_cursor.document_generation))) return false; const copy=structuredClone(value); copy.page.returned_bytes="0"; try { return BigInt(utf8ByteLength(protocolCanonicalJSON(copy)))+protocolBlobBytes(value)===BigInt(value.page.returned_bytes); } catch { return false; } }
  if (profile === "query_result") { const result=value.result; if (!isObject(result)) return false; const fields=["seed_entity_addresses","reached_entity_addresses","traversed_entity_addresses","path_relation_addresses","induced_relation_addresses","primary_entity_addresses","selected_relation_addresses","support_entity_addresses","paths","cycle_refs","state_reads","diagnostics"]; let count=0n; for (const field of fields) { if (!Array.isArray(result[field])) return false; count+=BigInt(result[field].length); } if (value.returned_items!==String(count)) return false; const copy=structuredClone(value); copy.returned_bytes="0"; try { return BigInt(utf8ByteLength(protocolCanonicalJSON(copy)))+protocolBlobBytes(value)===BigInt(value.returned_bytes); } catch { return false; } }
  if (profile === "view_data_result") { if (!isObject(value.view_data) || value.returned_items !== String(protocolArrayItems(value.view_data))) return false; const copy=structuredClone(value); copy.returned_bytes="0"; try { return BigInt(utf8ByteLength(protocolCanonicalJSON(copy)))===BigInt(value.returned_bytes); } catch { return false; } }
  if (profile === "bounded_text_chunk") { if (!isObject(value.blob)) return false; try { const offset=BigInt(value.offset),total=BigInt(value.total_bytes),size=BigInt(value.blob.size); return offset<=total&&size<=total-offset&&value.blob.media_type==="text/plain; charset=utf-8"&&value.blob.lifetime==="request"&&(offset!==0n||size!==total||value.blob.digest===value.full_digest); } catch { return false; } }
  if (profile === "source_edit") { if (isObject(value.replacement_blob)&&(value.replacement_blob.digest!==value.after_digest||value.replacement_blob.media_type!=="text/plain; charset=utf-8"||value.replacement_blob.lifetime!=="request")) return false; return value.kind!=="move"||value.before_digest===value.after_digest&&protocolCanonicalJSON(value.before_module)!==protocolCanonicalJSON(value.after_module); }
  if (profile === "preview_result") { if (value.status!=="valid") return Array.isArray(value.conflicts)&&value.conflicts.length>0||protocolErrorDiagnostic(value.diagnostics); const impact=value.authoring_impact,semantic=value.semantic_diff,source=value.source_diff,hashes=value.resulting_hashes,base=protocolGenerationKey(value.base_generation); return isObject(impact)&&isObject(semantic)&&isObject(source)&&isObject(hashes)&&isObject(value.preview_id)&&base!==undefined&&!protocolErrorDiagnostic(value.diagnostics)&&value.authoring_impact_digest===impact.impact_digest&&protocolCanonicalJSON(value.required_authoring_capabilities)===protocolCanonicalJSON(impact.required_capabilities)&&impact.semantic_diff_hash===semantic.digest&&impact.source_diff_hash===source.digest&&impact.resulting_definition_hash===hashes.definition_hash&&value.preview_id.endpoint_instance_id===base[0]&&nextProtocolGeneration(value.base_generation,value.proposed_generation); }
  if (profile === "apply_input") { const base=protocolGenerationKey(value.base_generation); return isObject(value.preview_id)&&base!==undefined&&value.preview_id.endpoint_instance_id===base[0]; }
  if (profile === "apply_result") return isObject(value.authoring_impact)&&isObject(value.source_diff)&&isObject(value.resulting_hashes)&&value.authoring_impact.source_diff_hash===value.source_diff.digest&&value.authoring_impact.resulting_definition_hash===value.resulting_hashes.definition_hash;
  if (profile === "semantic_operation") return validProtocolSemanticOperation(value);
  return false;
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

function semanticSubjectKindRank(kind) {
  return ["project", "pack", "entity_type", "relation_type", "layer", "entity", "relation", "query", "view", "reference", "entity_type_column", "entity_type_constraint", "relation_type_column", "relation_type_constraint", "entity_row", "relation_row", "query_parameter", "view_table_column", "view_export"].indexOf(kind);
}

function compareModuleOrder(left, right) {
  const origin = (raw) => {
    if (!isObject(raw) || typeof raw.kind !== "string") return undefined;
    if (raw.kind === "project") return [0, ""];
    return raw.kind === "pack" && typeof raw.pack_address === "string" ? [1, raw.pack_address] : undefined;
  };
  const leftOrigin = origin(left.origin);
  const rightOrigin = origin(right.origin);
  if (leftOrigin === undefined || rightOrigin === undefined || typeof left.module_path !== "string" || typeof right.module_path !== "string") return undefined;
  if (leftOrigin[0] !== rightOrigin[0]) return leftOrigin[0] - rightOrigin[0];
  const pack = compareUnicodeScalars(leftOrigin[1], rightOrigin[1]);
  return pack !== 0 ? pack : compareUnicodeScalars(left.module_path, right.module_path);
}

function compareRangePosition(left, right) {
  if (typeof left.start_byte !== "string" || typeof right.start_byte !== "string" || typeof left.end_byte !== "string" || typeof right.end_byte !== "string") return undefined;
  const start = compareCanonicalUnsignedDecimals(left.start_byte, right.start_byte);
  return start === undefined || start !== 0 ? start : compareCanonicalUnsignedDecimals(left.end_byte, right.end_byte);
}

function compareCanonicalCollection(profile, left, right) {
  if (!isObject(left) || !isObject(right)) return undefined;
  const stable = (property) => typeof left[property] === "string" && typeof right[property] === "string" ? compareStableAddresses(left[property], right[property]) : undefined;
  const text = (property) => typeof left[property] === "string" && typeof right[property] === "string" ? compareUnicodeScalars(left[property], right[property]) : undefined;
  const kind = (property) => {
    if (typeof left[property] !== "string" || typeof right[property] !== "string") return undefined;
    const leftRank = semanticSubjectKindRank(left[property]);
    const rightRank = semanticSubjectKindRank(right[property]);
    return leftRank < 0 || rightRank < 0 ? undefined : leftRank - rightRank;
  };
  const range = () => isObject(left.range) && isObject(right.range) ? compareRangePosition(left.range, right.range) : undefined;
  const identity = () => {
    const a = left.before_address ?? left.after_address;
    const b = right.before_address ?? right.after_address;
    return typeof a === "string" && typeof b === "string" ? compareStableAddresses(a, b) : undefined;
  };
  const conflictAddress = (property) => {
    const a = left[property] ?? "";
    const b = right[property] ?? "";
    return typeof a === "string" && typeof b === "string" ? (a === "" || b === "" ? compareUnicodeScalars(a, b) : compareStableAddresses(a, b)) : undefined;
  };
  const conflictKind = () => {
    const order = ["stale_revision", "subject_changed", "subtree_changed", "child_set_changed", "same_field_changed", "delete_vs_update", "duplicate_identity", "reference_broken", "schema_row_incompatible", "placement_changed", "project_identity_changed"];
    if (typeof left.kind !== "string" || typeof right.kind !== "string") return undefined;
    const a = order.indexOf(left.kind);
    const b = order.indexOf(right.kind);
    return a < 0 || b < 0 ? undefined : a - b;
  };
  const conflictChildKind = () => {
    const a = left.child_kind ?? "";
    const b = right.child_kind ?? "";
    if (typeof a !== "string" || typeof b !== "string") return undefined;
    if (a === "" || b === "") return compareUnicodeScalars(a, b);
    const leftRank = semanticSubjectKindRank(a);
    const rightRank = semanticSubjectKindRank(b);
    return leftRank < 0 || rightRank < 0 ? undefined : leftRank - rightRank;
  };
  const path = () => {
    const a = left.path ?? [];
    const b = right.path ?? [];
    if (!Array.isArray(a) || !Array.isArray(b)) return undefined;
    for (let index = 0; index < Math.min(a.length, b.length); index++) {
      if (typeof a[index] !== "string" || typeof b[index] !== "string") return undefined;
      const value = compareUnicodeScalars(a[index], b[index]);
      if (value !== 0) return value;
    }
    return a.length - b.length;
  };
  const optionalSourceRange = () => {
    const a = left.source_range;
    const b = right.source_range;
    if (!isObject(a) || !isObject(b)) return isObject(a) ? 1 : isObject(b) ? -1 : 0;
    return compareRangePosition(a, b);
  };
  const stringArray = (property) => {
    const a = left[property] ?? [];
    const b = right[property] ?? [];
    if (!Array.isArray(a) || !Array.isArray(b)) return undefined;
    for (let index = 0; index < Math.min(a.length, b.length); index++) {
      if (typeof a[index] !== "string" || typeof b[index] !== "string") return undefined;
      const value = compareUnicodeScalars(a[index], b[index]);
      if (value !== 0) return value;
    }
    return a.length - b.length;
  };
  const chain = (...comparisons) => {
    for (const comparison of comparisons) {
      const value = comparison();
      if (value === undefined || value !== 0) return value;
    }
    return 0;
  };
  if (profile === "authored_field_path") return stringArray("tokens");
  if (profile === "authoring_impact") {
    const address = () => {
      const a = left.subject_address ?? left.owner_address ?? "";
      const b = right.subject_address ?? right.owner_address ?? "";
      return typeof a === "string" && typeof b === "string" ? (a === "" || b === "" ? compareUnicodeScalars(a, b) : compareStableAddresses(a, b)) : undefined;
    };
    return chain(address, () => text("capability"), () => text("action"));
  }
  if (profile === "bounded_text_chunk") {
    const address = () => {
      const a = left.address ?? left.owner_address;
      const b = right.address ?? right.owner_address;
      return typeof a === "string" && typeof b === "string" ? compareStableAddresses(a, b) : undefined;
    };
    const offset = () => {
      const a = left.source_chunk ?? left.text_chunk;
      const b = right.source_chunk ?? right.text_chunk;
      return isObject(a) && isObject(b) && typeof a.offset === "string" && typeof b.offset === "string"
        ? compareCanonicalUnsignedDecimals(a.offset, b.offset) : undefined;
    };
    return chain(address, offset);
  }
  if (profile === "child_set") return chain(() => stable("owner_address"), () => kind("child_kind"));
  if (profile === "conflict") return chain(conflictKind, () => conflictAddress("target_address"), () => conflictAddress("owner_address"), conflictChildKind, path);
  if (profile === "reference_id") return text("id");
  if (profile === "subject_kind") return kind("kind");
  if (profile === "module_scope") return isObject(left.module) && isObject(right.module) ? compareModuleOrder(left.module, right.module) : undefined;
  if (profile === "neighbor") return typeof left.traversal_index === "string" && typeof right.traversal_index === "string"
    ? compareCanonicalUnsignedDecimals(left.traversal_index, right.traversal_index) : undefined;
  if (profile === "source_file") return compareModuleOrder(left, right);
  if (profile === "source_patch") {
    if (!isObject(left.source_range) || !isObject(right.source_range)) return undefined;
    const module = compareModuleOrder(left.source_range, right.source_range);
    return module === 0 ? compareRangePosition(left.source_range, right.source_range) : module;
  }
  if (profile === "semantic_diff") return chain(identity, () => text("kind"));
  if (profile === "semantic_map_entry") return text("key");
  if (profile === "source_diff") {
    const module = (value) => isObject(value.source_range) ? value.source_range : isObject(value.before_module) ? value.before_module : isObject(value.after_module) ? value.after_module : undefined;
    const primary = () => { const a = module(left), b = module(right); return a === undefined || b === undefined ? undefined : compareModuleOrder(a, b); };
    const after = () => { const a = left.after_module, b = right.after_module; if (!isObject(a) || !isObject(b)) return isObject(a) ? 1 : isObject(b) ? -1 : 0; return compareModuleOrder(a, b); };
    return chain(primary, () => text("kind"), optionalSourceRange, after);
  }
  if (profile === "source_range") {
    const module = compareModuleOrder(left, right);
    return module === 0 ? compareRangePosition(left, right) : module;
  }
  if (profile === "subgraph") return typeof left.traversal_index === "string" && typeof right.traversal_index === "string"
    ? compareCanonicalUnsignedDecimals(left.traversal_index, right.traversal_index) : undefined;
  if (profile === "source_asset") return chain(() => stable("subject_address"), () => text("locator"));
  if (profile === "semantic_reference") return chain(() => stable("source_address"), range, () => stable("target_address"), () => kind("target_kind"), () => text("via"));
  if (profile === "source_binding") {
    const owner = () => {
      const leftOwner = left.target_owner_address ?? "";
      const rightOwner = right.target_owner_address ?? "";
      if (typeof leftOwner !== "string" || typeof rightOwner !== "string") return undefined;
      return leftOwner === "" || rightOwner === "" ? compareUnicodeScalars(leftOwner, rightOwner) : compareStableAddresses(leftOwner, rightOwner);
    };
    return chain(() => stable("source_address"), range, () => stable("target_address"), () => kind("target_kind"), owner, () => text("via"));
  }
  if (profile === "export_binding") {
    const module = () => isObject(left.module) && isObject(right.module) ? compareModuleOrder(left.module, right.module) : undefined;
    const reexport = () => typeof left.re_export === "boolean" && typeof right.re_export === "boolean" ? Number(left.re_export) - Number(right.re_export) : undefined;
    return chain(module, range, () => text("public_name"), () => stable("target_address"), reexport);
  }
  return undefined;
}

function validChildSet(value) {
  if (typeof value.owner_address !== "string" || typeof value.child_kind !== "string") return false;
  const ownerKind = stableAddressSubject(value.owner_address)?.kind;
  const allowed = {
    project: ["entity_type", "relation_type", "layer", "entity", "relation", "query", "view", "reference"],
    pack: ["entity_type", "relation_type", "query", "view", "reference"],
    entity_type: ["entity_type_column", "entity_type_constraint"], relation_type: ["relation_type_column", "relation_type_constraint"],
    entity: ["entity_row"], relation: ["relation_row"], query: ["query_parameter"], view: ["view_table_column", "view_export"],
  };
  return ownerKind !== undefined && (allowed[ownerKind]?.includes(value.child_kind) ?? false);
}

function addLayerDrawVocabulary(ajv, meta, enumAuthorities = new Map()) {
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
    const reference = parentSchema?.items?.$ref;
    const definition = typeof reference === "string" ? reference.split("#/$defs/")[1] : undefined;
    const values = parentSchema?.items?.enum ?? enumAuthorities.get(definition);
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
  register({keyword: "x-layerdraw-canonical-collection-order", schemaType: "string", type: "array", errors: false, validate(profile, data) {
    return data.every((item, index) => {
      if (index === 0) return true;
      const previous = data[index - 1];
      if ((compareCanonicalCollection(profile, previous, item) ?? 0) >= 0) return false;
      if (profile !== "source_patch") return true;
      if (!isObject(previous?.source_range) || !isObject(item?.source_range)) return false;
      if (compareModuleOrder(previous.source_range, item.source_range) !== 0) return true;
      const overlap = compareCanonicalUnsignedDecimals(previous.source_range.end_byte, item.source_range.start_byte);
      return overlap !== undefined && overlap <= 0;
    });
  }});
  register({keyword: "x-layerdraw-child-set", schemaType: "boolean", type: "object", errors: false, validate(enabled, data) {
    return !enabled || !isObject(data) || validChildSet(data);
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
      const items = Object.prototype.hasOwnProperty.call(data, rule.array) ? data[rule.array] : [];
      const strings = Object.prototype.hasOwnProperty.call(data, rule.strings) ? data[rule.strings] : [];
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
      (variant.error_diagnostic ?? []).every((key) => Array.isArray(data[key]) && data[key].some((item) => item !== null && typeof item === "object" && !Array.isArray(item) && item.severity === "error")) &&
      ((variant.any_non_empty ?? []).length === 0 || variant.any_non_empty.some((key) => Array.isArray(data[key]) && data[key].length > 0)) &&
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
    if (data.outcome === "failed" || data.outcome === "cancelled") {
      if (own("payload") || !isObject(data.failure)) return false;
      const category = data.failure.workbench_category;
      return typeof category !== "string" || (data.outcome === "cancelled" ? category === "cancelled" : category !== "cancelled");
    }
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
  register({keyword: "x-layerdraw-protocol-invariant", schemaType: "string", type: "object", errors: false, validate: validProtocolInvariant});
  register({keyword: "x-layerdraw-query-parameter", schemaType: "boolean", type: "object", errors: false, validate(enabled, data) {
    return !enabled || !isObject(data) || validQueryParameter(data);
  }});
  register({keyword: "x-layerdraw-query-recipe", schemaType: "boolean", type: "object", errors: false, validate(enabled, data) {
    return !enabled || !isObject(data) || validQueryRecipe(data);
  }});
  register({keyword: "x-layerdraw-recipe-predicate", schemaType: "string", type: "object", errors: false, validate(predicateKind, data) {
    return !isObject(data) || validRecipePredicate(data, predicateKind);
  }});
  register({keyword: "x-layerdraw-recipe-scalar", schemaType: "boolean", type: "object", errors: false, validate(enabled, data) {
    return !enabled || !isObject(data) || validRecipeScalar(data);
  }});
  register({keyword: "x-layerdraw-stable-address-roles", schemaType: "array", type: "object", errors: false, validate(rules, data) {
    return !isObject(data) || validStableAddressRoles(data, rules);
  }});
  register({keyword: "x-layerdraw-state-read", schemaType: "boolean", type: "object", errors: false, validate(enabled, data) {
    return !enabled || !isObject(data) || validStateRead(data);
  }});
  register({keyword: "x-layerdraw-state-read-order", schemaType: "boolean", type: "array", errors: false, validate(enabled, data) {
    return !enabled || validStateReadOrder(data);
  }});
  register({keyword: "x-layerdraw-view-projection", schemaType: "string", type: "object", errors: false, validate(projectionKind, data) {
    return !isObject(data) || validViewProjection(data, projectionKind);
  }});
  register({keyword: "x-layerdraw-limit-capability", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object") return true;
    try { return BigInt(data.default_value) <= BigInt(data.hard_maximum) && BigInt(data.effective_maximum) <= BigInt(data.hard_maximum); } catch { return false; }
  }});
  register({keyword: "x-layerdraw-unique-array-keys", schemaType: "array", errors: false, validate(rules, data) {
    if (data === null || typeof data !== "object") return true;
    return rules.every((rule) => {
      const seen = new Set();
      const items = Object.prototype.hasOwnProperty.call(data, rule.array) ? data[rule.array] : [];
      return Array.isArray(items) && items.every((item) => item !== null && typeof item === "object" &&
        typeof item[rule.property] === "string" && !seen.has(item[rule.property]) && Boolean(seen.add(item[rule.property])));
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
  const enumAuthorities = new Map();
  for (const document of documents) for (const [name, definition] of Object.entries(document.$defs)) {
    if (!Array.isArray(definition?.enum)) continue;
    const previous = enumAuthorities.get(name);
    assert.ok(previous === undefined || JSON.stringify(previous) === JSON.stringify(definition.enum), `conflicting enum authority ${name}`);
    enumAuthorities.set(name, definition.enum);
  }
  const rootRequirements = addLayerDrawVocabulary(ajv, meta, enumAuthorities);
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

test("Ajv enforces closed Workbench handles, recursive values, ordering, and outcome invariants", async () => {
  const compile = await authority();
  const engine = "https://schemas.layerdraw.dev/engine-protocol/v1";
  const validateRequest = compile(engine, "PreviewOperationsRequestEnvelope");
  const validateResponse = compile(engine, "PreviewOperationsResponseEnvelope");
  assert.equal(validateRequest(await readJSON("fixtures/engine/workbench-preview-operations-request.json")), true);
  for (const name of [
    "workbench-invalid-handle-request.json",
    "workbench-invalid-null-request.json",
    "workbench-invalid-order-request.json",
  ]) assert.equal(validateRequest(await readJSON(`fixtures/engine/${name}`)), false, name);
  assert.equal(validateResponse(await readJSON("fixtures/engine/workbench-invalid-outcome-response.json")), false);
  assert.equal(validateResponse(await readJSON("fixtures/engine/workbench-preview-conflict-only-response.json")), true);
  assert.equal(validateResponse(await readJSON("fixtures/engine/workbench-invalid-empty-preview-response.json")), false);
  const validatePatch = compile(engine, "PreviewSourcePatchRequestEnvelope");
  assert.equal(validatePatch(await readJSON("fixtures/engine/workbench-preview-source-patch-request.json")), true);
  assert.equal(validatePatch(await readJSON("fixtures/engine/workbench-invalid-overlapping-source-patch-request.json")), false);
  assert.equal(compile(engine, "OpenDocumentResponseEnvelope")(await readJSON("fixtures/engine/workbench-open-document-response.json")), true);
  assert.equal(compile(engine, "OpenDocumentResponseEnvelope")(await readJSON("fixtures/engine/workbench-open-pack-document-response.json")), true);
  assert.equal(compile(engine, "OpenDocumentResponseEnvelope")(await readJSON("fixtures/engine/workbench-open-invalid-root-response.json")), true);
  assert.equal(compile(engine, "MaterializeViewRequestEnvelope")(await readJSON("fixtures/engine/materialize-view-request.json")), true);
  assert.equal(compile(engine, "MaterializeViewResponseEnvelope")(await readJSON("fixtures/engine/materialize-view-success.json")), true);
  assert.equal(compile(engine, "MaterializeViewResponseEnvelope")(await readJSON("fixtures/engine/materialize-view-rejected.json")), true);
  assert.equal(compile(engine, "OpenDocumentResponseEnvelope")(await readJSON("fixtures/engine/workbench-invalid-unavailable-warning-only-response.json")), false);
  assert.equal(compile(engine, "OpenDocumentResponseEnvelope")(await readJSON("fixtures/engine/workbench-invalid-open-document-capabilities-response.json")), false);
  assert.equal(compile(engine, "ReplaceSourceTreeResult")(await readJSON("fixtures/engine/workbench-replace-pack-result.json")), true);
  assert.equal(compile(engine, "ResultingHashes")(await readJSON("fixtures/engine/workbench-invalid-pack-resulting-hashes.json")), false);
  assert.equal(compile(engine, "InspectSubgraphResult")(await readJSON("fixtures/engine/workbench-inspect-subgraph-result.json")), true);
  assert.equal(compile(engine, "InspectSubgraphResult")(await readJSON("fixtures/engine/workbench-invalid-subgraph-relation-result.json")), false);
  assert.equal(compile(engine, "InspectSubgraphResult")(await readJSON("fixtures/engine/workbench-invalid-subgraph-item-facts-result.json")), false);
  assert.equal(compile(engine, "InspectSubgraphInput")(await readJSON("fixtures/engine/workbench-invalid-subgraph-root-input.json")), false);
  assert.equal(compile(engine, "FindUsagesResult")(await readJSON("fixtures/engine/workbench-find-usages-result.json")), true);
  assert.equal(compile(engine, "FindUsagesResult")(await readJSON("fixtures/engine/workbench-invalid-find-usages-target-kind-result.json")), false);
  assert.equal(compile(engine, "CloseDocumentResponseEnvelope")(await readJSON("fixtures/engine/workbench-close-failed-response.json")), true);
  assert.equal(compile(engine, "CloseDocumentResponseEnvelope")(await readJSON("fixtures/engine/workbench-invalid-close-failed-response.json")), false);
  assert.equal(compile(engine, "ReadDeclarationsResult")(await readJSON("fixtures/engine/workbench-large-declaration-result.json")), true);
  assert.equal(compile(engine, "ReadDeclarationsResult")(await readJSON("fixtures/engine/workbench-invalid-declaration-chunk-order-result.json")), false);
  assert.equal(compile(engine, "FindUsagesResult")(await readJSON("fixtures/engine/workbench-invalid-find-usages-order-result.json")), false);
  assert.equal(compile(engine, "EngineEditPreconditions")(await readJSON("fixtures/engine/workbench-optional-preconditions.json")), true);
  assert.equal(compile(engine, "EngineEditPreconditions")(await readJSON("fixtures/engine/workbench-invalid-source-digest-order.json")), false);
  assert.equal(compile(engine, "SemanticOperation")(await readJSON("fixtures/engine/workbench-upsert-row-default-absent.json")), true);
  assert.equal(compile(engine, "SemanticOperation")(await readJSON("fixtures/engine/workbench-create-subject-single-kind.json")), true);
  assert.equal(compile(engine, "SemanticOperation")(await readJSON("fixtures/engine/workbench-invalid-semantic-map-order.json")), false);
  assert.equal(compile(engine, "SemanticOperation")(await readJSON("fixtures/engine/workbench-invalid-authored-path-depth.json")), false);
  assert.equal(compile(engine, "SemanticOperation")(await readJSON("fixtures/engine/workbench-invalid-upsert-row-overlap.json")), false);
  assert.equal(compile(engine, "SemanticOperation")(await readJSON("fixtures/engine/workbench-invalid-non-upsert-explicit-absence.json")), false);
  assert.equal(compile(engine, "ClassifyAuthoringImpactInput")(await readJSON("fixtures/engine/workbench-invalid-classify-raw-diff.json")), false);
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  assert.equal(compile(semantic, "SemanticDiff")(await readJSON("fixtures/engine/workbench-invalid-semantic-diff-order.json")), false);
  assert.equal(compile(engine, "SourceDiff")(await readJSON("fixtures/engine/workbench-invalid-source-diff-order.json")), false);
  assert.equal(compile(engine, "SourceDiff")(await readJSON("fixtures/engine/workbench-source-diff-all-kinds.json")), true);
  assert.equal(compile(engine, "SourceDiff")(await readJSON("fixtures/engine/workbench-invalid-replace-source-edit-identity.json")), false);
  assert.equal(compile(semantic, "AuthoringImpact")(await readJSON("fixtures/engine/workbench-invalid-authoring-impact-order.json")), false);
  assert.equal(compile(engine, "FindSymbolsInput")(await readJSON("fixtures/engine/workbench-find-symbols-input.json")), true);
  assert.equal(compile(engine, "FindSymbolsInput")(await readJSON("fixtures/engine/workbench-find-symbols-unrestricted-input.json")), true);
  assert.equal(compile(engine, "FindSymbolsInput")(await readJSON("fixtures/engine/workbench-invalid-find-symbols-mode-input.json")), false);
  assert.equal(compile(engine, "FindSymbolsInput")(await readJSON("fixtures/engine/workbench-invalid-find-symbols-empty-filter-input.json")), false);
  assert.equal(compile(engine, "PreviewOperationsResponseEnvelope")(await readJSON("fixtures/engine/workbench-stale-generation-response.json")), true);
  assert.equal(compile(engine, "PreviewOperationsResponseEnvelope")(await readJSON("fixtures/engine/workbench-invalid-stale-generation-response.json")), false);
  const validateAllCreatableKinds = compile(engine, "SemanticOperationBatch");
  assert.equal(validateAllCreatableKinds(await readJSON("fixtures/engine/workbench-create-subject-all-kinds.json")), true, JSON.stringify(validateAllCreatableKinds.errors));
  const validateSemanticOperation = compile(engine, "SemanticOperation");
  const authoredContracts = await readJSON("fixtures/engine/workbench-create-subject-contracts.json");
  assert.equal(validateAllCreatableKinds(authoredContracts), true, JSON.stringify(validateAllCreatableKinds.errors));
  const contractByID = new Map(authoredContracts.operations.map((operation) => [operation.id, operation]));
  const rejectContract = (name, id, mutate) => {
    const operation = structuredClone(contractByID.get(id));
    mutate(operation.fields);
    assert.equal(validateSemanticOperation(operation), false, name);
  };
  for (const format of ["bpmn", "csv", "docx", "drawio", "html", "json", "markdown", "mermaid", "pdf", "png", "pptx", "svg", "tsv", "xlsx", "yaml"]) {
    rejectContract(`${format} exact extension`, format, (fields) => { fields.filename = "map.invalid"; });
  }
  rejectContract("basename only", "json", (fields) => { fields.filename = "../map.json"; });
  rejectContract("options format authority", "json", (fields) => { fields.options = structuredClone(contractByID.get("yaml").fields.options); });
  rejectContract("exporter profile format authority", "json", (fields) => { fields.exporter_profile.format = "yaml"; });
  rejectContract("Column format domain", "email_value", (fields) => { fields.format = "json"; });
  rejectContract("View Export format domain", "json", (fields) => { fields.format = "email"; fields.filename = "map.email"; delete fields.options; delete fields.exporter_profile; });
  rejectContract("active and reserved enum disjointness", "choice", (fields) => { fields.reserved_enum_values = ["a"]; });
  rejectContract("nonempty enum member", "choice", (fields) => { fields.enum_values = [""]; delete fields.default; });
  rejectContract("safe integer bound", "count", (fields) => { fields.max = "9007199254740992"; });
  rejectContract("integer default within bounds", "count", (fields) => { fields.default.integer_value = "-11"; });
  rejectContract("minimum string length", "code", (fields) => { fields.default.string_value = "a"; });
  rejectContract("maximum string length", "code", (fields) => { fields.default.string_value = "abcde"; });
  rejectContract("canonical string format", "email_value", (fields) => { fields.default.string_value = "not-email"; });
  rejectContract("active enum default", "choice", (fields) => { fields.default.string_value = "missing"; });
  assert.equal(compile(engine, "SemanticOperation")(await readJSON("fixtures/engine/workbench-create-relation-fields.json")), true);
  for (const name of ["workbench-invalid-create-subject-foreign-field.json", "workbench-invalid-create-subject-missing-field.json", "workbench-invalid-create-subject-parent-kind.json", "workbench-invalid-create-subject-nested.json", "workbench-invalid-create-subject-cardinality.json", "workbench-invalid-create-subject-enum-options.json", "workbench-invalid-create-subject-view-shape.json", "workbench-invalid-create-subject-flow-lanes.json"]) assert.equal(compile(engine, "SemanticOperation")(await readJSON(`fixtures/engine/${name}`)), false, name);
  const validateAuthoringImpact = compile("https://schemas.layerdraw.dev/semantic/v1", "AuthoringImpact");
  assert.equal(validateAuthoringImpact(await readJSON("fixtures/engine/workbench-authoring-impact-graph.json")), true, JSON.stringify(validateAuthoringImpact.errors));
  for (const name of ["workbench-invalid-authoring-impact-missing-facts.json", "workbench-invalid-authoring-impact-address-kind.json", "workbench-invalid-authoring-impact-action.json", "workbench-invalid-authoring-impact-capabilities.json"]) assert.equal(compile("https://schemas.layerdraw.dev/semantic/v1", "AuthoringImpact")(await readJSON(`fixtures/engine/${name}`)), false, name);
  for (const name of ["workbench-invalid-source-create-digest.json", "workbench-invalid-source-blob-media.json", "workbench-invalid-source-blob-lifetime.json", "workbench-invalid-source-move-module.json", "workbench-invalid-source-move-digest.json"]) assert.equal(compile(engine, "SourceDiff")(await readJSON(`fixtures/engine/${name}`)), false, name);
  const validateCompletePreview = compile(engine, "WorkbenchPreviewResult");
  assert.equal(validateCompletePreview(await readJSON("fixtures/engine/workbench-preview-valid-warning.json")), true, JSON.stringify(validateCompletePreview.errors));
  for (const name of ["workbench-invalid-preview-impact-digest.json", "workbench-invalid-preview-capabilities.json", "workbench-invalid-preview-semantic-hash.json", "workbench-invalid-preview-source-hash.json", "workbench-invalid-preview-resulting-hash.json", "workbench-invalid-preview-endpoint.json", "workbench-invalid-preview-generation.json", "workbench-invalid-preview-warning-only.json"]) assert.equal(compile(engine, "WorkbenchPreviewResult")(await readJSON(`fixtures/engine/${name}`)), false, name);
  assert.equal(compile(engine, "ApplyToHandleResult")(await readJSON("fixtures/engine/workbench-apply-result.json")), true);
  for (const name of ["workbench-invalid-apply-source-hash.json", "workbench-invalid-apply-resulting-hash.json"]) assert.equal(compile(engine, "ApplyToHandleResult")(await readJSON(`fixtures/engine/${name}`)), false, name);
  assert.equal(compile(engine, "FindSymbolsInput")(await readJSON("fixtures/engine/workbench-invalid-input-cursor-generation.json")), false);
  assert.equal(compile(engine, "CloseDocumentInput")(await readJSON("fixtures/engine/workbench-invalid-close-generation.json")), false);
  assert.equal(compile(engine, "ApplyToHandleInput")(await readJSON("fixtures/engine/workbench-invalid-apply-endpoint.json")), false);
  assert.equal(compile(engine, "OpenDocumentResult")(await readJSON("fixtures/engine/workbench-invalid-open-handle-generation.json")), false);
  assert.equal(compile(engine, "ListModulesResult")(await readJSON("fixtures/engine/workbench-page-empty.json")), true);
  for (const name of ["workbench-invalid-page-count.json", "workbench-invalid-page-bytes.json", "workbench-invalid-page-cursor-generation.json"]) assert.equal(compile(engine, "ListModulesResult")(await readJSON(`fixtures/engine/${name}`)), false, name);
  assert.equal(compile(engine, "ReadModulesResult")(await readJSON("fixtures/engine/workbench-read-modules-result.json")), true);
  assert.equal(compile(engine, "ReadModulesResult")(await readJSON("fixtures/engine/workbench-invalid-read-modules-bytes.json")), false);
  assert.equal(compile(engine, "ReadDeclarationsResult")(await readJSON("fixtures/engine/workbench-invalid-page-byte-overflow.json")), false);
  for (const name of ["workbench-invalid-chunk-overflow.json", "workbench-invalid-chunk-media.json"]) assert.equal(compile(engine, "BoundedTextChunk")(await readJSON(`fixtures/engine/${name}`)), false, name);
  assert.equal(compile(engine, "ClassifyAuthoringImpactInput")(await readJSON("fixtures/engine/workbench-classify-authoring-impact-input.json")), true);
  for (const name of ["workbench-rejected-handle-response.json", "workbench-rejected-cursor-response.json", "workbench-rejected-generation-response.json", "workbench-rejected-input-response.json", "workbench-rejected-not_found-response.json", "workbench-rejected-preview-response.json", "workbench-rejected-unsupported-response.json", "workbench-rejected-precondition-response.json", "workbench-failed-execution-response.json", "workbench-cancelled-response.json"]) assert.equal(compile(engine, "CloseDocumentResponseEnvelope")(await readJSON(`fixtures/engine/${name}`)), true, name);
  const engineSchema = await readJSON("engine-protocol/v1.schema.json");
  assert.match(engineSchema.$defs.WorkbenchLimits.description, /outer Engine response envelope/);
  assert.match(engineSchema.$defs.LogicalResponseByteCount.description, /BlobRef attachment/);
  assert.match(engineSchema.$defs.FindSymbolsInput.description, /without normalization/);
});

test("every Workbench envelope preserves common metadata and uses the closed Workbench failure", async () => {
  const engine = await readJSON("engine-protocol/v1.schema.json");
  const workbenchRequests = Object.entries(engine.$defs).filter(([, schema]) =>
    schema?.properties?.operation?.const?.startsWith("engine.") &&
    !["engine.compile", "engine.handshake"].includes(schema.properties.operation.const));
  assert.ok(workbenchRequests.length > 0);
  for (const [requestName, request] of workbenchRequests) {
    assert.deepEqual(request.properties.extensions, {$ref: "https://schemas.layerdraw.dev/protocol-common/v1#/$defs/Extensions"}, requestName);
    assert.deepEqual(request.properties.trace_context, {$ref: "https://schemas.layerdraw.dev/protocol-common/v1#/$defs/JsonObject"}, requestName);
    const responseName = requestName.replace(/RequestEnvelope$/, "ResponseEnvelope");
    const response = engine.$defs[responseName];
    assert.notEqual(response, undefined, responseName);
    assert.deepEqual(response.properties.extensions, {$ref: "https://schemas.layerdraw.dev/protocol-common/v1#/$defs/Extensions"}, responseName);
    assert.deepEqual(response.properties.failure, {$ref: "#/$defs/WorkbenchFailure"}, responseName);
  }
});

test("Workbench invariant profiles and Language 1 domain type ownership stay explicit", async () => {
  const engine = await readJSON("engine-protocol/v1.schema.json");
  const semantic = await readJSON("semantic/v1.schema.json");
  const common = await readJSON("protocol-common/v1.schema.json");
  const pagedFamilies = ["ListModules", "FindSymbols", "InspectSubgraph", "ReadDeclarations", "ReadRows", "GetNeighbors", "FindUsages", "ReadScope", "ListReferences", "ReadReferences"];
  for (const family of pagedFamilies) {
    assert.equal(engine.$defs[`${family}Input`]["x-layerdraw-protocol-invariant"], "document_bound_input", family);
    assert.equal(engine.$defs[`${family}Result`]["x-layerdraw-protocol-invariant"], "paged_result", family);
  }
  assert.equal(engine.$defs.CloseDocumentInput["x-layerdraw-protocol-invariant"], "document_bound_input");
  assert.equal(engine.$defs.OpenDocumentResult["x-layerdraw-protocol-invariant"], "open_document_result");
  assert.equal(engine.$defs.ExecuteQueryResult["x-layerdraw-protocol-invariant"], "query_result");
  assert.equal(engine.$defs.MaterializeViewResult["x-layerdraw-protocol-invariant"], "view_data_result");
  for (const name of ["AssetRef", "EntityRepresentation", "RelationEndpointRule", "RelationCardinality", "RelationTraversalPolicy", "RelationProjectionSet", "RelationRenderSet", "RelationExport", "AuthoredViewShape"]) {
    assert.equal(engine.$defs[name], undefined, `${name} must not be duplicated in the Engine schema`);
  }
  assert.deepEqual(engine.$defs.SemanticOperation.oneOf, [{$ref: "#/$defs/CreateSubjectOperation"}, {$ref: "#/$defs/NonCreateSemanticOperation"}]);
  assert.equal(engine.$defs.CreateSubjectOperation.oneOf.length, semantic.$defs.CreatableSubjectKind.enum.length);
  const nonCreate = engine.$defs.NonCreateSemanticOperation;
  const expectedOptional = {
    create_relation: ["fields", "placement"],
    delete_row: [],
    delete_subject: [],
    migrate_project_identity: [],
    move_entity_to_layer: [],
    rename_subject: [],
    update_relation_endpoint: [],
    update_subject_field: ["value"],
    upsert_row: ["explicit_absent_column_addresses", "placement"],
  };
  for (const [operation, expected] of Object.entries(expectedOptional)) {
    const variant = nonCreate["x-layerdraw-tagged-union"].variants[operation];
    const required = new Set(["operation", ...(variant.required ?? [])]);
    const forbidden = new Set(variant.forbidden ?? []);
    const optional = Object.keys(nonCreate.properties).filter((name) => !required.has(name) && !forbidden.has(name));
    assert.deepEqual(optional, expected, `${operation} optional field inventory`);
  }
  assert.deepEqual(engine.$defs.ColumnCreateSubjectFields.properties.format, {$ref: "https://schemas.layerdraw.dev/semantic/v1#/$defs/StringFormat"});
  assert.deepEqual(engine.$defs.QueryParameterCreateSubjectFields.properties.format, {$ref: "https://schemas.layerdraw.dev/semantic/v1#/$defs/StringFormat"});
  assert.deepEqual(engine.$defs.ViewExportCreateSubjectFields.properties.format, {$ref: "https://schemas.layerdraw.dev/semantic/v1#/$defs/ExportFormat"});
  assert.equal(engine.$defs.CreateSubjectFields.properties, undefined);
  assert.deepEqual(semantic.$defs.AuthoringImpact.properties.required_capabilities.items, {$ref: "#/$defs/AuthoringCapability"});
  assert.deepEqual(engine.$defs.WorkbenchPreviewResult.properties.required_authoring_capabilities.items, {$ref: "https://schemas.layerdraw.dev/semantic/v1#/$defs/AuthoringCapability"});
  assert.notEqual(semantic.$defs.AuthoringCapability, undefined);
  assert.equal(engine.$defs.AuthoringCapability, undefined);
  assert.equal(common.$defs.AuthoringCapability, undefined);
  assert.deepEqual(semantic.$defs.QueryRecipeParameter.properties.format, {$ref: "#/$defs/StringFormat"});
  assert.deepEqual(semantic.$defs.ViewTableValueType.properties.format, {$ref: "#/$defs/StringFormat"});
  const systemBoundary = await readFile(new URL("../../../docs/system-boundary-contracts-specification.md", import.meta.url), "utf8");
  const packageBoundary = await readFile(new URL("../../../docs/component-package-boundary-specification.md", import.meta.url), "utf8");
  assert.match(systemBoundary, /access-protocol -> semantic/);
  assert.match(packageBoundary, /AuthoringCapabilityの再定義/);
});

test("query parameter active enum order is authored while reserved order is canonical", async () => {
  const compile = await authority();
  const validate = compile("https://schemas.layerdraw.dev/semantic/v1", "QueryRecipeParameter");
  const base = {id: "x", address: "ldl:project:p:query:q:parameter:x", value_type: "enum", enum_values: ["z", "a"], reserved_enum_values: [], required: false};
  assert.equal(validate(base), true, JSON.stringify(validate.errors));
  assert.equal(validate({...base, enum_values: ["choice"], reserved_enum_values: ["z", "a"]}), false);
});

test("standalone create_subject field authority rejects loose formats", async () => {
  const compile = await authority();
  const engine = "https://schemas.layerdraw.dev/engine-protocol/v1";
  assert.equal(compile(engine, "CreateSubjectFields")({format: "garbage"}), false);
  assert.equal(compile(engine, "ColumnCreateSubjectFields")({display_name: "Email", value_type: "string", format: "garbage"}), false);
  assert.equal(compile(engine, "ViewExportCreateSubjectFields")({format: "garbage", filename: "bad.out", fidelity: "lossless"}), false);
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
  assert.equal(predicate({kind: "field", field: "id", operand_type: {kind: "scalar", scalar_type: "string"}, operator: "eq", value: {kind: "scalar", scalar_value: {kind: "string", string_value: "x"}}}), true);
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

test("independent schema authority preserves compiler semantic authority", async () => {
  const compile = await authority();
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  const engine = "https://schemas.layerdraw.dev/engine-protocol/v1";
  const corpusValue = async (path, name) => {
    const corpus = await readJSON(path);
    return structuredClone(corpus.canonical_cases.find((item) => item.name === name).value);
  };
  const parameter = (format, value) => ({
    id: "x", address: "ldl:project:p:query:q:parameter:x", value_type: "string",
    reserved_enum_values: [], required: false, format,
    default: {kind: "string", string_value: value},
  });
  const validateParameter = compile(semantic, "QueryRecipeParameter");
  for (const [format, value] of [["email", "first.last@example.com"], ["email", "first.last@EXAMPLE.COM"], ["ipv6", "::ffff:192.0.2.1"], ["cidr", "192.0.2.0/24"]]) assert.equal(validateParameter(parameter(format, value)), true);
  for (const [format, value] of [["uri", "http://exa mple.com"], ["ipv6", "1:2:3:4:5:6:7:8:9"], ["ipv6", "1::2::3"], ["cidr", "192.0.2.1/24"]]) assert.equal(validateParameter(parameter(format, value)), false);
  assert.equal(compile(semantic, "RecipeScalar")({kind: "datetime", string_value: "2026-07-15T12:34:56.120Z"}), false);

  const validatePredicate = compile(semantic, "RecipePredicate");
  assert.equal(validatePredicate({kind: "field", field: "id", operand_type: {kind: "scalar", scalar_type: "string"}, operator: "in", value: {kind: "scalar_set", scalar_values: [{kind: "string", string_value: "z"}, {kind: "string", string_value: "a"}]}}), true);

  const validateQuery = compile(semantic, "QueryRecipe");
  let query = await corpusValue("fixtures/conformance/query-authority-v1.json", "query_recipe_minimal");
  query.where = {kind: "field", field: "from", operator: "exists", operand_type: {kind: "address", address_kind: "entity"}};
  assert.equal(validateQuery(query), false);
  query = await corpusValue("fixtures/conformance/query-authority-v1.json", "query_recipe_minimal");
  query.relation_where = {kind: "field", field: "layer", operator: "exists", operand_type: {kind: "address", address_kind: "layer"}};
  assert.equal(validateQuery(query), false);

  const validateView = compile(semantic, "ViewRecipe");
  let view = await corpusValue("fixtures/conformance/view-export-semantics-v1.json", "complete_owned_view_graph");
  view.dependencies.query_addresses = [];
  assert.equal(validateView(view), false);
  view = await corpusValue("fixtures/conformance/view-export-semantics-v1.json", "complete_owned_view_graph");
  view.dependencies.export_addresses = [];
  assert.equal(validateView(view), false);
  view = await corpusValue("fixtures/conformance/view-export-semantics-v1.json", "complete_owned_view_graph");
  const renameExport = (source, id) => ({...structuredClone(source), id, address: `ldl:project:p:view:v:export:${id}`, filename: `${id}.json`});
  const zebra = renameExport(view.exports[0], "zebra");
  const alpha = renameExport(view.exports[0], "alpha");
  view.exports = [zebra, alpha];
  view.dependencies.export_addresses = [zebra.address, alpha.address];
  assert.equal(validateView(view), true);

  view = await corpusValue("fixtures/conformance/view-export-semantics-v1.json", "complete_owned_view_graph");
  const parameterAddress = "ldl:project:p:query:all:parameter:x";
  view.source.arguments = {[parameterAddress]: {kind: "string", string_value: "ldl:project:p:entity:not-a-dependency"}};
  view.dependencies.parameter_addresses = [parameterAddress];
  assert.equal(validateView(view), true);

  view = await corpusValue("fixtures/conformance/view-export-semantics-v1.json", "complete_owned_view_graph");
  Object.assign(view, {
    category: "diff",
    source: {kind: "diff", before: "before", after: "after", arguments: {}},
    shape: {kind: "diff", diff: {include: [], detect_moves: false}},
    exports: [],
  });
  Object.assign(view.dependencies, {query_addresses: [], export_addresses: []});
  assert.equal(validateView(view), true);
  view.dependencies.entity_addresses = ["ldl:project:p:entity:extra"];
  assert.equal(validateView(view), false);
  view.dependencies.entity_addresses = [];
  view.dependencies.state_reads = [{subject_kind: "entity", field_path: "system.created_at", value_type: "datetime"}];
  assert.equal(validateView(view), false);

  view = await corpusValue("fixtures/conformance/view-export-semantics-v1.json", "complete_owned_view_graph");
  const relationTypeAddress = "ldl:project:p:relation-type:r";
  const branchColumnAddress = `${relationTypeAddress}:column:branch`;
  view.relation_projection_overrides = {[relationTypeAddress]: {flow: {
    source_endpoint: "from", target_endpoint: "to", connector_kind: "control", branch_value_column_address: branchColumnAddress,
  }}};
  view.dependencies.relation_type_addresses = [relationTypeAddress];
  view.dependencies.column_addresses = [branchColumnAddress];
  assert.equal(validateView(view), true);
  view.dependencies.column_addresses = [];
  assert.equal(validateView(view), false);

  view = await corpusValue("fixtures/conformance/semantic-root-authority-v1.json", "owned_table_columns_disjoint_from_reservations");
  view.relation_projection_overrides = {"ldl:project:p:relation-type:r": {table: {row_mode: "automatic", include_from: true, include_to: true, include_relation_type: true}}};
  view.dependencies.relation_type_addresses = ["ldl:project:p:relation-type:r"];
  Object.assign(view.shape.table, {automatic_relation_columns: ["from", "relation_type", "to"], columns: [], include_entity_id: false, include_type: false, include_layer: false, row_source: "automatic_relations", sorts: [{column_id: "from", direction: "ascending", absent: "last"}]});
  assert.equal(validateView(view), true);
  view.relation_projection_overrides["ldl:project:p:relation-type:r"].table = {row_mode: "automatic", include_from: false, include_to: false, include_relation_type: false};
  view.shape.table.automatic_relation_columns = [];
  assert.equal(validateView(view), false);

  const validateExport = compile(semantic, "ExportRecipe");
  const exported = await corpusValue("fixtures/conformance/view-export-semantics-v1.json", "contract_export_svg");
  exported.source_refs = true;
  exported.requires_source_manifest = false;
  assert.equal(validateExport(exported), false);

  const hash = (character) => `sha256:${character.repeat(64)}`;
  const module = (path) => ({origin: {kind: "project"}, module_path: path});
  const range = (path) => ({...module(path), start_byte: "0", end_byte: "1"});
  assert.equal(compile(semantic, "ChildSetHash")({owner_address: "ldl:project:p:entity:e", child_kind: "query_parameter", child_addresses: [], hash: hash("a")}), false);
  assert.equal(compile(semantic, "SourceBindingRecord")({source_address: "ldl:project:p:view:v", target_address: "ldl:project:p:query:q:parameter:x", target_kind: "query_parameter", via: "argument", module: module("document.ldl"), range: range("document.ldl")}), false);

  const result = structuredClone((await readJSON("fixtures/engine/compile-success.json")).payload);
  const childSets = [
    {owner_address: "ldl:project:p", child_kind: "entity_type", child_addresses: [], hash: hash("a")},
    {owner_address: "ldl:project:p", child_kind: "relation_type", child_addresses: [], hash: hash("b")},
  ];
  const validateResult = compile(engine, "CompileResult");
  result.child_set_hashes = childSets;
  assert.equal(validateResult(result), true);
  result.child_set_hashes = [childSets[1], childSets[0]];
  assert.equal(validateResult(result), false);

  const semanticIndex = structuredClone((await readJSON("fixtures/engine/compile-success.json")).payload.semantic_index);
  const references = [
    {source_address: "ldl:project:p:entity:a", target_address: "ldl:project:p:entity:b", target_kind: "entity", via: "test", range: range("document.ldl")},
    {source_address: "ldl:project:p:entity:b", target_address: "ldl:project:p:entity:a", target_kind: "entity", via: "test", range: range("document.ldl")},
  ];
  const validateIndex = compile(semantic, "SemanticIndex");
  semanticIndex.references = references;
  assert.equal(validateIndex(semanticIndex), true);
  semanticIndex.references = [references[1], references[0]];
  assert.equal(validateIndex(semanticIndex), false);

  const sourceMap = structuredClone((await readJSON("fixtures/engine/compile-success.json")).payload.source_map);
  const files = [
    {origin: {kind: "project"}, module_path: "a.ldl", digest: hash("a"), byte_length: "0"},
    {origin: {kind: "project"}, module_path: "z.ldl", digest: hash("b"), byte_length: "0"},
  ];
  const validateSourceMap = compile(semantic, "SourceMap");
  sourceMap.files = files;
  assert.equal(validateSourceMap(sourceMap), true);
  sourceMap.files = [files[1], files[0]];
  assert.equal(validateSourceMap(sourceMap), false);
});
