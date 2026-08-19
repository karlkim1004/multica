#!/bin/sh
set -e

echo "Running database migrations..."
attempt=1
until ./migrate up; do
  if [ "$attempt" -ge 30 ]; then
    echo "Database migrations failed after ${attempt} attempts" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  echo "Database is not ready; retrying migrations (${attempt}/30)..." >&2
  sleep 1
done

echo "Starting server..."
exec ./server
