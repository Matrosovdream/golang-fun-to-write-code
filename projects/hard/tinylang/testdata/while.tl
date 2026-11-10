let i = 0;
let sum = 0;
while i < 100 { sum = sum + i; i = i + 1; }
print("sum", sum);

let collatz = fn(n) {
  let steps = 0;
  while n != 1 {
    if n % 2 == 0 { n = n / 2; } else { n = 3 * n + 1; }
    steps = steps + 1;
  }
  steps
};
print("collatz(27) =", collatz(27));

let primes = fn(limit) {
  let out = [];
  let n = 2;
  while n <= limit {
    let isPrime = true;
    let d = 2;
    while d * d <= n && isPrime {
      if n % d == 0 { isPrime = false; }
      d = d + 1;
    }
    if isPrime { push(out, n); }
    n = n + 1;
  }
  out
};
print(primes(50));

let x = 0;
while false { x = 1; }
print("never ran:", x);
