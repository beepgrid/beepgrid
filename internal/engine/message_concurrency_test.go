package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestIndependentMessageTransactionsOverlapAndFencePolicyWriter(t *testing.T) {
	s, owner := testStore(t)
	worker := testWorker(t, s, owner, "parallel-policy")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	results := make(chan error, 2)
	for i := range 2 {
		go func() {
			// Independent admission gates model distinct serving replicas.
			replica := &Store{Pool: s.Pool, SecretKey: s.SecretKey}
			messageCtx := context.WithValue(ctx, concurrentMessageKey{}, true)
			key := fmt.Sprint("overlap-", i)
			results <- replica.mutation(messageCtx, worker, true, func(tx pgx.Tx) error {
				entered <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				var out SendResult
				return replica.sendTx(messageCtx, tx, worker, SendInput{Target: Target{"person", owner.ID}, Body: key}, key, &out)
			})
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("independent sends still serialize before validation", ctx.Err())
		}
	}
	policy := make(chan error, 1)
	go func() { policy <- s.Revoke(ctx, owner, worker.CredentialID) }()
	// Wait until the policy writer is actually queued, rather than relying on
	// a goroutine's scheduling delay. Then exercise both send lock upgrades.
	for {
		var blocked bool
		err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE query LIKE '%FROM scopes WHERE id=$1 FOR UPDATE' AND cardinality(pg_blocking_pids(pid))>0)`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-policy:
			t.Fatal("policy writer escaped the in-flight authority fence", err)
		case <-ctx.Done():
			t.Fatal("policy writer did not wait", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	unblock()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal("concurrent send or lock upgrade failed", err)
		}
	}
	if err := <-policy; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(ctx, worker, SendInput{Target: Target{"person", owner.ID}, Body: "After revocation"}, "revoked"); ErrorStatus(err) != 403 {
		t.Fatal("revocation did not take effect", err)
	}
}

func TestParallelSendRetryAndConversationOrderingAcrossReplicas(t *testing.T) {
	s, owner := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	worker := testWorker(t, s, owner, "parallel-receipt")
	in := SendInput{Target: Target{"agent", worker.ID}, Body: "One durable operation"}
	type result struct {
		out SendResult
		err error
	}
	results := make(chan result, 30)
	for range 30 {
		go func() {
			replica := &Store{Pool: s.Pool, SecretKey: s.SecretKey}
			x, err := replica.Send(ctx, owner, in, "same-key")
			results <- result{x, err}
		}()
	}
	var first SendResult
	for i := range 30 {
		x := <-results
		if x.err != nil {
			t.Fatal("retry failed", x.err)
		}
		if i == 0 {
			first = x.out
		} else if string(JSON(first)) != string(JSON(x.out)) {
			t.Fatal("duplicate key returned another result")
		}
	}
	in.Body = "Changed payload"
	if _, err := s.Send(ctx, owner, in, "same-key"); ErrorStatus(err) != 409 {
		t.Fatal("changed-key-content was accepted", err)
	}
	for i := range 30 {
		go func() {
			replica := &Store{Pool: s.Pool, SecretKey: s.SecretKey}
			x, err := replica.Send(ctx, worker, SendInput{ConversationID: first.ConversationID, Body: fmt.Sprint("reply-", i)}, fmt.Sprint("ordered-", i))
			results <- result{x, err}
		}()
	}
	for range 30 {
		if x := <-results; x.err != nil {
			t.Fatal(x.err)
		}
	}
	conversation, err := s.Conversation(ctx, owner, first.ConversationID)
	if err != nil || len(conversation.Messages) != 31 {
		t.Fatal("lost or duplicated history", len(conversation.Messages), err)
	}
	rows, err := s.Pool.Query(ctx, `SELECT data->>'id' FROM events WHERE scope=$1 AND conversation_id=$2 AND kind='message.created' ORDER BY sequence`, owner.Scope, first.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if i >= len(conversation.Messages) || id != conversation.Messages[i].ID {
			t.Fatal("event order differs from canonical message order", i, id)
		}
		i++
	}
	if rows.Err() != nil || i != 31 {
		t.Fatal(i, rows.Err())
	}
}

func TestParallelSendQuotaRollbackAcrossReplicas(t *testing.T) {
	s, owner := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	worker := testWorker(t, s, owner, "parallel-quota")
	if err := s.ApplyWorkspaceBudget(ctx, owner.Scope, WorkspaceBudget{Version: 1, Messages: 7, Requests: -1, UploadBytes: -1}); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 24)
	for i := range 24 {
		go func() {
			replica := &Store{Pool: s.Pool, SecretKey: s.SecretKey}
			_, err := replica.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "Bounded acceptance"}, fmt.Sprint("quota-", i))
			results <- err
		}()
	}
	accepted := 0
	for range 24 {
		if err := <-results; err == nil {
			accepted++
		} else if ErrorStatus(err) != 429 {
			t.Fatal("unexpected parallel failure", err)
		}
	}
	var messages, receipts, deliveries, conversations int
	err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages),(SELECT count(*) FROM idempotency WHERE operation='send'),(SELECT count(*) FROM deliveries),(SELECT count(*) FROM conversations)`).Scan(&messages, &receipts, &deliveries, &conversations)
	if err != nil || accepted != 7 || messages != 7 || receipts != 7 || deliveries != 7 || conversations != 7 {
		t.Fatal("quota or atomicity changed", accepted, messages, receipts, deliveries, conversations, err)
	}
}

func TestParallelMessageCursorCannotSkipUncommittedReservation(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprint("rollback=", rollback), func(t *testing.T) {
			s, owner := testStore(t)
			worker := testWorker(t, s, owner, "cursor-order")
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			var before int64
			if err := s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&before); err != nil {
				t.Fatal(err)
			}
			written := make(chan uint32, 1)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			first := make(chan error, 1)
			go func() {
				messageCtx := context.WithValue(ctx, concurrentMessageKey{}, true)
				first <- s.mutation(messageCtx, owner, true, func(tx pgx.Tx) error {
					var out SendResult
					if err := s.sendTx(messageCtx, tx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "First"}, "cursor-first", &out); err != nil {
						return err
					}
					written <- tx.Conn().PgConn().PID()
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
					if rollback {
						return Bad("Injected rollback after every canonical write")
					}
					return nil
				})
			}()
			var pid uint32
			select {
			case pid = <-written:
			case err := <-first:
				t.Fatal("first send failed before reservation", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			second := make(chan error, 1)
			go func() {
				replica := &Store{Pool: s.Pool, SecretKey: s.SecretKey}
				_, err := replica.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "Second"}, "cursor-second")
				second <- err
			}()
			for {
				var blocked bool
				if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case err := <-second:
					t.Fatal("later event escaped the uncommitted reservation", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			events, scanned, err := s.EventPage(ctx, owner, before, false)
			if err != nil || len(events) != 0 || scanned != before {
				t.Fatal("uncommitted event/cursor became visible", events, scanned, before, err)
			}
			unblock()
			if err = <-first; (rollback && ErrorStatus(err) != 400) || (!rollback && err != nil) {
				t.Fatal(err)
			}
			if err = <-second; err != nil {
				t.Fatal(err)
			}
			events, scanned, err = s.EventPage(ctx, owner, before, false)
			want := 2
			if rollback {
				want = 1
			}
			if err != nil || len(events) != want || scanned != before+int64(want) {
				t.Fatal("reservation gap or loss", len(events), scanned, before, err)
			}
			for i, event := range events {
				if event.Sequence != before+int64(i+1) {
					t.Fatal("commit order changed", events)
				}
			}
		})
	}
}

func TestParallelInboxKeepsOneRuntimeLeaseAndAckGeneration(t *testing.T) {
	s, owner := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	worker := testWorker(t, s, owner, "parallel-inbox")
	for i := range 2 {
		if _, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: fmt.Sprint("Job ", i)}, fmt.Sprint("job-", i)); err != nil {
			t.Fatal(err)
		}
	}
	type poll struct {
		jobs []Delivery
		err  error
	}
	results := make(chan poll, 12)
	for range 12 {
		go func() {
			replica := &Store{Pool: s.Pool, SecretKey: s.SecretKey}
			jobs, err := replica.Inbox(ctx, worker)
			results <- poll{jobs, err}
		}()
	}
	var lease Delivery
	generations := map[int64]bool{}
	for range 12 {
		x := <-results
		if x.err != nil || len(x.jobs) != 1 {
			t.Fatal(x.jobs, x.err)
		}
		job := x.jobs[0]
		if lease.ID != "" && lease.ID != job.ID {
			t.Fatal("parallel polls skipped this runtime's owned delivery")
		}
		if job.Generation > lease.Generation {
			lease = job
		}
		generations[job.Generation] = true
	}
	if len(generations) != 12 || lease.Generation != 12 {
		t.Fatal("lease generation update lost", generations)
	}
	if err := s.Ack(ctx, worker, lease.ID, 1); ErrorStatus(err) != 409 {
		t.Fatal("stale lease acknowledged", err)
	}
	acks := make(chan error, 12)
	for range 12 {
		go func() {
			replica := &Store{Pool: s.Pool, SecretKey: s.SecretKey}
			acks <- replica.Ack(ctx, worker, lease.ID, lease.Generation)
		}()
	}
	for range 12 {
		if err := <-acks; err != nil {
			t.Fatal(err)
		}
	}
	var events int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE scope=$1 AND kind='worker.received'`, owner.Scope).Scan(&events); err != nil || events != 1 {
		t.Fatal("duplicate acknowledgement event", events, err)
	}
	jobs, err := s.Inbox(ctx, worker)
	if err != nil || len(jobs) != 1 || jobs[0].ID == lease.ID || jobs[0].Generation != 1 {
		t.Fatal("next durable delivery was lost", jobs, err)
	}
}
