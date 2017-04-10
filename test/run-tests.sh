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
    pushd plugin > /dev/null
    trap 'popd >/dev/null' RETURN

    CONFIG=$(cat test/config.json | jq -cM .)

    CONFIG_INIT_CMD=$(echo $CONFIG | jq -r '.init_cmd')
    CONFIG_EXEC=$(echo $CONFIG | jq -r '.exec_file')
    CONFIG_TEST_DIR=$(echo $CONFIG | jq -r '.test_dir')

    [[ $CONFIG_INIT_CMD == null ]] && CONFIG_INIT_CMD=
    [[ $CONFIG_TEST_DIR == null ]] && CONFIG_TEST_DIR='test'
    [[ $CONFIG_EXEC_FILE == null ]] && \
        echo '==> Invalid configuration in test/config_json: exec_file unset' >&2 && exit 1
}

setup_plugin_tester() {
    echo '==>' "Building WaTTS plugin tester"
    ./utils/compile.sh || exit
}

test_plugin() {
    plugin=$1
    action=$2
    input_file=$3
    expected=${4/pass/ok}
    expected=${expected/fail/error}
    name=$5

    list=$(cat test_results.json)
    output=$(./watts-plugin-tester test $plugin --plugin-action=$action --json=$input_file -m)
    if [[ $(echo $output | jq -r '.result') == $expected ]]
    then echo -n '.'
    else echo -n 'F'
    fi

    jq --null-input --compact-output \
       --argjson list "$list" \
       --argjson output "$output" \
       --arg expected "$expected" \
       --arg name "$name" \
       '$list+[$output+{expected_result:$expected, test_name:$name}]' \
       > test_results.json
}

run_tests() {
    trap 'rm -f found_at_least_one_input' EXIT
    echo '[]' > test_results.json

    echo '==> Starting tests'
    find plugin/test -name "*_*_*.json" \
        | (while read input
           do
               keys=${input%.json}
               keys=${keys#plugin/test/}
               action=${keys%%_*}
               name=${keys%_*}
               name=${name#*_}
               expected_result=${keys##*_}

               [[ $action =~ request|revoke|parameter ]] || continue
               [[ $expected_result =~ fail|pass ]] || continue
               touch found_at_least_one_input

               test_plugin plugin/$CONFIG_EXEC $action $input $expected_result $name
           done) || exit

    if [[ ! -f found_at_least_one_input ]]
    then
        echo '==>' "No input files found"
    else
        echo
    fi
}

report_results() {
    echo -n '==> Finished testing (fail/pass/total): '
    jq '[(map(select(.output.result!=.expected_result)) | length),
         (map(select(.output.result==.expected_result)) | length),
         length] | map(tostring) | join("/")' \
             -r test_results.json

    echo '==> Results of failed tests (if any)'
    jq 'map(select(.output.result!=.expected_result)) | .[]' \
       -r test_results.json \
        | jq -j '"==> "+.test_name+" returned "+.output.result+", but expected "+.expected_result+" ==> ",.'
    echo
}


setup_plugin_tester || exit
setup_plugin || exit
run_tests || exit
report_results || exit
[[ $(jq 'map(select(.output.result!=.expected_result)) | length == 0' -r test_results.json) == true ]]
