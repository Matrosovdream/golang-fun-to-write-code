package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"jobqueue/internal/job"
	"jobqueue/internal/queue"
)

// miniredis: a real Redis protocol implementation in pure Go — full queue
// semantics in unit tests, no Docker, microsecond startup.
func newQueue(t *testing.T) (*queue.Queue, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return queue.New(rdb), rdb
}

func TestEnqueueDequeueAck(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()

	j, err := q.Enqueue(ctx, "email", map[string]string{"to": "a@b.co"}, queue.EnqueueOptions{})
	require.NoError(t, err)
	require.Equal(t, job.StatePending, j.State)

	got, err := q.Dequeue(ctx)
	require.NoError(t, err)
	require.Equal(t, j.ID, got.ID)
	require.Equal(t, job.StateRunning, got.State)
	require.Equal(t, 1, got.Attempts)

	require.NoError(t, q.Ack(ctx, got))

	final, err := q.Job(ctx, j.ID)
	require.NoError(t, err)
	require.Equal(t, job.StateDone, final.State)

	stats, err := q.Stats(ctx)
	require.NoError(t, err)
	require.Zero(t, stats.Pending+stats.Processing+stats.Dead+stats.Delayed)

	_, err = q.Dequeue(ctx)
	require.ErrorIs(t, err, queue.ErrEmpty)
}

func TestPriorityOrdering(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()

	low, err := q.Enqueue(ctx, "t", nil, queue.EnqueueOptions{Priority: job.PriorityLow})
	require.NoError(t, err)
	high, err := q.Enqueue(ctx, "t", nil, queue.EnqueueOptions{Priority: job.PriorityHigh})
	require.NoError(t, err)

	first, err := q.Dequeue(ctx)
	require.NoError(t, err)
	require.Equal(t, high.ID, first.ID, "high priority must jump the line")

	second, err := q.Dequeue(ctx)
	require.NoError(t, err)
	require.Equal(t, low.ID, second.ID)
}

func TestDelayedJobIsInvisibleUntilDue(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()

	_, err := q.Enqueue(ctx, "t", nil, queue.EnqueueOptions{Delay: time.Second})
	require.NoError(t, err)

	_, err = q.Dequeue(ctx)
	require.ErrorIs(t, err, queue.ErrEmpty, "delayed job must not be dequeued early")

	moved, err := q.MoveDueJobs(ctx)
	require.NoError(t, err)
	require.Zero(t, moved)

	time.Sleep(1100 * time.Millisecond)
	moved, err = q.MoveDueJobs(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, moved)

	_, err = q.Dequeue(ctx)
	require.NoError(t, err)
}

func TestRetriesEndInDeadLetter(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()

	_, err := q.Enqueue(ctx, "t", nil, queue.EnqueueOptions{MaxAttempts: 2})
	require.NoError(t, err)

	// attempt 1 fails → scheduled for retry (backoff 1s)
	j, err := q.Dequeue(ctx)
	require.NoError(t, err)
	require.NoError(t, q.Fail(ctx, j, errors.New("boom")))

	stats, _ := q.Stats(ctx)
	require.EqualValues(t, 1, stats.Delayed)

	time.Sleep(1100 * time.Millisecond)
	_, err = q.MoveDueJobs(ctx)
	require.NoError(t, err)

	// attempt 2 fails → max reached → dead letter
	j, err = q.Dequeue(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, j.Attempts)
	require.NoError(t, q.Fail(ctx, j, errors.New("boom again")))

	final, err := q.Job(ctx, j.ID)
	require.NoError(t, err)
	require.Equal(t, job.StateDead, final.State)
	require.Contains(t, final.LastError, "boom again")

	stats, _ = q.Stats(ctx)
	require.EqualValues(t, 1, stats.Dead)
	require.Zero(t, stats.Pending+stats.Delayed+stats.Processing)
}

// The chaos test: a worker takes a job and dies (its lease vanishes, no ack).
// The reaper must put the job back — at-least-once delivery in action.
func TestReaperRecoversCrashedWorker(t *testing.T) {
	q, rdb := newQueue(t)
	ctx := context.Background()

	orig, err := q.Enqueue(ctx, "t", nil, queue.EnqueueOptions{})
	require.NoError(t, err)

	j, err := q.Dequeue(ctx)
	require.NoError(t, err)

	// Simulate the crash: the lease disappears, the ack never comes.
	require.NoError(t, rdb.Del(ctx, "q:lease:"+j.ID).Err())

	reaped, err := q.Reap(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, reaped)

	again, err := q.Dequeue(ctx)
	require.NoError(t, err)
	require.Equal(t, orig.ID, again.ID)
	require.Equal(t, 2, again.Attempts, "redelivery must count as a new attempt")
}

func TestIllegalTransitionRejected(t *testing.T) {
	j := &job.Job{ID: "x", State: job.StateDone}
	require.Error(t, j.TransitionTo(job.StateRunning))
}
