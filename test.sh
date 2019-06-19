#!/usr/bin/env bash
# To run this, make sure you have dropboxfs already running.

set -CEeuo pipefail
IFS=$'\n\t'
shopt -s extdebug

MOUNTPOINT="$HOME/tmp/dbfs"
set -x

run_test() {
    NAME="$1"
    EXPECTED="$2"
    COMMAND="$3"

    DIFF=$(diff <(printf "%s\n" "$EXPECTED") <(printf "%s\n" "$COMMAND"))
    if [ -z "$DIFF" ]; then
        echo "PASSED $NAME"
    else
        echo "FAILED $NAME"
        echo "Diff:"
        echo "$DIFF"
        exit
    fi
}


echo "Runnning tests"

cleanup(){
  rm -rf "$MOUNTPOINT/testing"
}

main(){
  cleanup
  (
    cd "$MOUNTPOINT"

    set +e
    run_test "mkdir test dir" "" "$(mkdir "$MOUNTPOINT/testing")"
    echo 'cd into test dir'
    cd testing
    run_test "empty ls on new dir" "" "$(ls)"
    echo 'this is a test' > test.txt
    run_test "add new file" "test.txt" "$(ls)"
    run_test "cat new file" "this is a test" "$(cat test.txt)"
    run_test "cp file" "test.txt -> ./test-copied.txt" "$(cp -v test.txt ./test-copied.txt)"
    run_test "ls files" $'test-copied.txt\ntest.txt' "$(ls)"
    run_test "mv file" "test-copied.txt -> ./test-moved.txt" "$(mv -v test-copied.txt ./test-moved.txt)"
    run_test "ls files" $'test-moved.txt\ntest.txt' "$(ls)"
    run_test "cat moved & copied file" "this is a test" "$(cat test-moved.txt)"
    run_test "cat original file" "this is a test" "$(cat test.txt)"
    run_test "rm 1/2 files" "test-moved.txt" "$(rm -v test-moved.txt)"
    run_test "mkdir subdir" "" "$(mkdir subdir)"
    echo 'cd into subdir'
    cd subdir
    run_test "cp file down a level" "../test.txt -> ./sub.txt" "$(cp -v ../test.txt ./sub.txt)"
    run_test "ls subdir files" $'sub.txt' "$(ls)"
    run_test "cat copied subdir file" "this is a test" "$(cat sub.txt)"
    run_test "mv file up a level" "sub.txt -> ../super.txt" "$(mv -v sub.txt ../super.txt)"
    sleep 1
    run_test "cat moved subdir file" "this is a test" "$(cat ../super.txt)"
    echo 'cd back up to test dir'
    cd ../
    run_test "rmdir subdir" "" "$(rmdir subdir)"
    run_test "rm moved file" "super.txt" "$(rm -v super.txt)"
    run_test "ls one file" "test.txt" "$(ls)"
    run_test "rm last file" "test.txt" "$(rm -v test.txt)"
    run_test "ls no files" "" "$(ls)"
    echo 'cd back up to root'
    cd ../
    run_test "rmdir testing" "" "$(rmdir "$MOUNTPOINT/testing")"
    set -e

    echo "All tests passed"
  )
}

main "$@"
