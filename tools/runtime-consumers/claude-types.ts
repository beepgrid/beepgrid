// Published SDK types, no model call or credentials. The binding stays optional
// and the application's existing query/input queue remains the owner.
import type { SDKUserMessage } from '@anthropic-ai/claude-agent-sdk';
import { claudeSDKBinding } from '../../sdk/typescript/dist/runtime/bindings.js';
claudeSDKBinding({
  route: async (conversation) => conversation,
  reconcile: async () => 'unknown',
  append: async (_session, message, _id, _mode) => {
    const accepted: SDKUserMessage = message;
    void accepted;
  },
});
