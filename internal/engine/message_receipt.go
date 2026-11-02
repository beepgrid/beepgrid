package engine

// Read-only evidence from existing native acknowledgements. This expression
// uses the delivery operation index and runs inside the bounded history read,
// never on the send path. Only the sender sees receipts, and only for current
// Agent participants. Provider acceptance is deliberately excluded.
const messageReceivedSQL = `CASE WHEN m.sender_id=$3 THEN ARRAY(
 SELECT DISTINCT d.recipient_id FROM deliveries d
 JOIN principals p ON p.scope=d.scope AND p.id=d.recipient_id AND p.kind='agent'
 JOIN participants cp ON cp.scope=d.scope AND cp.conversation_id=m.conversation_id AND cp.principal_id=d.recipient_id AND cp.left_at IS NULL
 WHERE d.scope=m.scope AND d.operation_id=m.id AND d.connection_id='' AND d.state='received'
 ORDER BY d.recipient_id
) ELSE ARRAY[]::text[] END`
