# Physical writer handoff: prerequisites and bounded candidate

Status: design/preflight only. No policy has been applied, no legacy writer has
been stopped, and no production readiness producer is enabled. The read-only
inventory is recorded in the parent plan's
`tasks/43/physical-writer-fence-inventory-20261103.json`.

## Observed deployment

On 2026-11-03 JST the live legacy Deployment was `scanservjs/scanservjs`, one
replica with Recreate, with a Running/ready non-hostNetwork Pod. Cilium was pinned
to 1.20.3 with three agents ready; configuration reported default policy mode,
host firewall false and IPv6 false. No NetworkPolicy/CiliumNetworkPolicy was
returned in the legacy namespace, and no clusterwide Cilium policy was returned.
These are configuration observations, not datapath enforcement or process-stop
proofs. Other namespaces, host processes, LAN clients and device protocols have
not been exhaustively inventoried.

### Expanded path inventory (2026-11-03)

Read-only cluster queries found 204 Pods, including 20 with hostNetwork, 45
Kubernetes NetworkPolicies, five CiliumNetworkPolicies and no clusterwide Cilium
policy. These counts do not establish which Pods can reach the device: selector,
effective policy and datapath checks are still required. Name/image matches also
included the scanner portal, Stirling and backup maintenance Jobs; a name match
is not evidence of a physical scanner writer. The legacy Deployment has neither
explicit SANED_NET_HOSTS/PIXMA_HOSTS values nor envFrom references; this does not
exclude built-in backends or discovery through other configuration.

The Windows host has one Canon TS8130 series XPS printer queue using a WSD port.
Get-PrinterPort returned no PrinterHostAddress, PortNumber or Protocol for that
port. Local registry/PnP property-name inspection did not establish an endpoint.
This is a configured, model-matching queue, not proof that it currently targets
the same canonical physical device or that printing succeeds. No queue, driver,
port, registry value or policy was changed, and no print job was requested.

[Microsoft's WSDMON description](https://learn.microsoft.com/en-us/windows-hardware/drivers/print/wsdmon-port-monitor)
confirms that WSD is a network printing transport with bidirectional status
support. It does not supply the actual endpoint/ports for this installation.
Do not infer that TCP80 is unrelated to printing or replace WSD with another
transport merely to simplify fencing.

The parent task retains `physical-writer-paths-kubernetes-20261103.json`,
`physical-writer-policy-inventory-20261103.json`,
`physical-writer-printing-windows-20261103.json`, the WSD/PnP property-name records
and `physical-writer-legacy-transports-20261103.json`. No secret references were
resolved, device discovery traffic or direct device probes were requested, and
no print-job/document metadata was collected.

Before extending the candidate, bind the Windows queue to the device identity
and identify its real print/status/discovery endpoints. Inventory Windows/LAN
scan-capable clients and host processes separately. Preserve both printing and
status behavior, then verify each applicable policy on the actual source path.
None of this inventory enables the production readiness publisher.

### Registered-device read-only attempt (2026-11-03)

The product eSCL identity observer attempted one bounded GET of the registered
ScannerCapabilities endpoint from WSL and returned no identity observation. A
separate no-proxy/no-redirect HTTP diagnostic received no HTTP response. The
local route selected eth0 with an on-link address, but the neighbor entry was
FAILED. One bounded GET from the existing scanservjs Pod also returned
EHOSTUNREACH. The child process had an empty environment and performed no scan
creation, native backend invocation, file write or service restart.

These results establish failure to reach the registered address at the observed
times; they do not distinguish power-off, link/address changes or network
filtering. They do not test UUID compatibility, native capture, Idle, local stop
or fencing. No readiness or lease state was published or released. The operator
was asked to confirm current power/LAN status; no answer is assumed.

The parent task's `real-device-identity-readonly-20261103/` retains the product
probe source/result, size-limited HTTP diagnostic, passive route/neighbor summary
and Pod diagnostic. Raw XML, UUIDs and document/job metadata were not retained.
Once connectivity is established, repeat non-acquiring identity checks before
continuing the separate physical ownership and prepared-original acceptance.

The registered airscan transport in homelab source is HTTP eSCL on TCP80. Actual
printing protocols/ports are not established by that source. The new Helm design
co-locates API and worker in one Pod; Pod network policy cannot distinguish these
two containers. Do not describe a policy allowing that Pod as worker-only control.

## Candidate and limits

[`legacy-escl-candidate.yaml`](../../deploy/fencing/legacy-escl-candidate.yaml)
selects only the known legacy Pod labels in its namespace and denies TCP80 to
the registered scanner IPv4 address. It deliberately disables that legacy eSCL
path, including read-only capabilities/status. It has no Helm/Compose/GitOps
reference and is not a normal installation step.

Both default-deny switches are false so the candidate does not introduce an
unrelated default-deny policy for selected Pods. Cilium deny rules take precedence
over allow rules; a later broad allow is not the rollback for this deny. Cilium
deny does not support HTTP path/method filtering. These behaviors must be checked
against the installed agent version and realized policy before cutover.

This candidate does not block a different label, namespace, scanner address,
alternate protocol, hostNetwork process or LAN client. It cannot stop an already
accepted hardware job or prove that existing connections have been revoked.
Blocking the entire scanner IP instead is not accepted: preserving printing is a
requirement, and its actual flows must be identified first. Kubernetes desired
state, a policy UID/resourceVersion, a Pod deletion acknowledgement or Idle alone
cannot be promoted to physical exclusivity.

## Readiness producer integration conditions

1. Inventory managed and unmanaged writer paths, aliases, device addresses and
   scanner protocols, plus actual printing flows to preserve. Establish which
   authority can revoke each path and prevent recreation by its controller.
2. Prepare a concrete fence covering those paths. The Cilium candidate covers
   only the known legacy Pod transport. Host/LAN paths require their own proven
   control or exclusion; enabling host firewall globally is a separate change.
3. Quiesce admissions and drain/cancel old work. Verify local process termination
   on every relevant node; an unreachable old node requires external fencing,
   never forced Pod deletion as a stop certificate. Preserve unresolved jobs,
   leases, reservations and original bytes.
4. Verify enforcement on all relevant paths/nodes, including existing connections,
   restart/recreation and policy loss. Use non-acquiring probes first. A denied
   request must be attributed to the fence, not an offline scanner or bad route.
   Confirm the allowed new application path separately. Actual acquisition tests
   require the existing physical acceptance gate and prepared originals.
5. Bind the verified authority to the worker boot, canonical physical identity,
   device and lease epoch. Its lifetime must cover capture and shutdown; loss
   closes admission, cancels local work and retains uncertainty until reconciled.
   No browser field, environment boolean or operator-supplied timestamp substitutes
   for observing this authority. The runtime currently has no such provider.
6. Only after authority and joined local stop may a fresh matching identity/Idle
   observation feed the existing transactional readiness publisher. Refresh after
   each consumed epoch and on expiry; metadata heartbeat cannot renew it.

Required acceptance includes legacy/new writer contention, stale controller/Pod
recreation, old-node partition, fence loss during capture, alias/address changes,
already-open sessions, preserved printing, stop/idle ordering and safe rollback.
Rollback first stops/quiesces the new writer and resolves hardware activity,
then removes the exact candidate fence and restores the old controller. Restoring
old access while the new writer is active is not an accepted rollback.

## Primary references

Checked 2026-11-03. The online stable docs currently describe 1.20.4, while the
observed cluster is 1.20.3; offline validation uses the installed CRD schema and
does not establish identical runtime behavior between those versions.

The candidate also passed strict Kubernetes server dry-run. A subsequent GET
returned NotFound, confirming that it was not persisted. The response/hash and
postcheck are recorded in `physical-writer-fence-server-dry-run-20261103.json` in
the parent task directory. This validates API admission, not Cilium enforcement.

- [Policy enforcement and default-deny controls](https://docs.cilium.io/en/stable/security/policy/intro/)
- [Deny precedence and lack of L7 deny](https://docs.cilium.io/en/stable/security/policy/deny/)
- [Host policy scope and audit-mode limitations](https://docs.cilium.io/en/stable/security/host-firewall/)
- [Readiness admission](scanner-readiness.md) and [identity binding](scanner-identity.md)


### Latest bounded recheck

A subsequent product identity GET and independent no-proxy/no-redirect HTTP GET
again failed to reach the registered address from WSL (errno 113). A dedicated
read-only, pinned-host-key router SSH attempt timed out waiting for a prompt;
no complete ARP reply was obtained, and the harness did not record whether the
intended `show arp` command had been sent. The receipt is interpreted as an
unavailable observation, not an empty ARP table, device power diagnosis or fence
proof. No unrelated client rows or credential values were retained. The owned
SSH session was stopped; no router/scanner configuration, readiness, lease,
legacy service or print flow was changed. Evidence is retained under the parent
plan's `physical-readonly-recheck-20261103/`.

### Connectivity restored; prepared-original preflight

On 2026-11-03 the owner confirmed power/LAN and a prepared original. A fresh
product identity observation succeeded. The latest standalone 5.5.4 candidate
was extracted and its actual bundled native tools passed the existing supervised
read-only `check-device.py` on WSL, including selected parameters and local
probe-stop/cleanup. A4 gray/color at 300 dpi returned 2480 × 3508.
The legacy Deployment was still one ready replica; its point-in-time process
inspection found no matching native scanner/OCR process names. No acquisition,
legacy stop, policy change or durable readiness/lease mutation occurred.
These observations supersede earlier connectivity failures only; they do not
establish exclusive ownership. Prepared-original evidence is retained in the
parent task `real-device-prepared-original-20261103/`.

## Accepted application scope (2026-11-04 owner decision)

The owner selected one application writer plus detection of another application's
busy device. LAN-wide exclusion and the optional Cilium fence above are outside
this acceptance scope. Printing and CUPS remain unchanged. The historical fence
inventory is retained as evidence, not a required installation procedure.

For this scope the existing shared-document-root worker guard, coordinator boot
and transactional device epoch own application admission. A readiness refresh
withdraws its previous advertisement before checking the idle lease, then the
session serializes a fresh registered UUID/Idle/UUID observation with local native
operations. The UUID is pinned for the session; changed identity or uncertain
local termination cannot be repaired by polling Idle. Publication must still
compare the observed idle epoch and the live guard/boot in its transaction.

The guard covers this deployment sharing the same document root, not independent
installations or LAN clients. External clients may start after an Idle observation;
native busy/error handling remains necessary at actual acquisition. No readiness
record claims prevention of those external races, revokes an external job, or
releases a held/reconciling lease. A successor worker retains the existing startup
reconciliation gate; clock expiry alone never authorizes a second scan.
