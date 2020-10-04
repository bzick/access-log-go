Access Log concept
====


Trying writes via go-routine (async writing, reopen and more). 
Unfortunately performance is worse than using locks. 

benchmark: 

```
BenchmarkAccessLog-8                      534584              2177 ns/op             744 B/op          3 allocs/op
BenchmarkPlainFileWithLock-8             1000000              1223 ns/op             305 B/op          1 allocs/op
BenchmarkPlainFileWithoutLock-8          1000000              1797 ns/op             222 B/op          1 allocs/op
```
