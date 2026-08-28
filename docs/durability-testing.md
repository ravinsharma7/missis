# Durability profiles and safe fault injection

**Current confirmed profile (2026-08-27):** the SQLite writer uses WAL with
`synchronous=NORMAL`. `missis show --health` reads the active pragmas and
reports this as `wal-normal`; it does not infer the profile only from source
code.

This profile preserves database consistency during ordinary recovery, but it
does not promise that the newest acknowledged transaction survives loss of
host power. SQLite documents that WAL + FULL adds the per-transaction WAL sync
that makes transactions durable across power loss. WAL + NORMAL omits that
sync for most transactions. See SQLite's
[`synchronous` pragma](https://www.sqlite.org/pragma.html#pragma_synchronous)
and [WAL documentation](https://www.sqlite.org/wal.html).

## Contract matrix

| Fault or operation | `wal-normal` guarantee | Confirmation state |
| --- | --- | --- |
| Missis process exits or is killed after a successful commit | SQLite recovers a consistent database; the acknowledged transaction is expected to remain because the host and filesystem continue running. | Confirmed by SQLite's contract; automated Missis crash-boundary coverage is still incomplete. |
| Operating system crashes or host power is lost | The database remains consistent after recovery, but one or more recently acknowledged transactions may be absent. | Confirmed contract limitation; actual laptop hardware behavior is intentionally not tested. |
| Torn, dropped, reordered, or corrupted storage writes | Outside the ordinary `wal-normal` guarantee. Recovery must detect the resulting integrity failure or use a verified backup. | Not confirmed end to end. |
| Backup/restore | Separate protocol with its own manifest, publication, and verification rules. | Covered separately by backup tests. |
| Replication to another authority | Not provided by the local durability profile. | Not implemented. |

“Commit succeeded” therefore means atomic acceptance under the active profile,
not replication and not strict host-power-loss durability. Ticket `#117` owns
a configurable strict profile and the benchmark needed before changing the
default.

## Safe fault-injection ladder

Climb this ladder one level at a time. A higher level is useful only after the
lower level produces a deterministic receipt and recovery check.

### 1. Process crash on the host

Use a new store under a test-created temporary directory. Run an append in a
child process, inject termination at controlled before-commit,
after-commit/before-response, and after-response points, then reopen the store
and check its chain, projection, idempotency receipt, and expected event count.

This is safe for laptop user data when the test resolves and validates its
temporary path before removal. It tests application/SQLite process failure. It
does **not** simulate kernel failure, write-cache loss, or power loss.

### 2. Hard power-off of a disposable virtual machine

Create a disposable VM with a separately attached disposable virtual disk.
Put only the test store on that disk. Take a clean snapshot, run a workload
that records request keys and acknowledged receipts outside the guest, then
use the hypervisor's hard power-off/reset operation. Reboot the guest, copy the
disk or store to a read-only recovery workspace, and compare acknowledged
receipts with the recovered ledger and consistency result.

This is the safest practical approximation on a developer laptop: only the
guest is power-cut. It still does not prove the behavior of the laptop's real
drive controller or filesystem.

### 3. Dropped/error/corrupted writes inside the VM

Inside the disposable VM only, place the test store on a disposable virtual
block device and interpose Linux `dm-flakey`. Its official documentation
defines `drop_writes`, `error_writes`, and targeted corruption features:
[Linux dm-flakey](https://docs.kernel.org/admin-guide/device-mapper/dm-flakey.html).
Run one fault mode at a time and preserve the failed image for read-only
analysis.

This level exercises hostile block behavior. It is not a realistic frequency
model, and a detected integrity incident is a valid outcome. Do not interpret
database consistency alone as proof that every acknowledged transaction
survived.

## Non-negotiable host safeguards

- Never run block-device fault injection against the host root disk, a physical
  device, the home directory, or the repository.
- Never grant a host container access to `/dev` for this test. Root privileges,
  device mapper, and destructive teardown stay inside the disposable VM.
- The harness must accept only an explicitly created virtual test disk, reject
  mounted root/home devices, cap disk size, and print the resolved target before
  enabling faults.
- Keep the acknowledgment log outside the guest. Analyze a copy or snapshot;
  do not repair the only failed image in place.
- Treat an actual laptop power cut as out of scope. It risks unrelated user
  data and gives less reproducible evidence than VM fault injection.

## Acceptance evidence for a strict profile

A future `wal-full` profile is confirmed only when all of the following exist:

1. health output reports the live WAL/FULL settings;
2. process-crash tests cover the commit/response ambiguity boundaries;
3. VM hard-reset trials recover every externally recorded acknowledged key;
4. injected-write failures either recover the receipt or surface a detectable
   integrity incident;
5. latency and throughput benchmarks state filesystem, storage, SQLite, and OS
   versions.

Until those checks exist, strict host-power-loss durability remains **not
confirmed**.
