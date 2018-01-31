#!/bin/bash

MOUNTPOINT=test

echo "Runnning tests"

cd $MOUNTPOINT

function Run_Test() {
    NAME=$1
    EXPECTED=$2
    shift
    shift
    COMMAND=$@

    DIFF=$(diff <(echo $EXPECTED) <(echo $COMMAND))
    if [ -z "$DIFF" ]; then
        echo "PASSED $NAME"
    else 
        echo "FAILED $NAME"
        echo "Diff:"
        echo $DIFF
        exit
    fi
}

Run_Test "mkdir test dir" "" $(mkdir testing)
echo 'cd into test dir'
cd testing
Run_Test "empty ls on new dir" "" $(ls)
echo 'this is a test' > test.txt
Run_Test "add new file" "test.txt" $(ls)
Run_Test "cat new file" "this is a test" $(cat test.txt)
Run_Test "cp file" "test.txt -> ./test-copied.txt" $(cp -v test.txt ./test-copied.txt)
Run_Test "ls files" "test-copied.txt test.txt" $(ls)
Run_Test "mv file" "test-copied.txt -> ./test-moved.txt" $(mv -v test-copied.txt ./test-moved.txt)
Run_Test "ls files" "test-moved.txt test.txt" $(ls)
Run_Test "cat moved & copied file" "this is a test" $(cat test-moved.txt)
Run_Test "cat original file" "this is a test" $(cat test.txt)
Run_Test "rm 1/2 files" "test-moved.txt" $(rm -v test-moved.txt)
Run_Test "mkdir subdir" "" $(mkdir subdir)
echo 'cd into subdir'
cd subdir 
Run_Test "cp file down a level" "../test.txt -> ./sub.txt" $(cp -v ../test.txt ./sub.txt)
Run_Test "ls subdir files" "sub.txt" $(ls)
Run_Test "cat copied subdir file" "this is a test" $(cat sub.txt)
Run_Test "mv file up a level" "sub.txt -> ../super.txt" $(mv -v sub.txt ../super.txt)
Run_Test "cat moved subdir file" "this is a test" $(cat ../super.txt)
echo 'cd back up to test dir'
cd ../
Run_Test "rmdir subdir" "" $(rmdir subdir)
Run_Test "rm moved file" "super.txt" $(rm -v super.txt)


Run_Test "ls one file" "test.txt" $(ls)
Run_Test "rm last file" "test.txt" $(rm -v test.txt)
Run_Test "ls no files" "" $(ls)
echo 'cd back up to root'
cd ../
Run_Test "rmdir testing" "" $(rmdir testing)

echo "All tests passed"
