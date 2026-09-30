package worker_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"jobqueue/internal/job"
	"jobqueue/internal/queue"
	"jobqueue/internal/worker"
)

func newPool(t *testing.T, size int) (*worker.Pool, *queue.Queue) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	q := queue.New(rdb)
	return worker.NewPool(q, slog.New(slog.NewTextHandler(io.Discard, nil)), size), q
}

func waitForState(t *testing.T, q *queue.Queue, id string, want job.State, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		j, err := q.Job(context.Background(), id)
		require.NoError(t, err)
		if j.State == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	j, _ := q.Job(context.Background(), id)
	t.Fatalf("job %s never reached %s (state=%s attempts=%d err=%q)",
		id, want, j.State, j.Attempts, j.LastError)
}

// End-to-end through the pool: a handler that fails twice must succeed on
// the 3rd attempt, driven by the scheduler promoting its backoff retries.
func TestPoolRetriesFlakyJobToSuccess(t *testing.T) {
	pool, q := newPool(t, 2)
	pool.Register("flaky", func(_ context.Context, j *job.Job) error {
		if j.Attempts < 3 {
			return fmt.Errorf("fail %d", j.Attempts)
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	poolDone := make(chan struct{})
	go func() { defer close(poolDone); pool.Run(ctx) }()

	j, err := q.Enqueue(context.Background(), "flaky", nil, queue.EnqueueOptions{MaxAttempts: 5})
	require.NoError(t, err)

	// backoff after attempts 1 and 2 is 1s + 2s → give it 8s of slack
	waitForState(t, q, j.ID, job.StateDone, 8*time.Second)

	final, err := q.Job(context.Background(), j.ID)
	require.NoError(t, err)
	require.Equal(t, 3, final.Attempts)

	// Graceful drain: cancel must end Run.
	cancel()
	select {
	case <-poolDone:
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not drain after cancel")
	}
}

func TestPoolSurvivesPanickingHandler(t *testing.T) {
	pool, q := newPool(t, 1)
	pool.Register("boom", func(context.Context, *job.Job) error {
		panic("bug")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pool.Run(ctx)

	j, err := q.Enqueue(context.Background(), "boom", nil, queue.EnqueueOptions{MaxAttempts: 1})
	require.NoError(t, err)

	waitForState(t, q, j.ID, job.StateDead, 5*time.Second)

	final, err := q.Job(context.Background(), j.ID)
	require.NoError(t, err)
	require.Contains(t, final.LastError, "panicked")
}

func TestUnknownJobTypeGoesDead(t *testing.T) {
	pool, q := newPool(t, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pool.Run(ctx)

	j, err := q.Enqueue(context.Background(), "nobody-handles-this", nil, queue.EnqueueOptions{MaxAttempts: 1})
	require.NoError(t, err)

	waitForState(t, q, j.ID, job.StateDead, 5*time.Second)
}
