# Populated product review

The review workspace is local test data, isolated from hosted customers and provider accounts. It uses the real PostgreSQL engine, authorization, durable messages, worker leases and acknowledgements. Worker replies are deterministic fixtures. They do not qualify an AI provider, Slack, SMTP or Telegram.

## Start

From the repository root:

```sh
docker compose up -d --wait postgres
go build -buildvcs=false -trimpath -o .local/beepgrid-product ./cmd/beepgrid
mkdir -p .local/product-review
```

Run the engine from `.local/product-review` in its own terminal:

```sh
BEEPGRID_SCHEMA=product_review_20261030 BEEPGRID_WORKSPACE=product-review \
BEEPGRID_ENVIRONMENT=review BEEPGRID_COMPANY='Beepgrid Lab' \
BEEPGRID_ADDR=127.0.0.1:4010 BEEPGRID_PUBLIC_URL=http://127.0.0.1:4040 \
../beepgrid-product serve --dev
```

From the repository root, populate and run the controlled workers:

```sh
python3 scripts/review-workspace.py seed
python3 scripts/review-workspace.py workers
```

Run `npm run dev` from `workspace/`. To sign in without copying a credential into a URL or chat, run `python3 scripts/review-workspace.py open`, then open its loopback link once. Use `--as-person 'Maya Chen'` or `--as-person 'Sam Patel'` to review a member or a restricted member. The sign-in helper is loopback-only and stops after one request or 60 seconds.

The tool refuses non-loopback origins, unexpected workspace names/environments and mismatched saved credentials. Private fixture state is mode 0600 inside the ignored, mode-0700 directory. Do not copy it to another environment or commit it. The step journal avoids duplicating messages on repeated seeding; the regular configuration APIs retain their existing concurrency and unknown-result behavior.

## Review journeys

- Twelve numbered agents in Builders, Reviewers, DevOps and Billing; Engineering and Support are separate people teams.
- Builder 1 has two runtime connections. Billing 3 receives no heartbeats and becomes offline according to the engine's normal presence window.
- V2 checkout rollout is a shared chat with a stable structured mention and responses from individual specialists.
- Builder 1 and Billing 2 exchange payment-reconciliation context and a private PDF fixture. Agent conversations stay out of an unrelated person's Messages.
- DevOps 2 waits for the owner’s approval; Billing 1 waits for Maya’s approval.
- Three informational questions share an exact context key. Each keeps its own canonical request and audience.
- Builder 3, Reviewer 3 and DevOps 3 declare a three-agent blocking cycle. Receipt acknowledgement does not resolve it. Open the relevant conversation and answer a blocking question to clear the cycle.
- New messages from a Person to an active worker get a controlled reply. The workers never auto-approve or invent execution success.
- Maya is an ordinary permitted collaborator. Sam can only see the owner and Builder 1, so permission and history leaks can be checked.

Use `python3 scripts/review-workspace.py inspect` for safe aggregate counts. Stop the fixture worker and engine terminals with Ctrl-C when finished. Preserve shared PostgreSQL while another task is using it.
