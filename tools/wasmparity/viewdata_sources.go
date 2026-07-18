// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

func viewDataDocumentSpecs() []viewDataDocumentSpec {
	return []viewDataDocumentSpec{
		{id: "structural", entry: "document.ldl", files: map[string][]byte{"document.ldl": []byte(viewDataStructuralSource)}},
		{id: "state", entry: "document.ldl", files: map[string][]byte{"document.ldl": []byte(viewDataStateSource)}},
		{id: "composed", entry: "document.ldl", files: map[string][]byte{"document.ldl": []byte(viewDataComposedSource)}},
		{id: "diff_before", entry: "document.ldl", files: map[string][]byte{"document.ldl": []byte(viewDataDiffBeforeSource)}},
		{id: "diff_after", entry: "document.ldl", files: map[string][]byte{"document.ldl": []byte(viewDataDiffAfterSource)}},
		{id: "deterministic", entry: "document.ldl", files: map[string][]byte{
			"document.ldl": []byte(viewDataDeterministicRootSource),
			"types.ldl":    []byte(viewDataDeterministicTypesSource),
		}},
	}
}

const viewDataStateSource = `project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, stg] required default prod
  }
}
entities service @app {
  alpha "Alpha"
}
rows service [environment] {
  alpha primary: prod
}
query prod_scope "Prod Scope" {
  select {
    layers [app]
    entity_types [service]
    roots [alpha]
  }
  traverse outgoing 0..0 visit_once
  result [seed_entities]
}
view state_optional "Optional state" inventory {
  state_input optional
  source query prod_scope {}
  table {
    rows entity_rows
    column entity_id {
      source field id
    }
    column updated_at {
      source state system.updated_at
    }
    sort updated_at descending nulls last
  }
}
view state_required "Required state" inventory {
  state_input required
  source query prod_scope {}
  table {
    rows entity_rows
    column entity_id {
      source field id
    }
    column updated_at {
      source state system.updated_at
    }
    sort updated_at descending nulls last
  }
}
`

const viewDataStructuralSource = `project p "Project" {}

layers {
  app "Application" @10
  data "Data" @20
}

entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, stg] required default prod
    capacity "Capacity" number min 0
  }
}

relation_type calls "Calls" data_flow {
  duplicate_policy allow
  from caller types [service] layers [app, data]
  to callee types [service] layers [app, data]
  label "calls"
  columns {
    protocol "Protocol" enum [http, grpc] required
  }
}

entities service @app {
  alpha "Alpha" {
    tags [critical]
  }
  beta "Beta"
}

entities service @data {
  gamma "Gamma"
}

rows service [environment, capacity] {
  alpha primary: prod, 75
  beta primary: prod, 25
  gamma primary: stg, 50
}

relations calls {
  alpha_beta: alpha -> beta
  beta_gamma: beta -> gamma
}

relation_rows calls [protocol] {
  alpha_beta primary: http
  beta_gamma primary: grpc
}

query prod_scope "Prod Scope" {
  parameters {
    environment enum [prod, stg] required default prod
  }
  select {
    layers [app, data]
    entity_types [service]
    relation_types [calls]
    roots [alpha]
  }
  where all {
    rows any types [service] {
      cell environment == $environment
    }
  }
  relation_where all {
    rows any types [calls] {
      cell protocol == http
    }
  }
  traverse outgoing 1..2 visit_once relations [calls]
  result [seed_entities, traversed_entities, path_relations, induced_relations]
}

view topology "Topology" topology {
  source query prod_scope { environment: prod }
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
  }
}

view table_automatic "Automatic relations" dependency {
  source query prod_scope { environment: prod }
  relation_projection calls {
    table {
      row_mode automatic
      include_from true
      include_to true
      include_relation_type true
    }
  }
  table {
    rows automatic_relations
  }
}

view table_relation "Relation owners" dependency {
  source query prod_scope { environment: prod }
  relation_projection calls {
    table {
      row_mode relation
      include_from true
      include_to true
      include_relation_type true
    }
  }
  table {
    rows automatic_relations
  }
}

view table_relation_rows "Relation rows" dependency {
  source query prod_scope { environment: prod }
  relation_projection calls {
    table {
      row_mode relation_rows
      include_from true
      include_to true
      include_relation_type true
    }
  }
  table {
    rows automatic_relations
  }
}

view matrix "Dependency matrix" dependency {
  source query prod_scope { environment: prod }
  relation_projection calls {
    matrix {
      row_endpoint from
      column_endpoint to
      include_relation_rows true
    }
  }
  matrix {
    row_axis {
      entity_types [service]
      label id
    }
    column_axis {
      entity_types [service]
      label display_name
    }
    cell {
      relation_types [calls]
      direction outgoing
      semantic relation_refs
      display attribute_summary
      attributes [protocol]
    }
  }
}

view tree "Hierarchy" hierarchy {
  source query prod_scope { environment: prod }
  relation_projection calls {
    tree {
      parent_endpoint from
      child_endpoint to
    }
  }
  tree {
    relation_types [calls]
    cycle_policy truncate
    shared_child_policy link
  }
}

view flow "Flow" flow {
  source query prod_scope { environment: prod }
  relation_projection calls {
    flow {
      source_endpoint from
      target_endpoint to
      connector_kind data
      branch_value_column protocol
    }
  }
  flow {
    relation_types [calls]
    lane_by attribute.environment
    cycle_policy include_cycle_ref
  }
}

view context "Context" context {
  source query prod_scope { environment: prod }
  relation_projection calls {
    context {
      fact_template "{from.display_name} calls {to.display_name}"
      reverse_fact_template "{to.display_name} is called by {from.display_name}"
      include_attribute_rows true
    }
  }
  context {
    group_by layer
    entity_rows
    relation_rows
    incoming
    outgoing
  }
}

view state_optional "Optional state" inventory {
  state_input optional
  source query prod_scope { environment: prod }
  table {
    rows entity_rows
    column entity_id {
      source field id
    }
    column updated_at {
      source state system.updated_at
    }
    sort updated_at descending nulls last
  }
}

view state_required "Required state" inventory {
  state_input required
  source query prod_scope { environment: prod }
  table {
    rows entity_rows
    column entity_id {
      source field id
    }
    column updated_at {
      source state system.updated_at
    }
    sort updated_at descending nulls last
  }
}
`

const viewDataComposedSource = `project p "Project" {}
layers {
  infra "Infrastructure" @10
  app "Application" @20
  security "Security" @30
}
entity_type group "Group" {
  representation container
  columns {
    note "Note" string
  }
}
entity_type node "Node" {
  representation shape rect
  columns {
    note "Note" string
  }
}
relation_type contains "Contains" containment {
  duplicate_policy allow
  from parent types [group] layers [infra]
  to child types [node] layers [app]
  label "contains"
  columns {
    note "Note" string
  }
  projection composed {
    mode nest
    parent_endpoint from
    child_endpoint to
    priority 1
    conflict diagnostic
    keep_edge false
  }
}
relation_type protects "Protects" security {
  duplicate_policy allow
  from control types [node] layers [security]
  to target types [node] layers [app]
  label "protects"
  columns {
    note "Note" string
  }
  projection composed {
    mode overlay
    overlay_endpoint from
    target_endpoint to
  }
}
relation_type marks "Marks" reference {
  duplicate_policy allow
  from badge types [node] layers [security]
  to target types [node] layers [app]
  label "marks"
  columns {
    note "Note" string
  }
  projection composed {
    mode badge
    badge_endpoint from
    target_endpoint to
  }
  render badge {
    label display_name
  }
}
relation_type links "Links" dependency {
  duplicate_policy allow
  from source types [node] layers [app]
  to target types [node] layers [app]
  label "links"
  columns {
    note "Note" string
  }
  projection composed {
    mode edge
  }
  projection diagram {
    mode edge
    source_endpoint to
    target_endpoint from
  }
}
relation_type conceals "Conceals" governance {
  duplicate_policy allow
  from source types [node] layers [app]
  to target types [group] layers [infra]
  label "conceals"
  columns {
    note "Note" string
  }
  projection composed {
    mode hide
  }
}
entities group @infra {
  root "Root"
}
entities node @app {
  child "Child"
  peer "Peer"
}
entities node @security {
  badge "Badge"
  shield "Shield"
}
rows group [note] {
  root primary: "root row"
}
rows node [note] {
  child primary: "child row"
  peer primary: "peer row"
  badge primary: "badge row"
  shield primary: "shield row"
}
relations contains {
  root_child: root -> child
}
relations protects {
  shield_child: shield -> child
}
relations marks {
  badge_child: badge -> child
}
relations links {
  child_peer: child -> peer
}
relations conceals {
  peer_root: peer -> root
}
relation_rows contains [note] {
  root_child primary: "contains row"
}
relation_rows protects [note] {
  shield_child primary: "protects row"
}
relation_rows marks [note] {
  badge_child primary: "marks row"
}
relation_rows links [note] {
  child_peer primary: "links row"
}
relation_rows conceals [note] {
  peer_root primary: "conceals row"
}
query scope "Scope" {
  select {
    layers [infra, app, security]
    entity_types [group, node]
    relation_types [contains, protects, marks, links, conceals]
    roots [root, child, peer, badge, shield]
  }
  result [seed_entities, induced_relations]
}
view composed "Composed" topology {
  source query scope {}
  relation_projection contains {
    composed {
      priority 20
      keep_edge true
    }
  }
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
    composed
    place root 10 20 300 200
  }
}
view hidden "Hidden edge" topology {
  source query scope {}
  relation_projection links {
    diagram {
      mode hide
    }
  }
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
  }
}
`

const viewDataDiffBeforeSource = `project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
relation_type depends "Depends" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "depends on"
}
entities service @app {
  alpha "Alpha"
  beta "Beta"
}
relations depends {
  alpha_beta: alpha -> beta
}
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [entity, relation]
  }
}
`

const viewDataDiffAfterSource = `project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
relation_type depends "Depends" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "depends on"
}
entities service @app {
  alpha "Alpha"
  beta "Beta updated"
  gamma "Gamma"
}
relations depends {
  alpha_beta: alpha -> beta
  beta_gamma: beta -> gamma
}
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [entity, relation]
  }
}
`

const viewDataDeterministicRootSource = `import { service } from "./types.ldl"
project p "Project" {}
layers {
  app "Application" @10
}
entities service @app {
  istanbul "İstanbul"
  angstrom "Ångström"
  eclair "Éclair"
}
query scope "Scope" {
  select {
    entity_types [service]
    roots [istanbul, angstrom, eclair]
  }
  result [seed_entities]
}
view context "Locale-independent context" context {
  source query scope {}
  context {
    group_by none
    incoming
    outgoing
  }
}
`

const viewDataDeterministicTypesSource = `entity_type service "Service" {
  representation shape rect
}
export { service }
`
