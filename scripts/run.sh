#!/usr/bin/env bash

if [[ ! -d /data ]]; then
  mkdir -p /data
fi

cd /data

RUNNER_STATE_FILE=${RUNNER_STATE_FILE:-'.runner'}

CONFIG_ARG=""
if [[ ! -z "${CONFIG_FILE}" ]]; then
  CONFIG_ARG="--config ${CONFIG_FILE}"
fi
EXTRA_ARGS=""
if [[ ! -z "${GIT_RUNNER_LABELS}" ]]; then
  EXTRA_ARGS="${EXTRA_ARGS} --labels ${GIT_RUNNER_LABELS}"
fi
if [[ ! -z "${GIT_RUNNER_EPHEMERAL}" ]]; then
  EXTRA_ARGS="${EXTRA_ARGS} --ephemeral"
fi
RUN_ARGS=""
if [[ ! -z "${GIT_RUNNER_ONCE}" ]]; then
  RUN_ARGS="${RUN_ARGS} --once"
fi

# In case no token is set, it's possible to read the token from a file, i.e. a Docker Secret
if [[ -z "${GIT_RUNNER_REGISTRATION_TOKEN}" ]] && [[ -f "${GIT_RUNNER_REGISTRATION_TOKEN_FILE}" ]]; then
  GIT_RUNNER_REGISTRATION_TOKEN=$(cat "${GIT_RUNNER_REGISTRATION_TOKEN_FILE}")
fi

test -f "$RUNNER_STATE_FILE" || echo "$RUNNER_STATE_FILE is missing or not a regular file"

if [[ ! -s "$RUNNER_STATE_FILE" ]]; then
  try=$((try + 1))
  success=0

  # The point of this loop is to make it simple, when running both git-runner and gitea in docker,
  # for the git-runner to wait a moment for gitea to become available before erroring out.  Within
  # the context of a single docker-compose, something similar could be done via healthchecks, but
  # this is more flexible.
  while [[ $success -eq 0 ]] && [[ $try -lt ${GIT_MAX_REG_ATTEMPTS:-10} ]]; do
    git-runner register \
      --instance "${GIT_INSTANCE_URL}" \
      --token    "${GIT_RUNNER_REGISTRATION_TOKEN}" \
      --name     "${GIT_RUNNER_NAME:-`hostname`}" \
      ${CONFIG_ARG} ${EXTRA_ARGS} --no-interactive 2>&1 | tee /tmp/reg.log

    cat /tmp/reg.log | grep 'Runner registered successfully' > /dev/null
    if [[ $? -eq 0 ]]; then
      echo "SUCCESS"
      success=1
    else
      echo "Waiting to retry ..."
      sleep 5
    fi
  done
fi
# Prevent reading the token from the git-runner process
unset GIT_RUNNER_REGISTRATION_TOKEN
unset GIT_RUNNER_REGISTRATION_TOKEN_FILE

exec git-runner daemon ${CONFIG_ARG} ${RUN_ARGS}
