let firstNegative = fn(xs) {
  let i = 0;
  while i < len(xs) {
    if xs[i] < 0 { return i; }
    i = i + 1;
  }
  -1
};
print(firstNegative([3, 1, -4, 1, -5]), firstNegative([1, 2]));

let early = fn(n) {
  if n > 10 { if n > 100 { return "huge"; } return "big"; }
  "small"
};
print(early(5), early(50), early(500));

let bare = fn() { return; };
print(bare());

let inExpr = fn(x) { 1 + if x { return "bailed"; } else { 2 } };
print(inExpr(false), inExpr(true));

let nestedLoops = fn() {
  let i = 0;
  while i < 10 {
    let j = 0;
    while j < 10 {
      if i * j == 42 { return [i, j]; }
      j = j + 1;
    }
    i = i + 1;
  }
};
print(nestedLoops());

print("before top-level return");
return 7;
print("never printed");
