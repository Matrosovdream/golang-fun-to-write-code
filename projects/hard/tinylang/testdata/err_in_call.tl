let outer = fn() { inner(0) };
let inner = fn(n) { 10 / n };
print("go");
outer();
