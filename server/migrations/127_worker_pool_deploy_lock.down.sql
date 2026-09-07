ALTER TABLE worker_pool_lock DROP CONSTRAINT worker_pool_lock_resource_type_check;
ALTER TABLE worker_pool_lock
    ADD CONSTRAINT worker_pool_lock_resource_type_check
    CHECK (resource_type IN ('issue', 'file'));
