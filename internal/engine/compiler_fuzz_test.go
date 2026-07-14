// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"reflect"
	"testing"
)

func FuzzCompileFacadeSmoke(f *testing.F) {
	f.Add([]byte(`project p "Project" {}`))
	f.Add([]byte("project"))
	f.Add([]byte{0xff, 0x00, '\n'})
	f.Fuzz(func(t *testing.T, source []byte) {
		if len(source) > 32<<10 {
			t.Skip()
		}
		input := projectCompileInput(string(source))
		first, firstErr := New(BuildInfo{}).Compile(context.Background(), input)
		second, secondErr := New(BuildInfo{}).Compile(context.Background(), input)
		if firstErr != nil || secondErr != nil {
			t.Fatalf("bounded in-memory source returned infrastructure error: first=%v second=%v", firstErr, secondErr)
		}
		if !reflect.DeepEqual(first.Snapshot(), second.Snapshot()) {
			t.Fatal("facade output is not deterministic")
		}
	})
}
