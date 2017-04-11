watts-plugin-tester
====


build
----
```
go get && go build
```

usage
----
```
./watts-plugin-tester --help
```


interpretation of exit codes
---
```
	0 -- validation passed
	1 -- error with the plugin output / validation failed
	2 -- error executing the plugin
	3 -- internal error
	4 -- user error. possibly a malformed json file
```
