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

TESTER_REPO=github.com/indigo-dc/watts-plugin-tester
TESTER=./watts-plugin-tester
CONFIG=test/config.json
plugin=target_plugin

setup_plugin() {
	echo "==> Obtaining WaTTS plugin from $TARGET_PLUGIN_REPO" >&2
	if [[ -d $plugin/.git ]]
	then git -C $plugin pull $TARGET_PLUGIN_REPO >&2 || exit
	else git clone $TARGET_PLUGIN_REPO $plugin >&2 || exit
	fi
	pushd $plugin >/dev/null
}

setup_plugin_tester() {
	if [[ -f $TESTER ]]
	then return
	fi

	echo '==> Building WaTTS plugin tester' >&2
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

	go get $TESTER_REPO >&2 || exit
	go build -o $TESTER $TESTER_REPO >&2 || exit
}

run_tests() {
	echo '==> Starting WaTTS plugin tester' >&2
	./watts-plugin-tester -m tests $CONFIG
}


clean_up() {
	echo
	echo '==> Cleaning up remaining files' >&2

	popd >/dev/null

	if [[ -d $plugin ]]
	then rm -rf $plugin
	fi
}

trap 'clean_up' EXIT

setup_plugin || exit
setup_plugin_tester || exit
run_tests || exit
