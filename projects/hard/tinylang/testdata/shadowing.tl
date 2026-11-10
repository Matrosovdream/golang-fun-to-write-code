let x = 1;
if true {
  let x = 2;
  print("inner", x);
  if true { let x = 3; print("innermost", x); }
  print("inner again", x);
}
print("outer", x);

let f = fn(x) { let y = x * 2; x = x + 1; [x, y] };
print(f(10), x);

let g = fn() { let x = "shadowed in fn"; x };
print(g(), x);

let len = fn(v) { 42 };
print(len([1, 2, 3]));

// assignment targets the innermost visible binding
let n = 0;
let bump = fn() { n = n + 1; };
bump(); bump();
print(n);
if true { let n = 100; bump(); print(n); }
print(n);

// redeclaring in the same scope reuses the binding
let r = 1;
let getR = fn() { r };
let r = 2;
print(getR());
