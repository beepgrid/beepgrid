# Beepgrid for React

[Website](https://beepgrid.com) · [Documentation](https://beepgrid.com/docs/sdks/react/) · [Source](https://github.com/beepgrid/beepgrid/tree/main/sdk/react)

```sh
npm install @beepgrid/react @beepgrid/sdk react
```

React 19 or later. Optional components for embedding Beepgrid in your own application.

This package provides `NotifRequest`, `NotifConversation`, `useNotifRequest` and `browserClient`. It uses the same authorization and durable request API as the employee app. Polling is cancelled when a component unmounts; this never cancels the request.

```tsx
import { useMemo } from 'react';
import { browserClient, NotifRequest, NotifConversation } from '@beepgrid/react';
import '@beepgrid/react/styles.css';

export function Decision({ requestId }: { requestId: string }) {
  const client = useMemo(() => browserClient(), []);
  return <NotifRequest client={client} id={requestId}
    onConversation={id => location.assign('/conversations/' + id)} />;
}
```

Host behind the same origin as Beepgrid, or proxy `/api/v1` through your application. Authenticate a person using Beepgrid's HttpOnly session and enforce its roles on the server. Owner tokens, launcher tokens and provider secrets belong only on trusted servers. An unrelated application's login cookie is not automatically a Beepgrid identity. OIDC or a trusted identity integration must establish it first.

For local development, build `../typescript` then run `npm install`, `npm run build` and `npm test` in this package. The supplied CSS is optional and every interaction uses semantic form controls.
