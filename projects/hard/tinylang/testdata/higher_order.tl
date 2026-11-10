let map = fn(f, xs) {
  let out = [];
  let i = 0;
  while i < len(xs) { push(out, f(xs[i])); i = i + 1; }
  out
};
let filter = fn(pred, xs) {
  let out = [];
  let i = 0;
  while i < len(xs) {
    if pred(xs[i]) { push(out, xs[i]); }
    i = i + 1;
  }
  out
};
let reduce = fn(f, acc, xs) {
  let i = 0;
  while i < len(xs) { acc = f(acc, xs[i]); i = i + 1; }
  acc
};
let compose = fn(f, g) { fn(x) { f(g(x)) } };

let nums = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
let double = fn(x) { x * 2 };
let isEven = fn(x) { x % 2 == 0 };
print(map(double, nums));
print(filter(isEven, nums));
print(reduce(fn(a, b) { a + b }, 0, nums));
print(reduce(fn(a, b) { a * b }, 1, [1, 2, 3, 4, 5, 6]));
print(map(compose(double, double), [1, 2, 3]));
print(reduce(fn(acc, x) { acc + str(x) }, "", nums));
print(map(len, ["a", "bb", "ccc"]));
