// the limit is 10000 active calls: 9999 more below the first one is fine, one more overflows
let down = fn(n) { if n == 0 { "bottom" } else { down(n - 1) } };
print(down(9999));
print(down(10000));
