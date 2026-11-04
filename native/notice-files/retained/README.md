# Retained notice inputs

These files preserve the existing runtime notice bytes, verified against
`native/runtime-manifest.json`. They replace an external historical build
working directory as assembler inputs. No runtime file has changed.

Some inventories, package recipes and build receipts describe earlier
candidates. Their presence preserves historical provenance and notice context;
it does not describe the current installed package set or approve publication.
Use the current runtime manifest/provenance and component source records for
current identities. Do not regenerate these retained records from ambient host
state, use them as credentials, or treat their paths as required host paths.
