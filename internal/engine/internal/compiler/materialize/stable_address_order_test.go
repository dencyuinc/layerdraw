// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestPreparedStableAddressOrderPreservesStructuredOrder(t *testing.T) {
	want := []string{
		"ldl:project:p",
		"ldl:project:p:entity-type:z",
		"ldl:project:p:entity:a",
		"ldl:project:p:entity:b",
		"ldl:project:p:reference:a",
		"ldl:project:p:entity-type:z:column:a",
		"ldl:project:p:entity:a:row:a",
		"ldl:pack:a:a",
		"ldl:pack:a:a:entity-type:a",
	}
	resolved := resolve.Result{}
	for _, address := range want[1:4] {
		symbol, _ := stableSymbolFromAddress(address)
		resolved.Candidates = append(resolved.Candidates, resolve.DeclarationSymbol{Address: address, Symbol: symbol})
	}
	order := NewStableAddressOrder(resolved)
	got := append([]string(nil), want...)
	for i, j := 0, len(got)-1; i < j; i, j = i+1, j-1 {
		got[i], got[j] = got[j], got[i]
	}
	order.Sort(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order=%v, want %v", got, want)
	}
	if !order.Less("invalid-a", "invalid-b") || order.Less("invalid-b", "invalid-a") {
		t.Fatal("invalid-address lexical fallback changed")
	}
	// Effective declarations must still override candidate symbols, and lookups
	// must use resolved symbols rather than reinterpreting the address string.
	resolved.Declarations = []resolve.DeclarationSymbol{{Address: want[2], Symbol: resolved.Candidates[2].Symbol}}
	if overridden := NewStableAddressOrder(resolved); overridden.Less(want[2], want[3]) || overridden.Less(want[3], want[2]) {
		t.Fatal("effective declaration did not override candidate")
	}
}

func TestPreparedStableAddressComparisonDoesNotAllocate(t *testing.T) {
	resolved, addresses := stableAddressSortFixture(5000)
	order := NewStableAddressOrder(resolved)
	if allocations := testing.AllocsPerRun(100, func() { order.Less(addresses[0], addresses[1]) }); allocations != 0 {
		t.Fatalf("prepared comparison allocates %v times; do not rebuild the declaration index in a comparator", allocations)
	}
}

func BenchmarkStableAddressSort(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			resolved, addresses := stableAddressSortFixture(size)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				values := append([]string(nil), addresses...)
				NewStableAddressOrder(resolved).Sort(values)
			}
		})
	}
}

func stableAddressSortFixture(size int) (resolve.Result, []string) {
	resolved := resolve.Result{}
	addresses := make([]string, size)
	for i := range size {
		address := fmt.Sprintf("ldl:project:p:entity:n_%06d", size-i)
		symbol, _ := stableSymbolFromAddress(address)
		resolved.Declarations = append(resolved.Declarations, resolve.DeclarationSymbol{Address: address, Symbol: symbol})
		addresses[i] = address
	}
	return resolved, addresses
}
