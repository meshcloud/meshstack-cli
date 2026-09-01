#!/usr/bin/env bash
#
# Gate the acceptance job on meshfed-api being ready. Used by .github/workflows/acceptance.yml.
#
# meshfed-api is the only service worth waiting for: it bootstraps API keys into Keycloak, so tests
# that start before it is healthy fail with auth errors, and it is the only backend the CLI talks to.
#
# Port 8180 is meshfed-api's *actuator* (management) port, not its app port (8089): meshfed sets
# management.server.port, which takes /actuator/health off the app port entirely. Probing the app port
# instead reaches the application's own filter chain, which answers 401 — indistinguishable from
# "not ready yet", so the gate would wait out the whole deadline on a healthy container.
#
# ARC runs the job in kubernetes container mode, where the job container and the service containers
# share a network namespace, so the service is reachable on localhost. That mode also means there is
# no docker or kubectl in the job container: the service container's own logs are unreachable, and its
# HTTP response is the only window into why it is unhealthy. Hence the diagnostics below, which print
# the status and body rather than discarding them.
set -euo pipefail

URL=http://localhost:8180/actuator/health
DEADLINE=$((SECONDS + 420))

# Called only on timeout, so a green run stays quiet. Separates the two failure modes the readiness
# gate otherwise reports identically: no HTTP response at all means the container is down or
# crash-looping (e.g. the OOM-kill the service heap caps in the workflow guard against), while a
# 4xx/5xx means the process is up — and the actuator body names the component that is unhealthy.
diagnose_backend() {
  echo "::group::backend diagnostics"
  local out rc=0
  out=$(curl -s -m 10 -w $'\n%{http_code}' "$URL" 2>&1) || rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "  no HTTP response (curl exit $rc): container is down, restarting, or not listening"
  else
    echo "  HTTP ${out##*$'\n'}: $(head -c 1500 <<<"${out%$'\n'*}")"
  fi
  # Service containers share the pod's memory budget, so record the headroom: a container killed for
  # overcommitting is the documented cause of a truncated acceptance run.
  echo "  job container memory (bytes):"
  echo "    max=$(cat /sys/fs/cgroup/memory.max 2>/dev/null || echo n/a)" \
    "current=$(cat /sys/fs/cgroup/memory.current 2>/dev/null || echo n/a)" \
    "peak=$(cat /sys/fs/cgroup/memory.peak 2>/dev/null || echo n/a)"
  echo "  node memory:"
  free -m 2>/dev/null | sed 's/^/    /' || echo "    free(1) unavailable"
  echo "::endgroup::"
}

echo "waiting for meshfed-api ($URL)..."
while true; do
  # Capture the status instead of relying on curl's exit code, so a timeout can report what the
  # endpoint last answered. curl already writes 000 for a connection-level failure, so only an empty
  # result needs covering.
  status=$(curl -s -o /dev/null -m 10 -w '%{http_code}' "$URL" 2>/dev/null || true)
  if [ "${status:-000}" = 200 ]; then
    echo "meshfed-api healthy"
    break
  fi
  if [ "$SECONDS" -gt "$DEADLINE" ]; then
    echo "::error::timeout waiting for meshfed-api ($URL), last status HTTP ${status:-000}"
    diagnose_backend
    exit 1
  fi
  sleep 3
done
