// closures capture variables, not values
let makeCounter = fn() {
  let n = 0;
  fn() { n = n + 1; n }
};
let a = makeCounter();
let b = makeCounter();
print(a(), a(), a(), b());

// two closures over the same variable see each other's writes
let makeAccount = fn(balance) {
  let deposit = fn(x) { balance = balance + x; balance };
  let peek = fn() { balance };
  [deposit, peek]
};
let acct = makeAccount(100);
acct[0](50);
print("balance", acct[1]());

// currying
let add = fn(x) { fn(y) { fn(z) { x + y + z } } };
print(add(1)(2)(3));
let add10 = add(10);
print(add10(1)(1), add10(5)(5));

// each loop iteration gets its own `let`
let fs = [];
let i = 0;
while i < 3 {
  let j = i * 10;
  push(fs, fn() { j });
  i = i + 1;
}
print(fs[0](), fs[1](), fs[2]());

// a closure outlives the call that made it
let adders = [];
let k = 0;
while k < 3 {
  push(adders, add(k));
  k = k + 1;
}
print(adders[2](5)(5));
