// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"reflect"
	"strings"
	"testing"
)

func TestPreparedSelectionMatchesSymbolAndBindingScans(t *testing.T) {
	r := &resolver{
		input: baseInput(), packs: map[string]packInfo{}, aliases: map[string]string{},
		modules: map[ModuleKey]*moduleState{}, visiting: map[ModuleKey]bool{},
		symbols: map[string]DeclarationSymbol{}, selected: map[string]bool{},
	}
	r.resolve()
	if len(r.diagnostics) != 0 || len(r.childrenByOwner) == 0 {
		t.Fatalf("fixture failed: %v", r.diagnostics)
	}
	for _, owner := range r.symbols {
		var wantChildren []DeclarationSymbol
		// The pre-index implementation selected direct non-root children by
		// structured depth and address prefix, including imported declarations.
		if len(owner.Symbol.Path) != 0 {
			for address, decl := range r.symbols {
				if strings.HasPrefix(address, owner.Address+":") && len(decl.Symbol.Path) == len(owner.Symbol.Path)+1 {
					wantChildren = append(wantChildren, decl)
				}
			}
		}
		sortDeclarations(wantChildren)
		if got := r.childrenByOwner[owner.Address]; !reflect.DeepEqual(got, wantChildren) {
			t.Fatalf("children for %s changed: got %v want %v", owner.Address, got, wantChildren)
		}
		var wantBindings []SourceBinding
		if module := r.modules[owner.Module]; module != nil {
			for _, binding := range module.bindings {
				if binding.Module == owner.Module && binding.SourceAddress == owner.Address {
					wantBindings = append(wantBindings, binding)
				}
			}
		}
		if got := r.bindingsBySource[owner.Module][owner.Address]; !reflect.DeepEqual(got, wantBindings) {
			t.Fatalf("bindings for %s changed: got %v want %v", owner.Address, got, wantBindings)
		}
	}
}
