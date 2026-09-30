# practice/

Your rewrites live here, one directory per project (`practice/taskcli/`, ...), each with its
own `go.mod` — exactly like the originals.

Rules of the game:

1. Read the original top-down once, then close it and build from memory.
2. Peek only when stuck for real, and only at the one spot you're stuck on.
3. Steal the original's tests early — they define "works".
4. For the concurrent projects (fetchpool, chathub, jobqueue, metricsd): `-race` always.
5. When yours passes, diff it against the original and write down what they did better —
   and what you did better.
