# Generics

Type parameters let a function or type work over many types while staying statically checked. A constraint names what the type argument must support, such as being comparable or ordered. Use generics for containers and algorithms like a heap, a set or a map-and-filter helper, where the logic is identical for every element type. Prefer a plain interface when all you need is to call a method. The compiler usually infers type arguments, so call sites stay free of angle brackets.
