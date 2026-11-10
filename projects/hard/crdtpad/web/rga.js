// JavaScript port of internal/rga: same insertion rule, same wire format.
// Positions are code-point indexes (not UTF-16 units) so emoji are one character.
'use strict';

const HEAD = { clock: 0, replica: '' };

const same = (a, b) => a.clock === b.clock && a.replica === b.replica;
const compare = (a, b) =>
  a.clock - b.clock || (a.replica < b.replica ? -1 : a.replica > b.replica ? 1 : 0);

class Doc {
  constructor(replica) {
    this.replica = replica;
    this.nodes = [{ id: HEAD, char: '', dead: true }]; // document order; nodes[0] is the head
    this.clock = 0;
    this.version = {}; // replica -> highest seq applied
    this.log = [];     // applied ops, a valid causal order
    this.pending = {}; // replica -> { seq: op }
  }

  indexOf(id) {
    const want = id || HEAD; // the server omits a zero "after"
    return this.nodes.findIndex((n) => same(n.id, want));
  }

  // index into nodes of the i-th visible character; i === -1 is the head
  slotOf(i) {
    if (i === -1) return 0;
    for (let k = 1, seen = 0; k < this.nodes.length; k++) {
      if (!this.nodes[k].dead && seen++ === i) return k;
    }
    return -1;
  }

  insert(pos, char) {
    const parent = this.nodes[this.slotOf(pos - 1)];
    const op = { replica: this.replica, seq: this.nextSeq(), kind: 'ins', clock: this.clock + 1, char: char.codePointAt(0) };
    if (!same(parent.id, HEAD)) op.after = parent.id;
    this.integrate(op);
    return op;
  }

  delete(pos) {
    const target = this.nodes[this.slotOf(pos)];
    const op = { replica: this.replica, seq: this.nextSeq(), kind: 'del', target: target.id };
    this.integrate(op);
    return op;
  }

  nextSeq() {
    return (this.version[this.replica] || 0) + 1;
  }

  // Deliver an op from anywhere, in any order, any number of times.
  // Returns the ops that were integrated as a result.
  apply(op) {
    if (op.seq <= (this.version[op.replica] || 0)) return [];
    (this.pending[op.replica] ||= {})[op.seq] = op;
    return this.drain();
  }

  drain() {
    const applied = [];
    for (let progress = true; progress; ) {
      progress = false;
      for (const [r, queue] of Object.entries(this.pending)) {
        const op = queue[(this.version[r] || 0) + 1];
        if (!op || !this.ready(op)) continue;
        this.integrate(op);
        applied.push(op);
        delete queue[op.seq];
        if (Object.keys(queue).length === 0) delete this.pending[r];
        progress = true;
      }
    }
    return applied;
  }

  ready(op) {
    const dep = op.kind === 'ins' ? op.after : op.target;
    return op.seq === (this.version[op.replica] || 0) + 1 && this.indexOf(dep) >= 0;
  }

  integrate(op) {
    if (op.kind === 'ins') {
      const id = { clock: op.clock, replica: op.replica };
      let i = this.indexOf(op.after) + 1;
      while (i < this.nodes.length && compare(this.nodes[i].id, id) > 0) i++;
      this.nodes.splice(i, 0, { id, char: String.fromCodePoint(op.char), dead: false });
      this.clock = Math.max(this.clock, op.clock);
    } else {
      this.nodes[this.indexOf(op.target)].dead = true;
    }
    this.version[op.replica] = op.seq;
    this.log.push(op);
  }

  missing(since) {
    return this.log.filter((op) => op.seq > (since[op.replica] || 0));
  }

  chars() {
    return this.nodes.filter((n) => !n.dead).map((n) => n.char);
  }

  text() {
    return this.chars().join('');
  }

  // A caret is remembered as "just after this character". Characters are never
  // removed from the list, so the anchor survives every remote edit.
  anchorAt(pos) {
    return this.nodes[this.slotOf(pos - 1)].id;
  }

  posOf(anchor) {
    const k = this.indexOf(anchor);
    let n = 0;
    for (let i = 1; i <= k; i++) if (!this.nodes[i].dead) n++;
    return n;
  }
}

if (typeof module !== 'undefined') module.exports = { Doc };
