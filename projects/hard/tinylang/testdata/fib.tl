// recursion
let fib = fn(n) { if n < 2 { n } else { fib(n - 1) + fib(n - 2) } };
let i = 0;
while i <= 15 {
  print("fib", i, "=", fib(i));
  i = i + 1;
}

let fact = fn(n) { if n <= 1 { return 1; } n * fact(n - 1) };
print("10! =", fact(10));

let gcd = fn(a, b) { if b == 0 { a } else { gcd(b, a % b) } };
print("gcd(84, 36) =", gcd(84, 36));
