package engine

import (
	"strings"
	"testing"
	"time"
)

func TestPrivateReceiptCodecPreservesPublicFieldsAndLegacyRecords(t *testing.T) {
	for _, count := range []int{0, 1, 32} {
		original := SendResult{ConversationID: "con", Messages: []Message{}}
		for i := 0; i < count; i++ {
			original.Messages = append(original.Messages, Message{ID: ID("msg"), ConversationID: "con", SenderID: "sender", InstanceID: "instance", Body: strings.Repeat("<é>\"\n", 1000), CreatedAt: time.Unix(1700000000, int64(i+1))})
		}
		public := string(JSON(original))
		for _, stored := range [][]byte{JSON(original), encodeReceipt("send", &original)} {
			var decoded SendResult
			if err := decodeReceipt("send", stored, &decoded); err != nil || string(JSON(decoded)) != public {
				t.Fatal("changed receipt", count, err)
			}
		}
		if string(JSON(original)) != public {
			t.Fatal("encoder mutated caller")
		}
	}
	// The generic idempotency helper retains heterogeneous outputs unchanged.
	heterogeneous := SendResult{Messages: []Message{{Body: "one"}, {Body: "two"}}}
	var got SendResult
	if err := decodeReceipt("send", encodeReceipt("send", &heterogeneous), &got); err != nil || string(JSON(got)) != string(JSON(heterogeneous)) {
		t.Fatal("heterogeneous fallback", err)
	}
}
