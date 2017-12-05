watts-plugin-tester
====

build
---
```
go get   github.com/indigo-dc/watts-plugin-tester

This will build the tester and install it at $GOPATH/bin
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
- machine readable json output
- complement the plugin input with (highest precendence first)
	- a json string
	- a json file
	- a watts.conf (only for the conf_params)
	
- validate the plugin input
- validate the plugin output
	- `check`: check that the json is conforming with the api of watts
	- `test`: do `check` and also compare the output with a provided expected output
	- `tests`: do multiple `test`s using a configuration file. See `test/` for an example
	
- generate a default json from the output of the plugins parameter action
- generate a default json with valid encoded fields, e.g. the watts_userid



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
