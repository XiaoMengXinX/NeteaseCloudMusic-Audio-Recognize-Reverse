//go:build oracle

package ncmrecognize

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/tetratelabs/wazero"
)

func wasmSpectrum(t *testing.T, pcm []float32) ([]float32, int) {
	t.Helper()
	ctx := context.Background()
	patched, err := patchExports(afpWasm, map[string]uint32{"x_ctor": 320, "x_feed": 307})
	if err != nil {
		t.Fatal(err)
	}
	e := &wasmEngine{methods: map[string]uint32{}, funcs: map[string]uint32{}}
	e.rt = wazero.NewRuntime(ctx)
	t.Cleanup(func() { e.rt.Close(ctx) })
	if err := e.defineImports(ctx); err != nil {
		t.Fatal(err)
	}
	e.mod, err = e.rt.InstantiateWithConfig(ctx, patched, wazero.NewModuleConfig().WithName("afp"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.ExportedFunction("C").Call(ctx); err != nil {
		t.Fatal(err)
	}
	mem := e.mod.Memory()
	call := func(n string, args ...uint64) uint64 {
		r, er := e.mod.ExportedFunction(n).Call(ctx, args...)
		if er != nil {
			t.Fatal(er)
		}
		if len(r) == 0 {
			return 0
		}
		return uint64(uint32(r[0]))
	}
	client := uint32(call("E", 256))
	for i := uint32(0); i < 256; i++ {
		mem.WriteByte(client+i, 0)
	}
	call("x_ctor", uint64(client))
	p := uint32(call("E", uint64(len(pcm)*4)))
	buf := make([]byte, len(pcm)*4)
	for i, v := range pcm {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	mem.Write(p, buf)
	call("x_feed", uint64(client), uint64(p), uint64(len(pcm)))
	A := peek32(mem, client+44)
	begin, end := peek32(mem, A+64), peek32(mem, A+68)
	n := int((end - begin) / 4)
	raw, _ := mem.Read(begin, uint32(n*4))
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out, n / specBins
}

func TestSpecMatch100(t *testing.T) {
	const n = 48000
	pcm := make([]float32, n)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 100 * float64(i) / 8000))
	}
	want, wf := wasmSpectrum(t, pcm)
	got, gf, err := spectrogram(pcm)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("frames", wf, gf, "len", len(want), len(got))
	bad := 0
	for i := range got {
		if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
			bad++
			if bad <= 8 {
				fmt.Printf("idx %d (f=%d b=%d) got %g(%#x) want %g(%#x)\n", i, i/specBins, i%specBins, got[i], math.Float32bits(got[i]), want[i], math.Float32bits(want[i]))
			}
		}
	}
	fmt.Println("spectrum mismatches", bad)
}

func peek32(m interface {
	Read(uint32, uint32) ([]byte, bool)
}, addr uint32) uint32 {
	b, ok := m.Read(addr, 4)
	if !ok {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}
