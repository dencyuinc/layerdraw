// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { deflateSync } from "node:zlib";
import { browserRasterizer } from "../dist/browser.js";
import { serializeExport } from "../dist/index.js";
import { nodeRasterizer } from "../dist/node.js";

const fixture = JSON.parse(await readFile(new URL("../../../schemas/fixtures/conformance/export-plan-transport-parity-v1.json", import.meta.url)));
const corpus = JSON.parse(await readFile(new URL("../../../tests/conformance/testdata/viewdata_conformance_v1.json", import.meta.url)));
const base = () => {
  const plan = structuredClone(fixture.export_plan);
  plan.required_font_digests = [];
  return { export_plan: plan, view_data: structuredClone(fixture.input.view_data),
    serializer_profile: { schema_version: 1, ref: plan.serializer_profile }, assets: [], fonts: [] };
};

test("canonical JSON is deterministic, selected, and LF terminated", async () => {
  const input = base();
  const first = await serializeExport(input);
  const second = await serializeExport(input);
  assert.equal(first.ok, true);
  assert.equal(second.ok, true);
  assert.deepEqual(first.artifacts[0].bytes, second.artifacts[0].bytes);
  assert.equal(new TextDecoder().decode(first.artifacts[0].bytes).endsWith("\n"), true);
  const artifact = JSON.parse(new TextDecoder().decode(first.artifacts[0].bytes));
  assert.deepEqual(Object.keys(artifact), ["format", "schema_version", "view_data"]);
  assert.equal(artifact.format, "layerdraw-viewdata");
});

test("JSON emits the selected complete semantic artifact for every ViewData shape", async () => {
  for (const name of ["composed_diagram","table_automatic","matrix","tree","flow","context","definition_diff"]) {
    const view=structuredClone(corpus.cases.find((item)=>item.name===name).expected.normalized_response.payload.view_data);
    const input=base();input.view_data=view;input.export_plan.view_data_hash=await viewHash(view);
    input.export_plan.recipe_address=`${view.view_address}:export:json`;
    input.export_plan.state_policy=view.state_policy;input.export_plan.state_input=view.state_input;
    input.export_plan.representations=[{viewdata_key:"viewdata-root",artifact_role:"viewdata",unit_id:"unit:viewdata",locator:"unit:viewdata:viewdata-root",disposition:"embedded",source:view.source}];
    input.export_plan.units=[{unit_id:"unit:viewdata",kind:"section",order:"0",role:"viewdata",artifact_role:"viewdata",viewdata_keys:["viewdata-root"]}];
    const result=await serializeExport(input);
    assert.equal(result.ok,true,`${name}: ${JSON.stringify(result)}`);
    assert.equal(JSON.parse(new TextDecoder().decode(result.artifacts[0].bytes)).view_data.kind,view.kind);
  }
});

test("manifest binds completed artifacts without recursively listing itself", async () => {
  const input = base();
  input.export_plan.source_manifest_required = true;
  input.export_plan.source_manifest_path = "context.sources.json";
  const result = await serializeExport(input);
  assert.equal(result.ok, true);
  assert.equal(result.source_manifest.artifacts.length, 1);
  assert.equal(result.source_manifest.artifacts[0].content_digest, result.artifacts[0].entry.content_digest);
  assert.equal(result.source_manifest_path, "context.sources.json");
  assert.match(result.source_manifest_digest, /^sha256:[0-9a-f]{64}$/);
  assert.equal(new TextDecoder().decode(result.source_manifest_bytes).endsWith("\n"), true);
  assert.equal(result.source_manifest.artifacts.some((item) => item.logical_path === "context.sources.json"), false);
});

test("selected state summaries are hash-bound and unselected summaries are rejected", async () => {
  const input=base();
  const snapshot={kind:"snapshot",snapshot_hash:"sha256:"+"1".repeat(64),state_version:"7",captured_at:"2026-07-19T00:00:00Z",definition_hash:input.view_data.revision.definition_hash};
  input.view_data.state_policy="optional";input.view_data.state_input=snapshot;
  input.export_plan.state_policy="optional";input.export_plan.state_input=snapshot;
  input.export_plan.view_data_hash=await viewHash(input.view_data);
  input.export_plan.serializer_options.state_summary=true;
  input.export_plan.source_manifest_required=true;input.export_plan.source_manifest_path="context.sources.json";
  const payload={active:true,count:"2"};
  const summary={format:"layerdraw-state-summary",schema_version:1,definition_hash:snapshot.definition_hash,state_version:"7",payload,payload_hash:await rawDigest(new TextEncoder().encode(canonical(payload)))};
  input.state_summary=summary;
  input.export_plan.state_summary_hash=await semanticDigest("export-state-summary",summary);
  const result=await serializeExport(input);
  assert.equal(result.ok,true,JSON.stringify(result));
  assert.deepEqual(JSON.parse(new TextDecoder().decode(result.artifacts[0].bytes)).state_summary,summary);

  const corrupt=structuredClone(input);corrupt.state_summary.payload_hash="sha256:"+"0".repeat(64);
  assert.equal((await serializeExport(corrupt)).diagnostics[0].code,"export.serializer_failed");
  const unselected=base();unselected.state_summary=summary;
  assert.equal((await serializeExport(unselected)).diagnostics[0].code,"export.serializer_failed");
});

test("stable public failure classes do not expose host errors", async () => {
  const missing = base(); delete missing.serializer_profile;
  assert.equal((await serializeExport(missing)).diagnostics[0].code, "export.profile_missing");

  const incompatible = base(); incompatible.serializer_profile.ref = { ...incompatible.serializer_profile.ref, id: "layerdraw/other@1" };
  assert.equal((await serializeExport(incompatible)).diagnostics[0].code, "export.profile_incompatible");

  const fidelity = base(); fidelity.export_plan.effective_maximum_fidelity = "lossy";
  assert.equal((await serializeExport(fidelity)).diagnostics[0].code, "export.fidelity_unavailable");

  const assets = base(); assets.export_plan.required_asset_digests = ["sha256:" + "a".repeat(64)];
  assert.equal((await serializeExport(assets)).diagnostics[0].code, "export.asset_missing");

  const fonts = base(); fonts.export_plan.required_font_digests = ["sha256:" + "b".repeat(64)];
  assert.equal((await serializeExport(fonts)).diagnostics[0].code, "export.font_missing");

  const malformed = base(); malformed.export_plan.extra = true;
  assert.equal((await serializeExport(malformed)).diagnostics[0].code, "export.serializer_failed");

  const bounded = base(); bounded.serializer_profile.limits = {max_input_bytes:1};
  assert.equal((await serializeExport(bounded)).diagnostics[0].code, "export.serializer_failed");

  const manifest = base(); manifest.export_plan.source_manifest_required = true;
  assert.equal((await serializeExport(manifest)).diagnostics[0].code, "export.source_manifest_invalid");

  const unsupported = base();
  const htmlProfile = {...unsupported.export_plan.serializer_profile,format:"html",id:"layerdraw/html@1"};
  unsupported.export_plan.format="html"; unsupported.export_plan.exporter_profile=htmlProfile; unsupported.export_plan.serializer_profile=htmlProfile;
  unsupported.serializer_profile.ref=htmlProfile; unsupported.export_plan.serializer_options={kind:"html",interactive:false,embed_assets:false};
  assert.equal((await serializeExport(unsupported)).diagnostics[0].code, "export.unsupported_shape_format");

  const aborted = base(); const controller = new AbortController(); controller.abort(); aborted.signal = controller.signal;
  assert.equal((await serializeExport(aborted)).diagnostics[0].code, "export.serializer_failed");
});

test("closed public inputs, exact resources, topology, mappings, and output bounds are enforced", async () => {
  assert.equal((await serializeExport(null)).diagnostics[0].code,"export.serializer_failed");
  const unknown=base();unknown.ambient=true;
  assert.equal((await serializeExport(unknown)).diagnostics[0].code,"export.serializer_failed");
  const badLimits=base();badLimits.serializer_profile.limits={unknown:1};
  assert.equal((await serializeExport(badLimits)).diagnostics[0].code,"export.profile_incompatible");
  const zeroLimit=base();zeroLimit.serializer_profile.limits={max_output_bytes:0};
  assert.equal((await serializeExport(zeroLimit)).diagnostics[0].code,"export.profile_incompatible");
  const badClock=base();badClock.clock={fixed_rfc3339:"now"};
  assert.equal((await serializeExport(badClock)).diagnostics[0].code,"export.profile_incompatible");
  const fixedClock=base();fixedClock.clock={fixed_rfc3339:"2026-07-19T00:00:00Z"};
  assert.equal((await serializeExport(fixedClock)).ok,true);

  const resourceBytes=new TextEncoder().encode("asset");
  const resourceDigest=await rawDigest(resourceBytes);
  const exact=base();exact.export_plan.required_asset_digests=[resourceDigest];exact.assets=[{digest:resourceDigest,bytes:resourceBytes}];
  assert.equal((await serializeExport(exact)).ok,true);
  const corrupt=base();corrupt.export_plan.required_asset_digests=[resourceDigest];corrupt.assets=[{digest:resourceDigest,bytes:new TextEncoder().encode("wrong")}];
  assert.equal((await serializeExport(corrupt)).diagnostics[0].code,"export.asset_missing");
  const malformedResource=base();malformedResource.export_plan.required_asset_digests=[resourceDigest];malformedResource.assets=[{digest:resourceDigest,bytes:resourceBytes,extra:true}];
  assert.equal((await serializeExport(malformedResource)).diagnostics[0].code,"export.asset_missing");
  const completed=base();completed.export_plan.artifacts[0].content_digest="sha256:"+"a".repeat(64);
  assert.equal((await serializeExport(completed)).diagnostics[0].code,"export.source_manifest_invalid");

  for (const mutate of [
    (input)=>input.export_plan.artifacts.push({...input.export_plan.artifacts[0]}),
    (input)=>{input.export_plan.units[0].artifact_role="missing";},
    (input)=>{input.export_plan.units[0].viewdata_keys=[];},
    (input)=>{input.export_plan.representations.push({...input.export_plan.representations[0]});},
    (input)=>{input.export_plan.source_manifest_required=true;input.export_plan.source_manifest_path="context.sources.json";input.export_plan.artifacts[0].logical_path="context.sources.json";},
  ]) {
    const input=base();mutate(input);
    assert.equal((await serializeExport(input)).diagnostics[0].code,"export.source_manifest_invalid");
  }
  const mapping=base();mapping.export_plan.representations[0].source={...mapping.export_plan.representations[0].source,subject_addresses:[]};
  assert.equal((await serializeExport(mapping)).diagnostics[0].code,"export.render_input_mismatch");
  const recipe=base();recipe.export_plan.recipe_address="ldl:project:p:view:other:export:json";
  assert.equal((await serializeExport(recipe)).diagnostics[0].code,"export.render_input_mismatch");
  const output=base();output.serializer_profile.limits={max_output_bytes:1};
  assert.equal((await serializeExport(output)).diagnostics[0].code,"export.serializer_failed");
});

function visualInput(format = "svg") {
  const input = base();
  const profile = { ...input.export_plan.serializer_profile, format, id: `layerdraw/${format}@1` };
  input.export_plan.format = format;
  input.export_plan.exporter_profile = profile;
  input.export_plan.serializer_profile = profile;
  input.serializer_profile.ref = profile;
  input.export_plan.serializer_options = format === "svg"
    ? { kind: "svg", width: {kind:"auto"}, height: {kind:"auto"}, scale:"1", background:"transparent" }
    : { kind: "png", width: {kind:"value",value:"64"}, height: {kind:"value",value:"32"}, scale:"1", background:"transparent" };
  input.export_plan.artifacts = [{logical_path:`context.${format}`, media_type: format === "svg" ? "image/svg+xml" : "image/png", primary:true, role:"visual"}];
  input.export_plan.units = [{unit_id:"unit:visual",kind:"section",order:"0",role:"visual",artifact_role:"visual",viewdata_keys:input.export_plan.units[0].viewdata_keys}];
  input.export_plan.representations = input.export_plan.representations.map((item, index) => ({...item, disposition:"rendered",artifact_role:"visual",unit_id:"unit:visual",locator:`visual-${index}`}));
  input.export_plan.requires_renderer = true;
  input.export_plan.layout_requirement = "presentation_geometry";
  const key = input.view_data.context.groups[0].key;
  input.render_data = {
    render_data_schema_version:1, renderer_profile:{profile_id:"test",profile_version:"1",specification_digest:"sha256:"+"c".repeat(64)},
    view_data_hash:input.export_plan.view_data_hash, render_input_hash:"sha256:"+"d".repeat(64), shape:"context",kind:"context",
    layout_seed:"fixed",locale:"en-US",timezone:"UTC",bounds:{x:0,y:0,width:64,height:32},
    source_bindings:[{render_key:"group",viewdata_key:key,source_refs:input.view_data.context.groups[0].source},{render_key:"root",viewdata_key:key,source_refs:input.view_data.context.groups[0].source}],
    resolved_asset_digests:[],resolved_font_digests:[],diagnostics:[],
    groups:[{render_key:"root",bounds:{x:0,y:0,width:64,height:32},label:"Context"}],
    facts:[{render_key:"group",bounds:{x:4,y:4,width:40,height:20},group_key:"root",text:"safe <text>"}],
    relation_summaries:[],truncation_markers:[]
  };
  return input;
}

test("SVG implements planned locators and escapes text", async () => {
  const result = await serializeExport(visualInput());
  assert.equal(result.ok, true, JSON.stringify(result));
  const svg = new TextDecoder().decode(result.artifacts[0].bytes);
  assert.match(svg, /id="visual-0"/);
  assert.match(svg, /id="visual-1"/);
  assert.match(svg, /safe &lt;text&gt;/);
  assert.doesNotMatch(svg, /render:/);
});

test("SVG covers every compatible planned visual shape", async () => {
  const cases = {
    diagram: ["composed_diagram", (view) => [view.diagram.occurrences[0], {occurrences:[{render_key:"item",bounds:{x:0,y:0,width:20,height:10},port_keys:["port"],label_key:"label"}],containers:[],ports:[{render_key:"port",position:{x:1,y:1},occurrence_key:"item"}],edge_paths:[{render_key:"edge",points:[{x:1,y:1},{x:2,y:2}],from_port_key:"port",to_port_key:"port"}],labels:[{render_key:"label",bounds:{x:0,y:0,width:10,height:5},text:"Label",anchor:{kind:"occurrence",occurrence_key:"item"}}],overlays:[],badges:[],support_geometry:[]}]],
    matrix: ["matrix", (view) => [view.matrix.row_axis[0], {row_axes:[{render_key:"item",bounds:{x:0,y:0,width:20,height:10},label:"Row"}],column_axes:[],cells:[],totals:[]}]],
    tree: ["tree", (view) => [view.tree.roots[0], {occurrences:[{render_key:"item",bounds:{x:0,y:0,width:20,height:10},depth:0,label:"Root"}],duplicate_refs:[],cycle_refs:[]}]],
    flow: ["flow", (view) => [view.flow.steps[0], {lanes:[{render_key:"lane",bounds:{x:0,y:0,width:30,height:20},label:"Lane"}],steps:[{render_key:"item",bounds:{x:2,y:2,width:20,height:10},lane_key:"lane",label:"Step"}],branches:[],joins:[],connectors:[],cycle_refs:[]}]],
    diff: ["definition_diff", (view) => [view.diff.changes[0], {before:[{render_key:"item",bounds:{x:0,y:0,width:20,height:10},label:"Before"}],after:[],changes:[],fields:[]}]],
  };
  for (const [shape,[name,primitive]] of Object.entries(cases)) {
    const view = structuredClone(corpus.cases.find((item) => item.name === name).expected.normalized_response.payload.view_data);
    const input = base(); input.view_data=view; input.export_plan.view_data_hash=await viewHash(view);
    const profile={...input.export_plan.serializer_profile,format:"svg",id:"layerdraw/svg@1"};
    input.export_plan.format="svg";input.export_plan.exporter_profile=profile;input.export_plan.serializer_profile=profile;input.serializer_profile.ref=profile;
    input.export_plan.recipe_address=`${view.view_address}:export:svg`;
    input.export_plan.serializer_options={kind:"svg",width:{kind:"auto"},height:{kind:"auto"},scale:"1",background:"transparent"};
    input.export_plan.artifacts=[{logical_path:`${shape}.svg`,media_type:"image/svg+xml",primary:true,role:"visual"}];
    const [item,sets]=primitive(view);
    input.export_plan.units=[{unit_id:"unit:visual",kind:"section",order:"0",role:"visual",artifact_role:"visual",viewdata_keys:[item.key,"viewdata-root"].sort()}];
    input.export_plan.representations=[{viewdata_key:"viewdata-root",artifact_role:"visual",unit_id:"unit:visual",locator:"root",disposition:"rendered",source:view.source},{viewdata_key:item.key,artifact_role:"visual",unit_id:"unit:visual",locator:"item",disposition:"rendered",source:item.source}];
    input.export_plan.requires_renderer=true;input.export_plan.layout_requirement="presentation_geometry";input.export_plan.state_policy=view.state_policy;input.export_plan.state_input=view.state_input;
    const renderKeys=Object.values(sets).flat().map((entry)=>entry.render_key);
    input.render_data={render_data_schema_version:1,renderer_profile:{profile_id:"test",profile_version:"1",specification_digest:"sha256:"+"c".repeat(64)},view_data_hash:input.export_plan.view_data_hash,render_input_hash:"sha256:"+"d".repeat(64),shape,kind:shape,layout_seed:"fixed",locale:"en-US",timezone:"UTC",bounds:{x:0,y:0,width:30,height:20},source_bindings:renderKeys.sort().map((render_key)=>({render_key,viewdata_key:item.key,source_refs:item.source})),resolved_asset_digests:[],resolved_font_digests:[],diagnostics:[],...sets};
    const result=await serializeExport(input);
    assert.equal(result.ok,true,`${shape}: ${JSON.stringify(result)}`);
    assert.match(new TextDecoder().decode(result.artifacts[0].bytes),/id="item"/);
  }
});

test("visual export requires matching RenderData", async () => {
  const missing = visualInput(); delete missing.render_data;
  assert.equal((await serializeExport(missing)).diagnostics[0].code, "export.render_required");
  const mismatch = visualInput(); mismatch.render_data.view_data_hash = "sha256:" + "e".repeat(64);
  assert.equal((await serializeExport(mismatch)).diagnostics[0].code, "export.render_input_mismatch");
  const sourceMismatch=visualInput();sourceMismatch.render_data.source_bindings[0].source_refs=sourceMismatch.view_data.source;
  assert.equal((await serializeExport(sourceMismatch)).diagnostics[0].code,"export.render_input_mismatch");
  const missingBinding=visualInput();missingBinding.render_data.source_bindings.pop();
  assert.equal((await serializeExport(missingBinding)).diagnostics[0].code,"export.render_input_mismatch");
  const unmappedBinding=visualInput();unmappedBinding.render_data.source_bindings[0].viewdata_key="viewdata-root";
  assert.equal((await serializeExport(unmappedBinding)).diagnostics[0].code,"export.render_input_mismatch");
  const duplicateBinding=visualInput();duplicateBinding.render_data.source_bindings.push({...duplicateBinding.render_data.source_bindings[0]});
  assert.equal((await serializeExport(duplicateBinding)).diagnostics[0].code,"export.render_input_mismatch");
  const orphanBinding=visualInput();orphanBinding.render_data.source_bindings.push({...orphanBinding.render_data.source_bindings[0],render_key:"orphan"});
  assert.equal((await serializeExport(orphanBinding)).diagnostics[0].code,"export.render_input_mismatch");
});

test("PNG requires a deterministic explicit versioned rasterizer", async () => {
  const input = visualInput("png");
  const rasterizerProfile = {profile_id:"fixed-png",profile_version:"1",specification_digest:"sha256:"+"f".repeat(64)};
  const crc32 = (bytes) => { let crc=0xffffffff; for(const byte of bytes){crc^=byte;for(let bit=0;bit<8;bit++)crc=(crc>>>1)^(0xedb88320 & -(crc&1));} return (crc^0xffffffff)>>>0; };
  const chunk = (type,data) => { const typeBytes=new TextEncoder().encode(type), out=new Uint8Array(12+data.length), view=new DataView(out.buffer); view.setUint32(0,data.length); out.set(typeBytes,4); out.set(data,8); view.setUint32(8+data.length,crc32(out.subarray(4,8+data.length))); return out; };
  const ihdr=new Uint8Array(13), ihdrView=new DataView(ihdr.buffer); ihdrView.setUint32(0,64);ihdrView.setUint32(4,32);ihdr[8]=8;ihdr[9]=6;
  const raw=new Uint8Array(32*(1+64*4));
  const parts=[new Uint8Array([137,80,78,71,13,10,26,10]),chunk("IHDR",ihdr),chunk("IDAT",deflateSync(raw)),chunk("IEND",new Uint8Array())];
  const png=new Uint8Array(parts.reduce((sum,item)=>sum+item.length,0)); let pngOffset=0; for(const part of parts){png.set(part,pngOffset);pngOffset+=part.length;}
  input.rasterizer_profile = rasterizerProfile;
  input.rasterizer = {api_version:1, environment:"node", profile:rasterizerProfile,
    rasterize:async (request) => ({bytes:png,width:request.width,height:request.height,density:request.density,profile:rasterizerProfile})};
  const result = await serializeExport(input);
  assert.equal(result.ok, true, JSON.stringify(result));
  assert.deepEqual(result.artifacts[0].bytes, png);

  const missing = visualInput("png");
  assert.equal((await serializeExport(missing)).diagnostics[0].code, "export.profile_missing");

  const unstable = visualInput("png"); let calls = 0;
  unstable.rasterizer_profile = rasterizerProfile;
  unstable.rasterizer = {api_version:1,environment:"browser",profile:rasterizerProfile,rasterize:async(request) => {
    const bytes = new Uint8Array(png); bytes[23] = calls++;
    return {bytes,width:request.width,height:request.height,density:request.density,profile:rasterizerProfile};
  }};
  assert.equal((await serializeExport(unstable)).diagnostics[0].code, "export.serializer_failed");

  const incompatible=visualInput("png");incompatible.rasterizer_profile=rasterizerProfile;
  incompatible.rasterizer={api_version:1,environment:"node",profile:{...rasterizerProfile,profile_version:"2"},rasterize:async()=>{throw new Error("unused");}};
  assert.equal((await serializeExport(incompatible)).diagnostics[0].code,"export.profile_incompatible");
  const automatic=visualInput("png");automatic.export_plan.serializer_options.width={kind:"auto"};automatic.rasterizer_profile=rasterizerProfile;automatic.rasterizer=input.rasterizer;
  assert.equal((await serializeExport(automatic)).diagnostics[0].code,"export.profile_incompatible");
  const thrown=visualInput("png");thrown.rasterizer_profile=rasterizerProfile;thrown.rasterizer={api_version:1,environment:"node",profile:rasterizerProfile,rasterize:async()=>{throw new Error("secret host error");}};
  assert.equal((await serializeExport(thrown)).diagnostics[0].code,"export.serializer_failed");
  const badSignature=new Uint8Array(png);badSignature[0]=0;
  const badCRC=new Uint8Array(png);badCRC[29]^=1;
  for(const malformed of [png.subarray(0,20),badSignature,badCRC]) {
    const bad=visualInput("png");bad.rasterizer_profile=rasterizerProfile;bad.rasterizer={api_version:1,environment:"node",profile:rasterizerProfile,rasterize:async(request)=>({bytes:malformed,width:request.width,height:request.height,density:request.density,profile:rasterizerProfile})};
    assert.equal((await serializeExport(bad)).diagnostics[0].code,"export.serializer_failed");
  }
});

function canonical(value) {
  if (value === null || typeof value === "boolean" || typeof value === "number") return JSON.stringify(value);
  if (typeof value === "string") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  return `{${Object.keys(value).sort().map((key)=>`${JSON.stringify(key)}:${canonical(value[key])}`).join(",")}}`;
}

async function viewHash(view) {
  const projected = structuredClone(view);
  const strip = (value) => { if (Array.isArray(value)) value.forEach(strip); else if (value && typeof value === "object") { delete value.message; Object.values(value).forEach(strip); } };
  strip(projected.diagnostics);
  const bytes = new TextEncoder().encode(`layerdraw-language-1\0export-viewdata\0${canonical(projected)}`);
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return `sha256:${Array.from(digest,(byte)=>byte.toString(16).padStart(2,"0")).join("")}`;
}

async function rawDigest(bytes) {
  const digest=new Uint8Array(await crypto.subtle.digest("SHA-256",bytes));
  return `sha256:${Array.from(digest,(byte)=>byte.toString(16).padStart(2,"0")).join("")}`;
}

async function semanticDigest(domain,value) {
  return rawDigest(new TextEncoder().encode(`layerdraw-language-1\0${domain}\0${canonical(value)}`));
}

function parseCsvRecord(record) {
  const fields=[];
  let field="";
  let quoted=false;
  for (let index=0;index<record.length;index++) {
    const character=record[index];
    if (quoted && character==='"' && record[index+1]==='"') { field+='"'; index++; continue; }
    if (character==='"') { quoted=!quoted; continue; }
    if (character==="," && !quoted) { fields.push(field); field=""; continue; }
    field+=character;
  }
  fields.push(field);
  return fields;
}

async function tabularInput(view, groups, format = "csv") {
  const input = base(); input.view_data = view;
  input.export_plan.view_data_hash = await viewHash(view);
  const profile = {...input.export_plan.serializer_profile,format,id:`layerdraw/${format}@1`};
  input.export_plan.format = format; input.export_plan.exporter_profile = profile; input.export_plan.serializer_profile = profile;
  input.export_plan.recipe_address=`${view.view_address}:export:${format}`;
  input.serializer_profile.ref = profile;
  input.export_plan.serializer_options = {kind:format,bundle:true,header:true,source_manifest:true};
  input.export_plan.requested_fidelity = "traceable_summary";
  input.export_plan.native_maximum_fidelity = "traceable_summary";
  input.export_plan.effective_maximum_fidelity = "traceable_summary";
  input.export_plan.artifacts = groups.map(([role],index)=>({logical_path:index===0?`${view.kind}.${format}`:`${view.kind}.${role}.${format}`,media_type:format === "csv"?"text/csv":"text/tab-separated-values",primary:index===0,role}));
  input.export_plan.units = groups.map(([role,items])=>({unit_id:`unit:${role}`,kind:"sheet",order:String(groups.findIndex(([candidate])=>candidate===role)),role,artifact_role:role,viewdata_keys:items.map((item)=>item.key).sort()}));
  input.export_plan.representations = groups.flatMap(([role,items])=>items.map((item,index)=>({viewdata_key:item.key,artifact_role:role,unit_id:`unit:${role}`,locator:`row:${index+2}`,disposition:"tabular",source:item.source ?? view.source})));
  input.export_plan.requires_renderer = false; input.export_plan.layout_requirement = "none";
  input.export_plan.source_manifest_required = true; input.export_plan.source_manifest_path = `${view.kind}.sources.json`;
  return input;
}

async function assertCrossRoleRejected(view, groups, fromRole, toRole) {
  const input=await tabularInput(view,groups);
  const representation=input.export_plan.representations.find((item)=>item.artifact_role===fromRole);
  const unit=input.export_plan.units.find((item)=>item.artifact_role===toRole);
  unit.viewdata_keys=[...unit.viewdata_keys,representation.viewdata_key].sort();
  input.export_plan.representations.push({...representation,artifact_role:toRole,unit_id:unit.unit_id,locator:`row:cross-role-${fromRole}`});
  assert.equal((await serializeExport(input)).diagnostics[0].code,"export.serializer_failed");
}

test("CSV follows plan artifact roles and emits deterministic UTF-8 LF rows", async () => {
  const matrix = structuredClone(corpus.cases.find((item) => item.name === "matrix").expected.normalized_response.payload.view_data);
  const groups = [["cells",matrix.matrix.cells],["row_axis",matrix.matrix.row_axis],["column_axis",matrix.matrix.column_axis]];
  const input = await tabularInput(matrix, groups);
  const first = await serializeExport(input); const second = await serializeExport(input);
  assert.equal(first.ok, true, JSON.stringify(first));
  assert.deepEqual(first.artifacts.map((item)=>item.bytes), second.artifacts.map((item)=>item.bytes));
  assert.equal(first.artifacts.length, 3);
  const texts = first.artifacts.map((item)=>new TextDecoder().decode(item.bytes));
  assert.equal(texts[0].split("\n")[0], "viewdata_key,row_key,column_key,semantic_refs,display_value,source_refs");
  assert.equal(texts[1].split("\n")[0], "viewdata_key,entity_address,label,source_refs");
  assert.equal(texts[2].split("\n")[0], "viewdata_key,entity_address,label,source_refs");
  const text = texts[0];
  assert.equal(text.endsWith("\n"), true);
  assert.equal(text.includes("\r"), false);
});

test("Table CSV headers use the closed columns and rows schemas even when optional values are absent", async () => {
  const table = structuredClone(corpus.cases.find((item) => item.name === "table_automatic").expected.normalized_response.payload.view_data);
  for (const column of table.table.columns) { delete column.address; delete column.state_field_path; delete column.enum_values; }
  const input = await tabularInput(table, [["columns",table.table.columns],["rows",table.table.rows]]);
  const result = await serializeExport(input);
  assert.equal(result.ok,true,JSON.stringify(result));
  const columnLines = new TextDecoder().decode(result.artifacts[0].bytes).split("\n");
  assert.equal(columnLines[0],"viewdata_key,id,address,label,value_type,enum_values,source_column_addresses,state_field_path,source_refs");
  const fields=parseCsvRecord(columnLines[1]);
  assert.equal(fields.length,9);
  assert.equal(fields[2],"");
  assert.equal(fields[5],"");
  assert.equal(fields[7],"");
  assert.equal(new TextDecoder().decode(result.artifacts[1].bytes).split("\n")[0],"viewdata_key,cells,source_refs");
});

test("TableColumn representations inherit exact root SourceRefs", async () => {
  const table=structuredClone(corpus.cases.find((item)=>item.name==="table_automatic").expected.normalized_response.payload.view_data);
  const input=await tabularInput(table,[["columns",table.table.columns],["rows",table.table.rows]]);
  assert.equal(table.table.columns[0].source,undefined);
  const column=input.export_plan.representations.find((item)=>item.artifact_role==="columns");
  assert.deepEqual(column.source,table.source);
  assert.equal((await serializeExport(input)).ok,true);

  const wrong=structuredClone(input);
  wrong.export_plan.representations.find((item)=>item.viewdata_key===column.viewdata_key).source={...table.source,subject_addresses:[]};
  assert.equal((await serializeExport(wrong)).diagnostics[0].code,"export.render_input_mismatch");
});

test("Table representations reject keys from another normative role collection", async () => {
  const table=structuredClone(corpus.cases.find((item)=>item.name==="table_automatic").expected.normalized_response.payload.view_data);
  const groups=[["columns",table.table.columns],["rows",table.table.rows]];
  await assertCrossRoleRejected(table,groups,"columns","rows");
  await assertCrossRoleRejected(table,groups,"rows","columns");
});

test("Matrix representations reject keys from another normative role collection", async () => {
  const matrix=structuredClone(corpus.cases.find((item)=>item.name==="matrix").expected.normalized_response.payload.view_data);
  const groups=[["cells",matrix.matrix.cells],["row_axis",matrix.matrix.row_axis],["column_axis",matrix.matrix.column_axis]];
  await assertCrossRoleRejected(matrix,groups,"cells","row_axis");
  await assertCrossRoleRejected(matrix,groups,"row_axis","cells");
  await assertCrossRoleRejected(matrix,groups,"row_axis","column_axis");
});

test("TSV preserves plan row order and canonical quoting while unknown roles fail closed", async () => {
  const table=structuredClone(corpus.cases.find((item)=>item.name==="table_automatic").expected.normalized_response.payload.view_data);
  table.table.columns[0].label='A\t"B"\nC';
  const groups=[["columns",table.table.columns],["rows",table.table.rows]];
  const input=await tabularInput(table,groups,"tsv");
  const columnRepresentations=input.export_plan.representations.filter((item)=>item.artifact_role==="columns").reverse();
  input.export_plan.representations=[...columnRepresentations,...input.export_plan.representations.filter((item)=>item.artifact_role!=="columns")];
  const result=await serializeExport(input);
  assert.equal(result.ok,true,JSON.stringify(result));
  const text=new TextDecoder().decode(result.artifacts[0].bytes);
  assert.equal(text.split("\n")[0],"viewdata_key\tid\taddress\tlabel\tvalue_type\tenum_values\tsource_column_addresses\tstate_field_path\tsource_refs");
  assert.ok(text.indexOf(columnRepresentations[0].viewdata_key)<text.indexOf(columnRepresentations.at(-1).viewdata_key));
  assert.match(text,/"A\t""B""\nC"/u);
  assert.equal(text.includes("\r"),false);

  const unknown=await tabularInput(structuredClone(table),groups);
  unknown.export_plan.artifacts[0].role="invented";
  unknown.export_plan.units[0].artifact_role="invented";unknown.export_plan.units[0].role="invented";
  for(const representation of unknown.export_plan.representations) if(representation.artifact_role==="columns") representation.artifact_role="invented";
  assert.equal((await serializeExport(unknown)).diagnostics[0].code,"export.serializer_failed");
});

test("browser and Node boundaries reject the other environment", () => {
  const browser = {api_version:1,environment:"browser",profile:{profile_id:"p",profile_version:"1",specification_digest:"sha256:"+"a".repeat(64)},rasterize:async()=>{throw new Error("unused")}};
  assert.equal(browserRasterizer(browser), browser);
  assert.throws(() => nodeRasterizer(browser), /Node rasterizer/);
  const node = {...browser,environment:"node"};
  assert.equal(nodeRasterizer(node), node);
  assert.throws(() => browserRasterizer(node), /browser rasterizer/);
});
