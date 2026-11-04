#!/bin/sh
set -eu
test "${TEST_RESULT:-}" = success
test "${DEPLOY_RESULT:-}" = success
case "${EVENT_NAME:-}" in
    push) test "${REF_RESULT:-}" = success ;;
    pull_request|workflow_dispatch) test "${REF_RESULT:-}" = skipped ;;
    *) exit 1 ;;
esac
