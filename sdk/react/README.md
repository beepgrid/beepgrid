# Beep Grid React components

This package provides `NotifRequest`, `NotifConversation`, `useNotifRequest` and `browserClient`. It uses the same authorization and durable request API as the employee app. Polling is cancelled when a component unmounts; this never cancels the request.

```tsx
import { useMemo } from 'react';
import { browserClient, NotifRequest, NotifConversation } from '@notif/react';
import '@notif/react/styles.css';

export function Decision({ requestId }: { requestId: string }) {
  const client = useMemo(() => browserClient(), []);
  return <NotifRequest client={client} id={requestId}
    onConversation={id => location.assign('/conversations/' + id)} />;
}
```

Host behind the same origin as Beep Grid, or proxy `/api/v1` through your application. Authenticate a person using Beep Grid's HttpOnly session and enforce its roles on the server. Owner tokens, launcher tokens and provider secrets belong only on trusted servers. An unrelated application's login cookie is not automatically a Beep Grid identity. OIDC or a trusted identity integration must establish it first.

Package names are development placeholders until the public publishing namespace is selected. For local development, build `../typescript` then run `npm install`, `npm run build` and `npm test` in this package. The supplied CSS is optional and every interaction uses semantic form controls.
