let s = "Hello" + ", " + "World";
print(s, len(s));
print(s[0], s[len(s) - 1], s[7]);
print("apple" < "banana", "b" > "a", "abc" == "abc", "abc" != "abd");
print("tab\there", "quote\"d", "back\\slash");
print(str(42) + "!", str(true), str(null), str([1, "two"]));
let rev = fn(s) {
  let out = "";
  let i = len(s) - 1;
  while i >= 0 { out = out + s[i]; i = i - 1; }
  out
};
print(rev("tinylang"));
let isPalindrome = fn(s) { s == rev(s) };
print(isPalindrome("racecar"), isPalindrome("racecars"));
let words = fn(s) {
  let out = [];
  let cur = "";
  let i = 0;
  while i < len(s) {
    if s[i] == " " { if len(cur) > 0 { push(out, cur); cur = ""; } } else { cur = cur + s[i]; }
    i = i + 1;
  }
  if len(cur) > 0 { push(out, cur); }
  out
};
print(words("  the quick  brown fox "));
print("" == "", len(""));
