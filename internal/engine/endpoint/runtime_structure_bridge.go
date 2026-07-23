// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"errors"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

// BridgeStructure is the trusted read projection of the master document the
// Desktop Structure editor renders. It carries display data plus the stable
// addresses and document generation the frontend needs to compose semantic
// operations; it never exposes source text.
type BridgeStructure struct {
	DocumentGeneration engineprotocol.DocumentGeneration `json:"document_generation"`
	ProjectAddress     string                            `json:"project_address"`
	Layers             []BridgeLayer                     `json:"layers"`
	EntityTypes        []BridgeEntityType                `json:"entity_types"`
	RelationTypes      []BridgeRelationType              `json:"relation_types"`
	Entities           []BridgeEntity                    `json:"entities"`
	Relations          []BridgeRelation                  `json:"relations"`
}

type BridgeLayer struct {
	Address     string `json:"address"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Order       int64  `json:"order"`
}

type BridgeColumn struct {
	Address     string   `json:"address"`
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	ValueType   string   `json:"value_type"`
	EnumValues  []string `json:"enum_values"`
	Required    bool     `json:"required"`
}

type BridgeEntityType struct {
	Address     string         `json:"address"`
	ID          string         `json:"id"`
	DisplayName string         `json:"display_name"`
	Columns     []BridgeColumn `json:"columns"`
}

type BridgeRelationType struct {
	Address         string         `json:"address"`
	ID              string         `json:"id"`
	DisplayName     string         `json:"display_name"`
	ForwardLabel    string         `json:"forward_label"`
	FromEntityTypes []string       `json:"from_entity_types"`
	ToEntityTypes   []string       `json:"to_entity_types"`
	Columns         []BridgeColumn `json:"columns"`
}

type BridgeCell struct {
	ColumnAddress string `json:"column_address"`
	// Value is the canonical scalar rendered as a display string; Kind keeps
	// the scalar type so the frontend can pick the right editing control.
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type BridgeAttributeRow struct {
	ID      string       `json:"id"`
	Address string       `json:"address"`
	Values  []BridgeCell `json:"values"`
}

type BridgeEntity struct {
	Address      string               `json:"address"`
	ID           string               `json:"id"`
	DisplayName  string               `json:"display_name"`
	TypeAddress  string               `json:"type_address"`
	LayerAddress string               `json:"layer_address"`
	Tags         []string             `json:"tags"`
	Rows         []BridgeAttributeRow `json:"rows"`
}

type BridgeRelation struct {
	Address     string  `json:"address"`
	ID          string  `json:"id"`
	DisplayName *string `json:"display_name,omitempty"`
	TypeAddress string  `json:"type_address"`
	FromAddress string  `json:"from_address"`
	ToAddress   string  `json:"to_address"`
	CrossLayer  bool    `json:"cross_layer"`
}

func (w *RuntimeEngineBridge) Structure(working BridgeWorking) (BridgeStructure, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.working != working {
		return BridgeStructure{}, errors.New("stale working document")
	}
	ast := doc.snapshot.TypedAST
	result := BridgeStructure{
		DocumentGeneration: engineprotocol.DocumentGeneration{
			DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: w.endpoint, Value: working.Handle},
			Value:          protocolcommon.CanonicalUint64(working.Generation),
		},
		Layers:        []BridgeLayer{},
		EntityTypes:   []BridgeEntityType{},
		RelationTypes: []BridgeRelationType{},
		Entities:      []BridgeEntity{},
		Relations:     []BridgeRelation{},
	}
	if ast.Project != nil {
		result.ProjectAddress = ast.Project.Address
	}
	for _, layer := range ast.Layers {
		result.Layers = append(result.Layers, BridgeLayer{Address: layer.Address, ID: layer.ID, DisplayName: layer.DisplayName, Order: layer.Order})
	}
	for _, entityType := range ast.EntityTypes {
		mapped := BridgeEntityType{Address: entityType.Address, ID: entityType.ID, DisplayName: entityType.DisplayName, Columns: []BridgeColumn{}}
		for _, column := range entityType.Columns {
			mapped.Columns = append(mapped.Columns, BridgeColumn{Address: column.Address, ID: column.ID, DisplayName: column.DisplayName, ValueType: string(column.ValueType), EnumValues: append([]string{}, column.EnumValues...), Required: column.Required})
		}
		result.EntityTypes = append(result.EntityTypes, mapped)
	}
	for _, relationType := range ast.RelationTypes {
		mapped := BridgeRelationType{
			Address: relationType.Address, ID: relationType.ID, DisplayName: relationType.DisplayName, ForwardLabel: relationType.ForwardLabel,
			FromEntityTypes: append([]string{}, relationType.From.EntityTypeAddresses...),
			ToEntityTypes:   append([]string{}, relationType.To.EntityTypeAddresses...),
			Columns:         []BridgeColumn{},
		}
		for _, column := range relationType.Columns {
			mapped.Columns = append(mapped.Columns, BridgeColumn{Address: column.Address, ID: column.ID, DisplayName: column.DisplayName, ValueType: string(column.ValueType), EnumValues: append([]string{}, column.EnumValues...), Required: column.Required})
		}
		result.RelationTypes = append(result.RelationTypes, mapped)
	}
	if ast.Graph != nil {
		for _, entity := range ast.Graph.Entities {
			mapped := BridgeEntity{
				Address: entity.Address, ID: entity.ID, DisplayName: entity.DisplayName,
				TypeAddress: entity.TypeAddress, LayerAddress: entity.LayerAddress,
				Tags: append([]string{}, entity.Tags...), Rows: []BridgeAttributeRow{},
			}
			for _, row := range entity.Rows {
				mappedRow := BridgeAttributeRow{ID: row.ID, Address: row.Address, Values: []BridgeCell{}}
				for _, cell := range row.Values {
					mappedRow.Values = append(mappedRow.Values, BridgeCell{ColumnAddress: cell.ColumnAddress, Kind: string(cell.Value.Type), Value: scalarDisplay(cell.Value)})
				}
				mapped.Rows = append(mapped.Rows, mappedRow)
			}
			result.Entities = append(result.Entities, mapped)
		}
		for _, relation := range ast.Graph.Relations {
			result.Relations = append(result.Relations, BridgeRelation{
				Address: relation.Address, ID: relation.ID, DisplayName: relation.DisplayName,
				TypeAddress: relation.TypeAddress, FromAddress: relation.FromAddress, ToAddress: relation.ToAddress, CrossLayer: relation.CrossLayer,
			})
		}
	}
	return result, nil
}

func scalarDisplay(value engine.TypedScalar) string {
	switch string(value.Type) {
	case "integer":
		return strconv.FormatInt(value.Int, 10)
	case "number":
		return strconv.FormatFloat(value.Float, 'g', -1, 64)
	case "boolean":
		return strconv.FormatBool(value.Bool)
	default:
		return value.String
	}
}
