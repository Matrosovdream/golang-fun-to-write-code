// Package queue implements a reliable job queue on Redis — the same core
// design as Sidekiq/asynq, built from first principles.
//
// Data layout:
//
//	q:pending:{high|default|low}  LIST of job IDs, LPush in / LMove out
//	q:processing                  LIST of job IDs currently being worked
//	q:dead                        LIST of job IDs that exhausted retries
//	q:delayed                     ZSET id → unix run-at (delays AND retry backoff)
//	q:job:<id>                    the job document (JSON)
//	q:lease:<id>                  worker liveness key with TTL (see reaper)
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"jobqueue/internal/job"
)

var ErrEmpty = errors.New("no job available")

const leaseTTL = 30 * time.Second

// priorityOrder is the scan order on dequeue — strict priority.
var priorityOrder = []job.Priority{job.PriorityHigh, job.PriorityDefault, job.PriorityLow}

type Queue struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Queue { return &Queue{rdb: rdb} }

type EnqueueOptions struct {
	Priority    job.Priority
	Delay       time.Duration
	MaxAttempts int
}

func (q *Queue) Enqueue(ctx context.Context, jobType string, payload any, opts EnqueueOptions) (*job.Job, error) {
	if opts.Priority == "" {
		opts.Priority = job.PriorityDefault
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	j := &job.Job{
		ID:          newID(),
		Type:        jobType,
		Payload:     raw,
		Priority:    opts.Priority,
		State:       job.StatePending,
		MaxAttempts: opts.MaxAttempts,
		RunAt:       time.Now().Add(opts.Delay),
		CreatedAt:   time.Now(),
	}
	if err := q.saveJob(ctx, j); err != nil {
		return nil, err
	}

	if opts.Delay > 0 {
		err = q.rdb.ZAdd(ctx, "q:delayed", redis.Z{
			Score: float64(j.RunAt.Unix()), Member: j.ID,
		}).Err()
	} else {
		err = q.rdb.LPush(ctx, pendingKey(j.Priority), j.ID).Err()
	}
	return j, err
}

// Dequeue atomically moves one job id from a pending list to q:processing
// (LMOVE) and takes a lease. If the worker dies now, the id is still in
// q:processing and the reaper will recover it — that is the "reliable
// queue" pattern, and why a plain RPOP would lose jobs.
func (q *Queue) Dequeue(ctx context.Context) (*job.Job, error) {
	for _, prio := range priorityOrder {
		id, err := q.rdb.LMove(ctx, pendingKey(prio), "q:processing", "RIGHT", "LEFT").Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, err
		}

		j, err := q.loadJob(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := j.TransitionTo(job.StateRunning); err != nil {
			return nil, err
		}
		j.Attempts++
		if err := q.saveJob(ctx, j); err != nil {
			return nil, err
		}
		return j, q.rdb.Set(ctx, "q:lease:"+id, "1", leaseTTL).Err()
	}
	return nil, ErrEmpty
}

func (q *Queue) Ack(ctx context.Context, j *job.Job) error {
	if err := j.TransitionTo(job.StateDone); err != nil {
		return err
	}
	if err := q.saveJob(ctx, j); err != nil {
		return err
	}
	return q.release(ctx, j.ID)
}

// Fail retries with exponential backoff via the delayed zset, or moves the
// job to the dead-letter list once attempts are exhausted.
func (q *Queue) Fail(ctx context.Context, j *job.Job, cause error) error {
	j.LastError = cause.Error()

	if j.Attempts >= j.MaxAttempts {
		if err := j.TransitionTo(job.StateDead); err != nil {
			return err
		}
		if err := q.saveJob(ctx, j); err != nil {
			return err
		}
		if err := q.rdb.LPush(ctx, "q:dead", j.ID).Err(); err != nil {
			return err
		}
		return q.release(ctx, j.ID)
	}

	if err := j.TransitionTo(job.StatePending); err != nil {
		return err
	}
	j.RunAt = time.Now().Add(j.Backoff())
	if err := q.saveJob(ctx, j); err != nil {
		return err
	}
	if err := q.rdb.ZAdd(ctx, "q:delayed", redis.Z{
		Score: float64(j.RunAt.Unix()), Member: j.ID,
	}).Err(); err != nil {
		return err
	}
	return q.release(ctx, j.ID)
}

// Heartbeat extends the lease — call it while a long job is still alive.
func (q *Queue) Heartbeat(ctx context.Context, jobID string) error {
	return q.rdb.Expire(ctx, "q:lease:"+jobID, leaseTTL).Err()
}

// MoveDueJobs promotes delayed jobs whose time has come into their pending
// list. Run it periodically (the scheduler goroutine).
func (q *Queue) MoveDueJobs(ctx context.Context) (int, error) {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	ids, err := q.rdb.ZRangeByScore(ctx, "q:delayed", &redis.ZRangeBy{Min: "-inf", Max: now}).Result()
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	moved := 0
	for _, id := range ids {
		j, err := q.loadJob(ctx, id)
		if err != nil {
			continue
		}
		// ZRem first: if two schedulers race, only the one that removes the
		// member gets to enqueue it — exactly-once promotion.
		removed, err := q.rdb.ZRem(ctx, "q:delayed", id).Result()
		if err != nil || removed == 0 {
			continue
		}
		if err := q.rdb.LPush(ctx, pendingKey(j.Priority), id).Err(); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// Reap finds jobs stuck in q:processing whose lease expired — their worker
// crashed mid-job — and returns them to pending. At-least-once delivery:
// this is precisely why handlers must be idempotent.
func (q *Queue) Reap(ctx context.Context) (int, error) {
	ids, err := q.rdb.LRange(ctx, "q:processing", 0, -1).Result()
	if err != nil {
		return 0, err
	}
	reaped := 0
	for _, id := range ids {
		alive, err := q.rdb.Exists(ctx, "q:lease:"+id).Result()
		if err != nil || alive == 1 {
			continue
		}
		j, err := q.loadJob(ctx, id)
		if err != nil {
			continue
		}
		j.State = job.StatePending // forced: the crashed worker can't object
		if err := q.saveJob(ctx, j); err != nil {
			continue
		}
		if err := q.rdb.LRem(ctx, "q:processing", 1, id).Err(); err != nil {
			continue
		}
		if err := q.rdb.LPush(ctx, pendingKey(j.Priority), id).Err(); err != nil {
			return reaped, err
		}
		reaped++
	}
	return reaped, nil
}

func (q *Queue) Job(ctx context.Context, id string) (*job.Job, error) {
	return q.loadJob(ctx, id)
}

type Stats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Delayed    int64 `json:"delayed"`
	Dead       int64 `json:"dead"`
}

func (q *Queue) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	for _, prio := range priorityOrder {
		n, err := q.rdb.LLen(ctx, pendingKey(prio)).Result()
		if err != nil {
			return s, err
		}
		s.Pending += n
	}
	var err error
	if s.Processing, err = q.rdb.LLen(ctx, "q:processing").Result(); err != nil {
		return s, err
	}
	if s.Delayed, err = q.rdb.ZCard(ctx, "q:delayed").Result(); err != nil {
		return s, err
	}
	s.Dead, err = q.rdb.LLen(ctx, "q:dead").Result()
	return s, err
}

// --- internals ---

func (q *Queue) release(ctx context.Context, id string) error {
	if err := q.rdb.LRem(ctx, "q:processing", 1, id).Err(); err != nil {
		return err
	}
	return q.rdb.Del(ctx, "q:lease:"+id).Err()
}

func (q *Queue) saveJob(ctx context.Context, j *job.Job) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return q.rdb.Set(ctx, "q:job:"+j.ID, raw, 7*24*time.Hour).Err()
}

func (q *Queue) loadJob(ctx context.Context, id string) (*job.Job, error) {
	raw, err := q.rdb.Get(ctx, "q:job:"+id).Bytes()
	if err != nil {
		return nil, fmt.Errorf("load job %s: %w", id, err)
	}
	var j job.Job
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func pendingKey(p job.Priority) string { return "q:pending:" + string(p) }

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
