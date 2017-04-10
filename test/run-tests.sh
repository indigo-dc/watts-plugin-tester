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


setup_plugin() {
    echo '==>' "Obtaining WaTTS plugin from $TARGET_PLUGIN_REPO"
    if [[ -d plugin/.git ]]
    then git -C plugin pull $TARGET_PLUGIN_REPO || exit
    else git clone $TARGET_PLUGIN_REPO plugin || exit
    fi
    pushd plugin
    trap popd RETURN

    CONFIG=$(cat test/config.json | jq -cM .)

    CONFIG_INIT_CMD=$(echo $CONFIG | jq -r '.init_cmd')
    CONFIG_RUN_CMD=$(echo $CONFIG | jq -r '.run_cmd')
    CONFIG_TEST_DIR=$(echo $CONFIG | jq -r '.test_dir')

    [[ $CONFIG_INIT_CMD == null ]] && CONFIG_INIT_CMD=
    [[ $CONFIG_TEST_DIR == null ]] && CONFIG_TEST_DIR='test'
}

setup_plugin_tester() {
    echo '==>' "Building WaTTS plugin tester"
    ./utils/compile.sh || exit
}

test_plugin() {
    plugin=$1
    action=$2
    input_file=$3

    ./watts-plugin-tester test $plugin --plugin-action=$action --json=$input_file
}

run_tests() {
    trap 'rm -f found_at_least_one_input' EXIT

    find plugin/test -name "*_*_*.json" \
        | (while read input
           do
               keys=${input%.json}
               keys=${keys#plugin/test/}
               action=${keys%%_*}
               name=${keys%_*}
               name=${name#*_}
               expected_result=${keys##*_}

               echo "$input <=> $action _ $name _ $expected_result .json"

               [[ $action =~ request|revoke|parameter ]] || continue
               [[ $expected_result =~ fail|pass ]] || continue
               touch found_at_least_one_input

               echo '==>' "Running $action test $name with input '$input'"
               echo '==>' "Expecting test $name to $expected_result"


               test_plugin $action $input
               status=$?

               if [[ $status -eq 0 && $expected_result == fail ]]
               then
                   echo '==>' "But test $name passed (should have failed)"
                   exit 1
               elif [[ $status -ne 0 && $expected_result == pass ]]
               then
                   echo '==>' "But test $name failed with exit status $status (should have passed)"
                   exit $status
               else
                   echo '==>' "And test $name did $expected_result (as expected)"
               fi

           done) || exit

    if [[ ! -f found_at_least_one_input ]]
    then
        echo '==>' "No input files found"
    fi
}



setup_plugin_tester || exit
setup_plugin || exit
run_tests || exit
