//go:build oracle

package ncmrecognize

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestLogfMatch(t *testing.T) {
	ctx := context.Background()
	patched, _ := patchExports(afpWasm, map[string]uint32{"x_log": 82})
	e := &wasmEngine{methods: map[string]uint32{}, funcs: map[string]uint32{}}
	e.rt = wazero.NewRuntime(ctx)
	defer e.rt.Close(ctx)
	if err := e.defineImports(ctx); err != nil {
		t.Fatal(err)
	}
	var err error
	e.mod, err = e.rt.InstantiateWithConfig(ctx, patched, wazero.NewModuleConfig().WithName("afp"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.ExportedFunction("C").Call(ctx); err != nil {
		t.Fatal(err)
	}
	logw := e.mod.ExportedFunction("x_log")
	rnd := rand.New(rand.NewSource(1))
	bad := 0
	for i := 0; i < 200000; i++ {
		var x float32
		switch i % 4 {
		case 0:
			x = rnd.Float32() * 100
		case 1:
			x = float32(math.Exp(rnd.Float64()*20 - 10))
		case 2:
			x = float32(rnd.Float64()) // subnormal-ish/small
		case 3:
			x = float32(rnd.Float64() * 1e8)
		}
		if x <= 0 || math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			continue
		}
		r, er := logw.Call(ctx, uint64(math.Float32bits(x)))
		if er != nil {
			t.Fatal(er)
		}
		got := math.Float32frombits(uint32(r[0]))
		want := logf32(x)
		if math.Float32bits(got) != math.Float32bits(want) {
			bad++
			if bad <= 10 {
				fmt.Printf("x=%g(%#x) wasm=%g(%#x) go=%g(%#x)\n", x, math.Float32bits(x), got, math.Float32bits(got), want, math.Float32bits(want))
			}
		}
	}
	fmt.Println("logf mismatches", bad)
	if bad != 0 {
		t.Fatal("logf mismatch")
	}
}
