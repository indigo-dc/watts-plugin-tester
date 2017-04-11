watts-plugin-tester
====

get started
---
```
utils/compile.sh
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
	- does not work if the plugin prints arbitrary output


watts version support
---
- [x] 1.0.0 


interpretation of exit codes
---
	0 -- validation passed
	1 -- error with the plugin output / validation failed
	2 -- error executing the plugin
	3 -- internal error
	4 -- user error. possibly a malformed json file
