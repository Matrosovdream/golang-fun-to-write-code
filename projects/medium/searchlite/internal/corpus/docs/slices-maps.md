# Slices and maps

A slice is a view onto an array, described by a pointer, a length and a capacity. Appending beyond the capacity allocates a new array and copies, so two slices that once shared storage may silently diverge. Reslicing never copies, which makes it cheap but can pin a large backing array in memory. A map is an unordered hash table: iteration order is deliberately randomized, and writing to a nil map panics. Maps are not safe for concurrent use, so guard them with a mutex or use sync.Map for the narrow cases it fits.
