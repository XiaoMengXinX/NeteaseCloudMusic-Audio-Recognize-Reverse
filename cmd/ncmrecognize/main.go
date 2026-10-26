// Command ncmrecognize identifies a song file (wav, mp3, flac, ogg) via NetEase
// Cloud Music. By default it uses all available audio (adapted to the
// endpoint's supported lengths); -offset and -duration customise the window.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	ncm "github.com/XiaoMengXinX/NeteaseCloudMusic-Audio-Recognize-Reverse"
)

func main() {
	offset := flag.Float64("offset", 0, "start offset in seconds")
	duration := flag.Float64("duration", 0, "clip length in seconds; 0 = all available audio (capped at 16s)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-offset seconds] [-duration seconds] <audio.mp3|audio.wav|audio.flac|audio.ogg>\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	ctx := context.Background()
	data, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	a, err := ncm.Decode(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fp, info, err := ncm.FingerprintAdaptive(ctx, a.PCM, a.SampleRate, *offset, *duration)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "fingerprint: %.3fs audio, form duration %ds (%s)\n", info.Used, info.Duration, info.Action)
	resp, err := new(ncm.Client).MatchDuration(ctx, fp, info.Duration)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(string(resp.Raw))
}
