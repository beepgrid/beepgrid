package engine

import (
	"fmt"
	"strings"
	"testing"
)

func TestJSONDigestPreservesReceiptsCursorsAndEscaping(t *testing.T) {
	for _, value := range []any{nil, "", "<script>\n\"\\😀", map[string]any{"b": []any{1, nil, true}, "a": "<&>"},
		SendInput{Target: Target{"agent", "builder-7"}, Body: strings.Repeat("large <&> message\n", 2048)},
		QueryInput{Collection: "messages", Limit: 12, Filters: map[string]string{"conversation_id": "con_1"}}} {
		if got, want := digestJSON(value), Digest(string(JSON(value))); got != want {
			t.Fatal("canonical digest changed", got, want)
		}
	}
}

func BenchmarkJSONDigest(b *testing.B) {
	for _, size := range []int{1024, 8192, 32768} {
		in := SendInput{Target: Target{"agent", "builder-7"}, Body: strings.Repeat("x", size)}
		want := Digest(string(JSON(in)))
		for _, direct := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/direct=%t", size, direct), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					var got string
					if direct {
						got = digestJSON(in)
					} else {
						got = Digest(string(JSON(in)))
					}
					if got != want {
						b.Fatal("digest changed")
					}
				}
			})
		}
	}
}
