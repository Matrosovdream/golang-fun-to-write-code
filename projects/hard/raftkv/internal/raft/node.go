// Package raft implements the Raft consensus algorithm from the extended paper
// (Ongaro & Ousterhout, "In Search of an Understandable Consensus Algorithm").
// Snapshots and membership changes are not implemented.
//
// Concurrency: one goroutine, Node.Run, owns every field under "loop-owned"
// in Node. Peer messages, client proposals, status queries and clock ticks
// reach it over channels; there is not a single mutex in the algorithm. Time
// comes only from clock.Clock, the network only through Transport, the disk
// only through Storage, which is what lets tests run it on a fake clock.
//
// Figure 2 of the paper, and where each rule is enforced:
//
//	State, persistent on all servers (saved BEFORE replying to anyone)
//	  currentTerm, votedFor        n.term, n.votedFor      saveHardState
//	  log[]                        n.log                   appendEntries, truncateFrom
//	State, volatile
//	  commitIndex, lastApplied     n.commitIndex, n.lastApplied
//	State, volatile on leaders (reinitialised after every election)
//	  nextIndex[], matchIndex[]    n.nextIndex, n.matchIndex     becomeLeader
//
//	RequestVote, receiver (onRequestVote)
//	  1. reply false if term < currentTerm
//	  2. grant if votedFor is null or candidateId AND the candidate's log is at
//	     least as up-to-date as ours (upToDate)
//
//	AppendEntries, receiver (onAppendEntries)
//	  1. reply false if term < currentTerm
//	  2. reply false if the log has no entry at prevLogIndex matching prevLogTerm
//	  3. if an existing entry conflicts with a new one (same index, different
//	     term) delete it and everything after it: only on a real conflict, so a
//	     stale or duplicated message can never shorten a log
//	  4. append the entries not already in the log
//	  5. if leaderCommit > commitIndex, commitIndex = min(leaderCommit, index of
//	     the last NEW entry), never backwards
//
//	Rules for servers
//	  All:        apply committed entries in order (applyCommitted); any message
//	              with a higher term makes us adopt it and become a follower (step)
//	  Followers:  answer RPCs; no AppendEntries from the current leader and no
//	              vote granted within the election timeout -> candidate (tick)
//	  Candidates: increment term, vote for self, reset timer, ask everyone
//	              (campaign); majority of votes -> leader (maybeWin); AppendEntries
//	              from a leader of our term -> follower (onAppendEntries); timeout
//	              -> new election
//	  Leaders:    heartbeat on idle (tick); append client commands, answer the
//	              client once applied (propose, waiters); send entries from
//	              nextIndex (sendAppend); on success advance matchIndex, on failure
//	              back nextIndex off and retry (onAppendResp); commit N when a
//	              majority has matchIndex >= N AND log[N].term == currentTerm
//	              (advanceCommit: the Figure 8 rule). A new leader appends a no-op
//	              (becomeLeader) so entries from earlier terms get committed
//	              without waiting for a client write.
package raft

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"raftkv/internal/clock"
)

const (
	TickInterval   = 50 * time.Millisecond
	electionTicks  = 10 // the timeout is drawn from [electionTicks, 2*electionTicks) ticks
	heartbeatTicks = 2
	maxBatch       = 64 // entries per AppendEntries
	inboxSize      = 256
)

type proposal struct {
	cmd   []byte
	reply chan result // buffered 1: the loop sends exactly once and never blocks
}

type result struct {
	value any
	err   error
}

type Node struct {
	id      NodeID
	peers   []NodeID // everyone but this node
	quorum  int
	store   Storage
	machine StateMachine
	tr      Transport
	clock   clock.Clock
	onRole  func(uint64, Role)

	inbox     chan Message
	proposals chan proposal
	statuses  chan chan Status
	done      chan struct{}

	// Loop-owned: only the goroutine in Run may touch anything below.
	term             uint64
	votedFor         NodeID
	log              []Entry // log[i-1] holds index i
	commitIndex      uint64
	lastApplied      uint64
	role             Role
	leader           NodeID
	votes            map[NodeID]bool
	nextIndex        map[NodeID]uint64
	matchIndex       map[NodeID]uint64
	electionElapsed  int
	electionTimeout  int
	heartbeatElapsed int
	waiters          map[uint64]chan<- result // client proposals awaiting their entry, by log index
	rng              *rand.Rand
}

// New restores the node from cfg.Storage. Call Run to start it.
func New(cfg Config) (*Node, error) {
	if !slices.Contains(cfg.Peers, cfg.ID) {
		return nil, fmt.Errorf("raft: node %d is not in peers %v", cfg.ID, cfg.Peers)
	}
	hs, log, err := cfg.Storage.Load()
	if err != nil {
		return nil, fmt.Errorf("raft: load storage: %w", err)
	}
	peers := slices.DeleteFunc(slices.Clone(cfg.Peers), func(id NodeID) bool { return id == cfg.ID })
	slices.Sort(peers)
	n := &Node{
		id: cfg.ID, peers: peers, quorum: len(cfg.Peers)/2 + 1,
		store: cfg.Storage, machine: cfg.Machine, tr: cfg.Transport, clock: cfg.Clock, onRole: cfg.OnRole,
		inbox:     make(chan Message, inboxSize),
		proposals: make(chan proposal),
		statuses:  make(chan chan Status),
		done:      make(chan struct{}),
		term:      hs.Term, votedFor: hs.VotedFor, log: log,
		waiters: make(map[uint64]chan<- result),
		rng:     rand.New(rand.NewPCG(cfg.Seed, uint64(cfg.ID))),
	}
	n.resetElectionTimer()
	return n, nil
}

// Run is the event loop. It returns when ctx is cancelled; every waiting
// proposal then fails with ErrStopped.
func (n *Node) Run(ctx context.Context) {
	defer close(n.done)
	ticker := n.clock.NewTicker(TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C():
			n.tick()
		case m := <-n.inbox:
			n.step(m)
		case p := <-n.proposals:
			n.propose(p)
		case reply := <-n.statuses:
			reply <- n.status()
		case <-ctx.Done():
			n.failWaiters(ErrStopped)
			return
		}
	}
}

// Receive hands a peer message to the loop without ever blocking. A full
// inbox drops the message, which Raft is built to tolerate.
func (n *Node) Receive(m Message) {
	select {
	case n.inbox <- m:
	default:
	}
}

// Propose appends cmd to the replicated log and waits until it has been
// committed and applied, returning what the state machine returned. On a
// follower, or if leadership is lost first, it returns ErrNotLeader. An
// expired ctx or ErrNotLeader does NOT mean the command was not committed
// (it may be in the new leader's log), so callers must retry with an
// idempotency key.
func (n *Node) Propose(ctx context.Context, cmd []byte) (any, error) {
	if len(cmd) == 0 {
		return nil, errors.New("raft: empty command")
	}
	p := proposal{cmd: cmd, reply: make(chan result, 1)}
	select {
	case n.proposals <- p:
	case <-n.done:
		return nil, ErrStopped
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case r := <-p.reply:
		return r.value, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Status asks the loop for a snapshot; it returns the zero Status once the node has stopped.
func (n *Node) Status() Status {
	reply := make(chan Status, 1)
	select {
	case n.statuses <- reply:
		return <-reply
	case <-n.done:
		return Status{}
	}
}

func (n *Node) status() Status {
	return Status{ID: n.id, Term: n.term, Role: n.role, Leader: n.leader,
		LastIndex: n.lastIndex(), CommitIndex: n.commitIndex, LastApplied: n.lastApplied}
}

func (n *Node) tick() {
	if n.role == Leader {
		n.heartbeatElapsed++
		if n.heartbeatElapsed >= heartbeatTicks {
			n.heartbeatElapsed = 0
			n.broadcastAppend()
		}
		return
	}
	n.electionElapsed++
	if n.electionElapsed >= n.electionTimeout {
		n.campaign()
	}
}

func (n *Node) step(m Message) {
	if m.To != n.id || !slices.Contains(n.peers, m.From) {
		return
	}
	if m.Term > n.term { // rules for all servers: a higher term always wins
		n.becomeFollower(m.Term)
	}
	switch m.Type {
	case MsgRequestVote:
		n.onRequestVote(m)
	case MsgRequestVoteResp:
		n.onVoteResp(m)
	case MsgAppendEntries:
		n.onAppendEntries(m)
	case MsgAppendEntriesResp:
		n.onAppendResp(m)
	}
}

func (n *Node) becomeFollower(term uint64) {
	if term > n.term {
		n.term, n.votedFor = term, 0
		n.saveHardState()
	}
	n.setRole(Follower)
	n.leader = 0
	n.resetElectionTimer()
	// A leader that is deposed must not ack its pending writes: the new
	// leader may overwrite them. The client retries; dedupe makes that safe.
	n.failWaiters(ErrNotLeader)
}

func (n *Node) campaign() {
	n.term++
	n.votedFor = n.id
	n.saveHardState() // durable before any RequestVote leaves
	n.setRole(Candidate)
	n.leader = 0
	n.votes = map[NodeID]bool{n.id: true}
	n.resetElectionTimer()
	last := n.lastIndex()
	for _, p := range n.peers {
		n.send(Message{Type: MsgRequestVote, To: p, LastLogIndex: last, LastLogTerm: n.termAt(last)})
	}
	n.maybeWin()
}

func (n *Node) onRequestVote(m Message) {
	grant := m.Term == n.term &&
		(n.votedFor == 0 || n.votedFor == m.From) &&
		n.upToDate(m.LastLogIndex, m.LastLogTerm)
	if grant {
		n.votedFor = m.From
		n.saveHardState() // durable before the reply: a restart must not vote again this term
		n.resetElectionTimer()
	}
	n.send(Message{Type: MsgRequestVoteResp, To: m.From, Success: grant})
}

// upToDate is the voting restriction: the candidate's log must be at least as
// up-to-date as ours, comparing last terms first and lengths second. It is what
// guarantees a leader's log contains every committed entry.
func (n *Node) upToDate(lastIndex, lastTerm uint64) bool {
	mine := n.lastIndex()
	return lastTerm > n.termAt(mine) || (lastTerm == n.termAt(mine) && lastIndex >= mine)
}

func (n *Node) onVoteResp(m Message) {
	if n.role != Candidate || m.Term != n.term || !m.Success {
		return
	}
	n.votes[m.From] = true // a set: duplicated responses cannot vote twice
	n.maybeWin()
}

func (n *Node) maybeWin() {
	if n.role == Candidate && len(n.votes) >= n.quorum {
		n.becomeLeader()
	}
}

func (n *Node) becomeLeader() {
	n.setRole(Leader)
	n.leader = n.id
	n.heartbeatElapsed = 0
	n.nextIndex = make(map[NodeID]uint64, len(n.peers))
	n.matchIndex = make(map[NodeID]uint64, len(n.peers))
	for _, p := range n.peers {
		n.nextIndex[p] = n.lastIndex() + 1
	}
	// Entries from earlier terms may only be committed indirectly, by
	// replicating an entry of this term (Figure 8). The no-op is that entry.
	n.appendEntries(Entry{Term: n.term})
	n.broadcastAppend()
	n.advanceCommit()
}

func (n *Node) propose(p proposal) {
	if n.role != Leader {
		p.reply <- result{err: ErrNotLeader}
		return
	}
	n.appendEntries(Entry{Term: n.term, Data: p.cmd})
	n.waiters[n.lastIndex()] = p.reply
	n.broadcastAppend()
	n.advanceCommit()
}

func (n *Node) broadcastAppend() {
	for _, p := range n.peers {
		n.sendAppend(p)
	}
}

func (n *Node) sendAppend(to NodeID) {
	prev := n.nextIndex[to] - 1
	end := min(n.lastIndex(), prev+maxBatch)
	n.send(Message{
		Type: MsgAppendEntries, To: to,
		PrevLogIndex: prev, PrevLogTerm: n.termAt(prev),
		Entries:      slices.Clone(n.log[prev:end]), // a copy: the receiver reads it on another goroutine
		LeaderCommit: n.commitIndex,
	})
}

func (n *Node) onAppendEntries(m Message) {
	resp := Message{Type: MsgAppendEntriesResp, To: m.From}
	if m.Term < n.term {
		n.send(resp) // carries our term, so the stale leader steps down
		return
	}
	if n.role != Follower {
		n.becomeFollower(m.Term) // a candidate yields to the leader of its own term
	}
	n.leader = m.From
	n.resetElectionTimer()

	if m.PrevLogIndex > n.lastIndex() || n.termAt(m.PrevLogIndex) != m.PrevLogTerm {
		resp.Index = n.lastIndex()
		n.send(resp)
		return
	}
	for i, e := range m.Entries {
		idx := m.PrevLogIndex + 1 + uint64(i)
		if idx <= n.lastIndex() {
			if n.termAt(idx) == e.Term {
				continue // already have it
			}
			n.truncateFrom(idx)
		}
		n.appendEntries(m.Entries[i:]...)
		break
	}
	last := m.PrevLogIndex + uint64(len(m.Entries))
	if c := min(m.LeaderCommit, last); c > n.commitIndex {
		n.commitIndex = c
		n.applyCommitted()
	}
	resp.Success, resp.Index = true, last
	n.send(resp)
}

func (n *Node) onAppendResp(m Message) {
	if n.role != Leader || m.Term != n.term {
		return
	}
	p := m.From
	if !m.Success {
		// Back off to just past what the follower says it has. Never below
		// matchIndex+1, which a stale rejection must not undo.
		n.nextIndex[p] = max(min(n.nextIndex[p]-1, m.Index+1), n.matchIndex[p]+1)
		n.sendAppend(p)
		return
	}
	if m.Index > n.matchIndex[p] { // duplicated or reordered acks never move matchIndex back
		n.matchIndex[p] = m.Index
		n.nextIndex[p] = m.Index + 1
		n.advanceCommit()
		if n.nextIndex[p] <= n.lastIndex() {
			n.sendAppend(p)
		}
	}
}

func (n *Node) advanceCommit() {
	idx := []uint64{n.lastIndex()}
	for _, p := range n.peers {
		idx = append(idx, n.matchIndex[p])
	}
	slices.Sort(idx)
	N := idx[len(idx)-n.quorum] // replicated on at least a quorum of nodes
	if N > n.commitIndex && n.termAt(N) == n.term {
		n.commitIndex = N
		n.applyCommitted()
	}
}

func (n *Node) applyCommitted() {
	for n.lastApplied < n.commitIndex {
		n.lastApplied++
		var res any
		if data := n.log[n.lastApplied-1].Data; len(data) > 0 {
			res = n.machine.Apply(n.lastApplied, data)
		}
		if w, ok := n.waiters[n.lastApplied]; ok {
			w <- result{value: res}
			delete(n.waiters, n.lastApplied)
		}
	}
}

func (n *Node) failWaiters(err error) {
	for i, w := range n.waiters {
		w <- result{err: err}
		delete(n.waiters, i)
	}
}

func (n *Node) send(m Message) {
	m.From, m.Term = n.id, n.term
	n.tr.Send(m)
}

func (n *Node) setRole(r Role) {
	if n.role == r {
		return
	}
	n.role = r
	if n.onRole != nil {
		n.onRole(n.term, r)
	}
}

func (n *Node) resetElectionTimer() {
	n.electionElapsed = 0
	n.electionTimeout = electionTicks + n.rng.IntN(electionTicks)
}

func (n *Node) lastIndex() uint64 { return uint64(len(n.log)) }

func (n *Node) termAt(index uint64) uint64 {
	if index == 0 {
		return 0
	}
	return n.log[index-1].Term
}

// The three persistence helpers panic on failure: a node that cannot write its
// state must stop rather than reply, and crashing is the safe way to stop.
func (n *Node) saveHardState() {
	fatal(n.store.SaveHardState(HardState{Term: n.term, VotedFor: n.votedFor}))
}

func (n *Node) appendEntries(es ...Entry) {
	fatal(n.store.Append(es))
	n.log = append(n.log, es...)
}

func (n *Node) truncateFrom(index uint64) {
	if index <= n.commitIndex {
		panic(fmt.Sprintf("raft: node %d asked to truncate committed index %d (commit %d)", n.id, index, n.commitIndex))
	}
	fatal(n.store.TruncateFrom(index))
	n.log = n.log[:index-1]
}

func fatal(err error) {
	if err != nil {
		panic(fmt.Errorf("raft: storage failure: %w", err))
	}
}
