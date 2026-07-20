// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import TestRenderer, {act} from "react-test-renderer";
import {createReviewModel} from "@layerdraw/review";
import {ReviewPanel} from "../dist/review.js";

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const digest=`sha256:${"a".repeat(64)}`;
const revision={document_id:"doc",revision_id:"revision_abcdefghijklmnop",definition_hash:digest,graph_hash:digest};
const proposal={id:"proposal",generation:1,status:"proposed",current_revision:revision,proposed_definition_hash:digest,proposed_graph_hash:digest,
  proposer:{actor_id:"agent-one",kind:"agent"},access_evaluation_digest:digest,access_decision_digest:digest,required_capabilities:["graph:write"],comments:[],created_at:"",updated_at:"",
  evidence:{semantic_diff:{digest,entries:[]},source_diff:{digest,edits:[]},authoring_impact:{base_definition_hash:digest,entries:[],impact_digest:digest,required_capabilities:["graph:write"],resulting_definition_hash:digest,semantic_diff_hash:digest,source_diff_hash:digest},diagnostics:[{code:"LDL1000"}],affected_usages:[],affected_rows:[],affected_views:[],render_previews:[{kind:"render",label:"Canvas",digest,media_type:"image/png"}]}};

test("ReviewPanel inspects canonical evidence, comments, and approves through the model",async()=>{
  const calls=[];
  const client={
    async snapshot(){return {version:1,proposals:[proposal]};},
    async comment(input){calls.push(["comment",input]);return {...proposal,generation:2,comments:[{id:input.comment_id,author:proposal.proposer,body:input.body,target:input.target,created_at:"",updated_at:"",stale:false,base_revision:revision.revision_id}]};},
    async approveAndApply(input){calls.push(["approve",input]);return {...proposal,generation:3,status:"applied"};},
    async withdraw(input){calls.push(["withdraw",input]);return {...proposal,generation:2,status:"withdrawn"};},
  };
  const model=createReviewModel(client); let renderer;
  await act(async()=>{renderer=TestRenderer.create(React.createElement(ReviewPanel,{model})); await Promise.resolve();});
  await act(async()=>renderer.root.findAllByType("button").find((node)=>node.children.join("").includes("proposal — proposed")).props.onClick());
  assert.ok(renderer.root.findAllByType("h3").some((node)=>node.children.join("")==="Semantic impact"));
  const textarea=renderer.root.findByType("textarea");
  await act(async()=>textarea.props.onChange({currentTarget:{value:"Looks correct"}}));
  await act(async()=>{renderer.root.findAllByType("button").find((node)=>node.children.join("")==="Add comment").props.onClick(); await Promise.resolve();});
  await act(async()=>{renderer.root.findAllByType("button").find((node)=>node.children.join("")==="Approve and apply").props.onClick(); await Promise.resolve();});
  assert.equal(calls[0][0],"comment"); assert.equal(calls[1][0],"approve");
  assert.equal(model.snapshot().selected.status,"applied");
  await act(async()=>renderer.unmount()); await model.close();
});
