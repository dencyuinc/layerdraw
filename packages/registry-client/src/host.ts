// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type {
  RegistryArtifactRelease, RegistryAuthConnectionInput, RegistryAuthoringInput,
  RegistryClient, RegistryCommitInput, RegistryCommitResult, RegistryFailure, RegistryInstallPlan, RegistryPlanInput,
  RegistryResult, RegistrySearchInput, RegistrySource, RegistryTransaction,
} from "./index.js";

const wireVersion = "1.0";
const operations = Object.freeze({sources:"registry.list_sources",configure:"registry.configure_source",connect:"registry.connect_source",disconnect:"registry.disconnect_source",search:"registry.search",plan:"registry.plan_install",commit:"registry.commit_plan",transaction:"registry.get_transaction",recover:"registry.recover_transaction",author:"registry.author_artifact"} as const);
type RegistryOperation = typeof operations[keyof typeof operations];
interface WireRequest { readonly wire_version:"1.0";readonly operation:RegistryOperation;readonly request_id:string;readonly input:unknown }
export interface RegistryHostBinding { invoke(request: WireRequest, signal?:AbortSignal):Promise<unknown> }

function isRecord(value:unknown):value is Record<string,unknown>{return typeof value==="object"&&value!==null&&!Array.isArray(value)}
function hasExactKeys(value:Record<string,unknown>,required:readonly string[],optional:readonly string[]=[]):boolean{const allowed=new Set([...required,...optional]);return required.every((key)=>Object.hasOwn(value,key))&&Object.keys(value).every((key)=>allowed.has(key))}
function parseResponse(value:unknown):unknown{if(typeof value==="string"){try{return JSON.parse(value)}catch{return undefined}}return value}
function validValue(operation:RegistryOperation,value:unknown):boolean{
  if(operation===operations.sources||operation===operations.search)return Array.isArray(value)&&value.every(isRecord);
  if(operation===operations.configure||operation===operations.connect||operation===operations.disconnect||operation===operations.author)return isRecord(value);
  if(operation===operations.plan)return isRecord(value)&&typeof value.transaction_id==="string"&&typeof value.plan_digest==="string"&&Array.isArray(value.host_operation_impacts)&&isRecord(value.access_decision);
  if(operation===operations.commit)return isRecord(value)&&typeof value.committed_revision==="string"&&typeof value.operation_result_id==="string";
  return isRecord(value)&&isRecord(value.plan)&&Array.isArray(value.events);
}
function hostResult<T>(operation:RegistryOperation,requestId:string,raw:unknown):RegistryResult<T>{
  const value=parseResponse(raw);if(!isRecord(value)||value.wire_version!==wireVersion||value.operation!==operation||value.request_id!==requestId||typeof value.ok!=="boolean")return {ok:false,failure:{code:"registry.unavailable",subject:"invalid_host_response",actionable:true}};
  if(value.ok===true){if(!hasExactKeys(value,["wire_version","operation","request_id","ok","value"])||!validValue(operation,value.value))return {ok:false,failure:{code:"registry.unavailable",subject:"invalid_host_response",actionable:true}};return {ok:true,value:structuredClone(value.value) as T}}
  if(!hasExactKeys(value,["wire_version","operation","request_id","ok","failure"])||!isRecord(value.failure)||!hasExactKeys(value.failure,["code","subject","actionable"])||typeof value.failure.code!=="string"||typeof value.failure.subject!=="string"||typeof value.failure.actionable!=="boolean")return {ok:false,failure:{code:"registry.unavailable",subject:"invalid_host_response",actionable:true}};
  const failure:RegistryFailure={code:value.failure.code,subject:value.failure.subject,actionable:value.failure.actionable};return {ok:false,failure};
}

export function createHostRegistryClient(binding:RegistryHostBinding):RegistryClient{
  if(!binding||typeof binding.invoke!=="function")throw new TypeError("registry host binding is required");let sequence=0;
  const call=async<T>(operation:RegistryOperation,input:unknown,signal?:AbortSignal):Promise<RegistryResult<T>>=>{if(signal?.aborted)return {ok:false,failure:{code:"registry.cancelled",subject:operation,actionable:true}};const requestId=`registry-${++sequence}`;const request:WireRequest={wire_version:wireVersion,operation,request_id:requestId,input:structuredClone(input)};try{return hostResult<T>(operation,requestId,await binding.invoke(request,signal))}catch{return {ok:false,failure:{code:"registry.unavailable",subject:operation,actionable:true}}}};
  return Object.freeze({
    listSources:(signal?:AbortSignal)=>call<readonly RegistrySource[]>(operations.sources,{},signal),
    configureSource:(source:Omit<RegistrySource,"connected">,signal?:AbortSignal)=>call<RegistrySource>(operations.configure,{source:{...source,connected:false}},signal),
    connectSource:(input:RegistryAuthConnectionInput,signal?:AbortSignal)=>call<RegistrySource>(operations.connect,input,signal),
    disconnectSource:(sourceId:string,signal?:AbortSignal)=>call<RegistrySource>(operations.disconnect,{source_id:sourceId},signal),
    search:(input:RegistrySearchInput,signal?:AbortSignal)=>call<readonly RegistryArtifactRelease[]>(operations.search,input,signal),
    plan:(input:RegistryPlanInput,signal?:AbortSignal)=>call<RegistryInstallPlan>(operations.plan,input,signal),
    commit:(input:RegistryCommitInput,signal?:AbortSignal)=>call<RegistryCommitResult>(operations.commit,input,signal),
    getTransaction:(transactionId:string,signal?:AbortSignal)=>call<RegistryTransaction>(operations.transaction,{transaction_id:transactionId},signal),
    recoverTransaction:(transactionId:string,signal?:AbortSignal)=>call<RegistryTransaction>(operations.recover,{transaction_id:transactionId},signal),
    authorArtifact:(input:RegistryAuthoringInput,signal?:AbortSignal)=>call<RegistryArtifactRelease>(operations.author,input,signal),
  });
}
