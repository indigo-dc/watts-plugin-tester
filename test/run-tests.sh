#!/bin/bash
#
# Test a WaTTS plugin using indigo-dc/watts-plugin-tester
#
# For documentation please consult: `test/README.md' in this repository
#
# Author: Joshua Bachmeier <uwdkl@student.kit.edu>
#


# Parameters (passed as environment variables)
: ${TARGET_PLUGIN_REPO:?"Environment variable unset"}

CONFIG=test/config.json

setup_plugin() {
	echo "==> Obtaining WaTTS plugin from $TARGET_PLUGIN_REPO" >&2
	if [[ -d plugin/.git ]]
	then git -C plugin pull $TARGET_PLUGIN_REPO &>/dev/null || exit
	else git clone $TARGET_PLUGIN_REPO plugin &>/dev/null || exit
	fi
}

setup_plugin_tester() {
	echo '==> Building WaTTS plugin tester' >&2
	PACKAGE=github.com/indigo-dc/watts-plugin-tester

	if ! which go >/dev/null
	then
		echo "go missing, please install go 1.5 or newer" >&2	
		exit 1
	fi

	if [[ -z $GOPATH ]]
	then
		export GOPATH=`pwd -P`/gopath
		echo using local GOPATH $GOPATH >&2
	fi

	go get $PACKAGE || exit
	go build -o watts-plugin-tester $PACKAGE || exit
}

run_tests() {
	echo '==> Starting tests' >&2
	cd plugin
	watts-plugin-tester -m tests $CONFIG
}



setup_plugin_tester || exit
setup_plugin || exit
run_tests || exit
