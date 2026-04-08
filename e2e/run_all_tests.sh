#!/bin/bash

E2E="$(dirname "$(realpath "$0")")"

cd "${E2E}" || exit

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

mkdir -p ./artifacts/junit

for f in ./tests/*.bats; do
    test_name="$(basename "$f" .bats)"
    echo "=== Running: ${test_name} ==="

    # Check if bats supports JUnit reporting to avoid running tests twice
    bats_help="$(bats --help 2>&1)"
    if printf '%s\n' "$bats_help" | grep -q -- "--report-formatter" &&
        printf '%s\n' "$bats_help" | grep -q -- "junit"; then
        bats --report-formatter junit --output "./artifacts/junit" "$f"
    else
        bats "$f"
    fi
    retval=$?

    if [ "$retval" -ne 0 ]; then
        suite_failed=1
        kind_logs_dir="./artifacts/$(basename "$f" .bats).test/kind-logs"
        ./bin/kind export logs "$kind_logs_dir"
    fi
done

exit $suite_failed
