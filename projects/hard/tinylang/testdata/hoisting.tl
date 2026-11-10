// every `let` of a block is visible in the whole block, so functions can be
// defined in any order and call each other
let isEven = fn(n) { if n == 0 { true } else { isOdd(n - 1) } };
let isOdd = fn(n) { if n == 0 { false } else { isEven(n - 1) } };
print(isEven(10), isOdd(7), isEven(7));

let main = fn() { helper(5) };
let helper = fn(x) { x * x };
print(main());

let outer = fn() {
  let a = fn() { b() + 1 };
  let b = fn() { 10 };
  a()
};
print(outer());

// ...but a value is only there once its `let` has run
let early = fn() { late };
let late = 7;
print(early());
