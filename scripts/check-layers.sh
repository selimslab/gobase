#!/usr/bin/env bash
# Verify the layering rules mechanically, independent of golangci-lint.
#
# The rule is about *direct* imports: a layer must not reach for a package it
# is forbidden to know about. Transitive dependencies of third-party libraries
# are not a layering violation — platform/obs uses the OTLP gRPC exporters,
# and gRPC itself speaks HTTP/2 through net/http. That says nothing about
# whether our code is transport-agnostic; `go list -deps` cannot tell the two
# apart, so this script inspects direct imports instead.
set -euo pipefail

fail=0

# imports_of prints every package the given packages import directly.
imports_of() {
  go list -f '{{range .Imports}}{{$.ImportPath}} {{.}}
{{end}}{{range .TestImports}}{{$.ImportPath}} {{.}}
{{end}}' "$@"
}

check() {
  local label="$1" pattern="$2"
  shift 2

  local hits
  hits=$(imports_of "$@" | grep -E " ${pattern}$" || true)

  if [ -n "$hits" ]; then
    echo "FAIL: ${label}"
    echo "$hits" | sed 's/^/  /'
    fail=1
  else
    echo "ok: ${label}"
  fi
}

check "platform does not import net/http" \
  'net/http' ./internal/platform/...

check "platform does not import the domain" \
  'github.com/selimslab/gobase/internal/domain' ./internal/platform/...

check "platform does not import adapters" \
  'github.com/selimslab/gobase/internal/adapters.*' ./internal/platform/...

check "domain does not import net/http" \
  'net/http' ./internal/domain/...

check "domain does not import database/sql" \
  'database/sql' ./internal/domain/...

check "domain does not import adapters" \
  'github.com/selimslab/gobase/internal/adapters.*' ./internal/domain/...

# The port inversion, stated as a dependency fact: the domain declares
# ExampleRepo and memstore implements it, so the arrow points inward.
if go list -deps ./internal/adapters/memstore | grep -qx 'github.com/selimslab/gobase/internal/domain'; then
  echo "ok: memstore implements a port declared in the domain"
else
  echo "FAIL: memstore no longer depends on the domain; the example inversion is broken"
  fail=1
fi

exit "$fail"
