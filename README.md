watts-plugin-tester
====

build
---
```
go get   github.com/indigo-dc/watts-plugin-tester
go build github.com/indigo-dc/watts-plugin-tester
(with a set GOPATH)
```
or
```
utils/compile.sh
```

get started
---
```
./watts-plugin-tester --help
```

features
---
- plugin output validation
- plugin input validation
- specify plugin input via a json file
	- The json gets extended with the defaults parameters to result in a valid plugin input
- parse configuration parameters from an existing watts.conf
	- lower precedence than a provided json
- machine readable json output
- generate a default json from the output of the plugins parameter action


watts version support
---
- [x] 1.0.0

Remark: leading 'v's in a provided version will be ignored


interpretation of exit codes
---
	0 -- validation passed
	1 -- error with the plugin output / validation failed
	2 -- error executing the plugin
	3 -- internal error
	4 -- user error. possibly a malformed json file
