let a = [1, 2, 3];
let b = a; // same array, not a copy
push(b, 4);
print(a, len(a), a == b, a == [1, 2, 3, 4]);
print(push(push([], "x"), "y"));
let nested = [[1, 2], [3, [4, 5]], []];
print(nested, nested[1][1][0], len(nested[2]));
let grow = fn(arr, n) { let i = 0; while i < n { push(arr, i * i); i = i + 1; } arr };
print(grow([], 6));
let copy = fn(xs) { let out = []; let i = 0; while i < len(xs) { push(out, xs[i]); i = i + 1; } out };
let c = copy(a);
push(c, 99);
print(a, c);
print([1, [2, 3]][1][0]);
