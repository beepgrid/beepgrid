package engine

import "encoding/json"

// Version 12 permits a shared-body representation in private idempotency
// records. Public receipts and canonical messages keep their existing fields.
// The schema guard prevents version 11 binaries from reading body placeholders.
type storedSendReceipt struct {
	*SendResult
	SharedBody *string `json:"_beepgrid_body_v1,omitempty"`
}

func encodeReceipt(operation string, output any) []byte {
	if operation == "send" {
		if result, ok := output.(*SendResult); ok && len(result.Messages) > 1 {
			body := result.Messages[0].Body
			for _, m := range result.Messages {
				if m.Body != body {
					return JSON(output)
				}
			}
			copyResult := *result
			copyResult.Messages = append([]Message(nil), result.Messages...)
			for i := range copyResult.Messages {
				copyResult.Messages[i].Body = ""
			}
			return JSON(storedSendReceipt{SendResult: &copyResult, SharedBody: &body})
		}
	}
	return JSON(output)
}
func decodeReceipt(operation string, raw []byte, output any) error {
	if operation == "send" {
		if result, ok := output.(*SendResult); ok {
			receipt := storedSendReceipt{SendResult: result}
			if err := json.Unmarshal(raw, &receipt); err != nil {
				return err
			}
			if receipt.SharedBody != nil {
				for i := range result.Messages {
					result.Messages[i].Body = *receipt.SharedBody
				}
			}
			return nil
		}
	}
	return json.Unmarshal(raw, output)
}
