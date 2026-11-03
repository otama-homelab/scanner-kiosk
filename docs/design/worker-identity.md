# Local worker identity and coordinator exclusion

`internal/workeridentity.Acquire` now creates a fresh 192-bit random worker boot
nonce and holds a nonblocking exclusive `flock` on a private local root. It rejects
unsafe roots, final lock symlinks, non-private/non-regular files and an already-active
local coordinator. The lock file is retained after close: unlinking it while another
process might still use it would permit independent locks on different inodes.

The nonce is process-lifetime state, never a stable environment setting or restored
identity. It stays constant while the guard is active; a closed guard no longer
returns an ID. A successor acquisition always receives a new nonce. Production
startup must acquire the guard before initializing worker execution and drain/join
owned work before closing it.

This is local coordinator exclusion, not distributed fencing or readiness. A crash
can release the kernel lock while a native child or physical scanner still runs.
Acquisition therefore never marks a device idle, releases a reservation/lease or
marks a worker ready. The successor must reconcile owned process identities and
hardware before accepting work. Existing scanservjs does not cooperate with this
lock and still requires the explicit production ownership handoff. NFS/multiple
replica deployment and administrator path/mount replacement are not protected.

Tests exercise exclusion within one process and between independent processes,
forced coordinator death, fresh nonce after restart, private modes, unsafe roots
and retained lock inode. An integration uses actual generated guard IDs with scan
admission and shows that a successor cannot claim an old queued physical scan;
the device lease and reservation remain held for reconciliation. No hardware,
child-process teardown, durable readiness projection, executable worker loop or
complete startup recovery is established by these tests.
