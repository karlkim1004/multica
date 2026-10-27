# Experimental claim fence primitive

This package is an isolated database experiment. It is not connected to the
Multica claim handler, daemon, installer, or migration system and must not be
used to authorize an update or restart.

The experiment serializes admission acquisition with a claim transaction for
one runtime. A claim operation and its result are committed together, so a
retry with the same operation ID returns the prior result even after a fence is
acquired. An unknown operation is refused while fenced. Assignment callbacks
must use only the supplied transaction and defer external side effects until
after commit. Concurrent owners cannot acquire the same fence.

The tests use a disposable PostgreSQL database and a synthetic task table. They
do not execute the real Multica handler, daemon, task runner, network failure,
or heartbeat. Without `NEX1499_TEST_DB_URL`, the tests skip. When set, it must
point to a database named `nex1499_isolated` on `127.0.0.1`; each test creates
and removes only a uniquely named schema. Store transactions explicitly use
READ COMMITTED so receipt reconciliation sees commits made while waiting for
the runtime row lock.

Before integrating this primitive, the server must move the actual task
selection, assignment, and receipt into the same transaction; enforce the
fence on every claim path including legacy clients and stale redelivery; bind
owners and operation IDs to authorized runtime/daemon identities; and define
retention and crash recovery. An external authenticated controller, durable
restart behavior, task/process drain evidence, safe fence release, and updater
mutual exclusion are not implemented. A receipt is not proof of local process
termination or of successful drain.
