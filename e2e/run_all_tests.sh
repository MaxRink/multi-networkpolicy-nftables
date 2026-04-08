#!/bin/bash

E2E="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"

pushd ${E2E}

suite_failed=0

cleanup_on_exit() {
    status="$1"
    signal="$2"

    if [ "$status" -ne 0 ] || [ "$suite_failed" -ne 0 ]; then
        echo "Test suite failed — collecting kind logs..."
        ./bin/kind export logs ./artifacts/suite-failure/kind-logs 2>/dev/null || true
    fi

    case "$signal" in
        INT)
            trap - INT
            exit 130
            ;;
        TERM)
            trap - TERM
            exit 143
            ;;
    esac
}
trap 'cleanup_on_exit $? EXIT' EXIT
trap 'cleanup_on_exit $? INT' INT
trap 'cleanup_on_exit $? TERM' TERM

for f in ./tests/*.bats; do
    echo "=== Running: $(basename $f) ==="
    bats $f
    retval=$?
    if [ $retval -ne 0 ]; then
        suite_failed=1
        ./bin/kind export logs ./artifacts/`basename $f`.test/kind-logs
    fi
done

exit $suite_failed
