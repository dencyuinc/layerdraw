// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import {createReviewModel, ReviewError} from "../dist/index.js";

const digest = `sha256:${"a".repeat(64)}`;
const revision = {document_id:"doc",revision_id:"revision_abcdefghijklmnop",definition_hash:digest,graph_hash:digest};
const actor = {actor_id:"owner",kind:"user"};
const evidence = {semantic_diff:{digest,entries:[]},source_diff:{digest,edits:[]},authoring_impact:{base_definition_hash:digest,entries:[],impact_digest:digest,required_capabilities:[],resulting_definition_hash:digest,semantic_diff_hash:digest,source_diff_hash:digest},diagnostics:[],affected_usages:[],affected_rows:[],affected_views:[],render_previews:[]};
const proposal = (status="proposed", generation=1) => ({id:"p1",generation,status,current_revision:revision,proposed_definition_hash:digest,proposed_graph_hash:digest,evidence,proposer:actor,access_evaluation_digest:digest,access_decision_digest:digest,required_capabilities:[],comments:[],created_at:"2026-01-01T00:00:00Z",updated_at:"2026-01-01T00:00:00Z"});

function client() {
  const calls=[]; let current=proposal();
  return {calls, value:{
    async snapshot(){calls.push("snapshot");return {version:1,proposals:[current]};},
    async comment(input){calls.push(["comment",input]);current={...current,generation:current.generation+1,comments:[{id:input.comment_id,body:input.body,target:input.target,author:actor,created_at:"",updated_at:"",stale:false,base_revision:revision.revision_id}]};return current;},
    async approveAndApply(input){calls.push(["approve",input]);current={...current,generation:current.generation+1,status:"applied",approved_by:actor,committed_revision:revision};return current;},
    async withdraw(input){calls.push(["withdraw",input]);current={...current,generation:current.generation+1,status:"withdrawn"};return current;},
  }};
}

test("model presents host-owned evidence and routes comment then approval with exact generation", async()=>{
  const host=client(), model=createReviewModel(host.value), states=[]; model.subscribe((state)=>states.push(state.phase));
  await model.refresh(); model.select("p1");
  await model.comment({comment_id:"c1",body:"Check this",target:{kind:"diff_entry",diff_key:"entry-1"}});
  assert.equal(model.snapshot().selected.comments.length,1);
  const applied=await model.approveAndApply(); assert.equal(applied.status,"applied");
  assert.deepEqual(host.calls[1][1],{proposal_id:"p1",generation:1,comment_id:"c1",body:"Check this",target:{kind:"diff_entry",diff_key:"entry-1"}});
  assert.deepEqual(host.calls[2][1],{proposal_id:"p1",generation:2});
  assert.ok(states.includes("loading")&&states.includes("mutating")&&states.includes("ready"));
});

test("terminal proposals cannot apply and close aborts late refresh",async()=>{
  const host=client(), model=createReviewModel(host.value); await model.refresh(); model.select("p1"); await model.withdraw();
  await assert.rejects(model.approveAndApply(),(error)=>error instanceof ReviewError && error.code==="review.invalid_state");
  await model.close(); assert.equal(model.snapshot().phase,"closed");
  await assert.rejects(model.refresh(),(error)=>error.code==="review.invalid_state");
});

test("transport failures remain typed and never fabricate state",async()=>{
  const model=createReviewModel({snapshot:async()=>{throw new Error("secret")}});
  await assert.rejects(model.refresh(),(error)=>error.code==="review.transport_failed"&&!error.message.includes("secret"));
  assert.equal(model.snapshot().phase,"failed");
});

test("invalid selection, typed failures, cancellation, and unsubscribe are fail closed",async()=>{
  assert.throws(()=>createReviewModel(),(error)=>error.code==="review.invalid_state");
  const host=client(), model=createReviewModel(host.value);
  await assert.rejects(model.comment({comment_id:"c",body:"x",target:{kind:"project"}}),(error)=>error.code==="review.invalid_state");
  const unsubscribe=model.subscribe(()=>{}); unsubscribe();
  await model.refresh(); model.select("missing"); assert.equal(model.snapshot().selected,undefined); model.select("p1");
  host.value.comment=async()=>{throw new ReviewError("review.conflict","changed")};
  await assert.rejects(model.comment({comment_id:"c",body:"x",target:{kind:"project"}}),(error)=>error.code==="review.conflict");
  assert.equal(model.snapshot().phase,"failed");
  await model.close(); await model.close(); model.subscribe(()=>assert.fail("closed subscriptions do not fire"));
  assert.throws(()=>model.select("p1"),(error)=>error.code==="review.invalid_state");
});

test("superseded refreshes map abort-like errors without leaking transport details",async()=>{
  let rejectFirst;
  const host=client();
  host.value.snapshot=({signal})=>new Promise((resolve,reject)=>{ rejectFirst=reject; signal.addEventListener("abort",()=>reject(Object.assign(new Error("private"),{name:"AbortError"}))); });
  const model=createReviewModel(host.value), first=model.refresh();
  const second=model.refresh();
  await assert.rejects(first,(error)=>error.code==="review.cancelled");
  rejectFirst(Object.assign(new Error("private"),{name:"AbortError"}));
  await assert.rejects(second,(error)=>error.code==="review.cancelled");
  assert.equal(model.snapshot().failure.code,"review.cancelled");
});
