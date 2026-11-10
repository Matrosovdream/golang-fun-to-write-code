# Interfaces

An interface describes behavior: any type with the right methods satisfies it, with no declaration of intent. This implicit satisfaction lets the consumer define the interface it needs, which keeps interfaces small and decoupled from implementations. Accept interfaces and return concrete types, so callers get the full value and the freedom to substitute fakes in tests. An interface value holds a type and a value, which is why an interface holding a nil pointer is not itself nil. The empty interface says nothing at all, so reach for it rarely.
