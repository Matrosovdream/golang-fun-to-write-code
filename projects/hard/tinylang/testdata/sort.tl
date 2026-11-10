let concat = fn(a, b) {
  let out = [];
  let i = 0;
  while i < len(a) { push(out, a[i]); i = i + 1; }
  i = 0;
  while i < len(b) { push(out, b[i]); i = i + 1; }
  out
};

let quick = fn(xs) {
  if len(xs) < 2 { return xs; }
  let pivot = xs[0];
  let lo = [];
  let hi = [];
  let i = 1;
  while i < len(xs) {
    if xs[i] < pivot { push(lo, xs[i]); } else { push(hi, xs[i]); }
    i = i + 1;
  }
  concat(concat(quick(lo), [pivot]), quick(hi))
};
print(quick([3, 6, 1, 8, 2, 9, 2, 7]));
print(quick(["pear", "apple", "fig", "banana"]));

let insert = fn(sorted, x) {
  let out = [];
  let placed = false;
  let i = 0;
  while i < len(sorted) {
    if !placed && x < sorted[i] { push(out, x); placed = true; }
    push(out, sorted[i]);
    i = i + 1;
  }
  if !placed { push(out, x); }
  out
};
let insertionSort = fn(xs) {
  let out = [];
  let i = 0;
  while i < len(xs) { out = insert(out, xs[i]); i = i + 1; }
  out
};
print(insertionSort([5, 2, 9, 1, 5, 6, -3, 0]));
