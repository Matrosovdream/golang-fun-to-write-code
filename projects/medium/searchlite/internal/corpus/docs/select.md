# The select statement

Select waits on several channel operations at once and runs the first one that can proceed. If more than one is ready it picks one at random, which stops a busy channel from starving the others. A default case makes the select non-blocking, turning it into a try-send or a try-receive. Pair a receive on a timer channel with your work channel to build timeouts without any extra goroutine. Select with a done channel is the classic way to let a worker exit when its owner loses interest.
