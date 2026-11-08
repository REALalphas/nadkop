# Interrupted config/nodes save

Before replacing `nodes.json` and `config.json`, Nadkop writes a private file
`/etc/nadkop/.save-state.json`. It holds the protocol version and the SHA256 of the old/new pair;
passwords, URLs, HWID, and full copies of the configuration are never written there. Payload
size is under 400 bytes. One record plus two fsyncs are added ahead of the pair,
and after success the marker is removed plus a directory fsync.

On the next read, a complete old or new pair is accepted. When a
configuration lock is acquired, a leftover marker for such a pair is removed. A mixed pair,
unknown bytes, or a damaged marker cause `Interrupted save`; get,
save, source adding, validate, and generation do not continue. The current
Xray/nfqws generation does not change because of this error. Status stays available.

This is detection with manual recovery, not an automatic replay journal
and not a guarantee of overall file atomicity or survival of physical power loss.
A directory-fsync error after a config rename no longer rolls back only nodes
to the old version once a complete new pair is already visible. The result stays
unconfirmed, and the UI does not start service after such an error.

## Recovery

1. Over SSH, save copies of `config.json`, `nodes.json`, `servers.json`, and
   `.save-state.json` with private permissions. If saved server refs are needed,
   also restore the matching `servers.json`; for a full device copy
   keep HWID and geodatabases too.
2. Restore a consistent `config.json` and `nodes.json` from a single complete
   backup. Do not take nodes from another date/device separately from config.
3. If the old- or new-pair bytes named in the marker were restored, Nadkop
   recognises them on the next operation. For an older complete backup
   matching neither pair in the marker, delete the stale `.save-state.json`
   over SSH after manual verification.
4. Run `nadkop validate`, check diagnostics, then explicitly apply
   the configuration. Do not delete the marker to suppress the error before recovery.

Old complete backups without a marker keep working. The marker is included in keep.d
for sysupgrade; it is not a separate mandatory restore artifact.
Backup during save can still capture mixed files: run backup only
while no save is in progress. Without a marker, its old format does not promise
automatic recognition of every mix; the existing refs/compile checks
remain required. Do not restore a partial backup as a complete pair.

Check: `python3 scripts/test-x2-save-faults.py` uses only its own
files under `build/improvements-X/x2-implementation/save-faults`. 12 scenarios
cover SIGKILL before each of the three renames, kill after config rename, six
fsync EIOs, config-rename failure, and baseline. This is in-process syscall injection,
not a flash or router-power-loss test.
