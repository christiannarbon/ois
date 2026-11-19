# Local cluster: `ois up` and `ois down`

`ois up` gives each project its own minikube cluster, and `ois down` stops or deletes it.
Both commands read `ois.yaml`. Run them from the project folder, or pass the file with `-f`.

## Prerequisites

You need `minikube`, `kubectl` and Docker on your `PATH`.

## Your current kube context never changes

The cluster is a minikube profile named `ois-<project>`, with a kube context of the same
name. ois starts minikube with `--keep-context` and passes `--context ois-<project>` to every
`kubectl` call. Whatever context you had selected before, for example a work cluster, stays
selected and is never used.

## `ois up`

```bash
ois up
```

`ois up` takes these steps:
1. Creates or starts the profile at the size in `ois.yaml`.
2. Turns on the `default-storageclass` and `storage-provisioner` addons.
3. Waits until the node and every `kube-system` pod are Ready (up to 5 minutes in total).
4. Checks that the cluster has exactly one default StorageClass.

Running it again on a running cluster is safe.

Sizes come from `cluster.size`:

| `cluster.size` | CPUs | Memory | Disk |
| :-- | :-- | :-- | :-- |
| `minimal` | 2 | 4096 MiB | 20g |
| `lean` (default) | 4 | 7168 MiB | 30g |
| `custom` | `cpus` | `memory` | `disk` |

### When the cluster no longer matches

minikube cannot change an existing cluster's CPUs, memory, Kubernetes version or driver.
If any of these differ from what ois asks for, `ois up` stops and explains why. This can
happen when you change `cluster.size`, when a new ois release pins a newer Kubernetes, or
when you set `OIS_MINIKUBE_DRIVER`. To start over:

```bash
ois up --recreate
```

This **deletes the cluster and all data in it** and creates a new one. ois asks first; pass
`--yes` to skip the question, for example in CI. Without a terminal and without `--yes`,
ois refuses. `--recreate` does nothing extra when the cluster already matches.

## `ois down`

```bash
ois down                # stop the cluster; data is kept for the next "ois up"
ois down --purge        # delete the cluster and all data in it (asks first)
ois down --purge --yes  # the same, without asking
```

If the cluster doesn't exist, `ois down` says so and exits successfully. If minikube
reports the profile as invalid, `ois down --purge` still deletes it.

## Profiles that target an existing cluster

`ois up` and `ois down` manage local minikube clusters only. For a profile with
`target: kubernetes`, both refuse before running anything. ois never creates, stops or
deletes a cluster it did not create.

## Options

| Option | Effect |
| :-- | :-- |
| `-v`, `--verbose` | Log every command and show minikube's own output while it starts |
| `--dry-run` | Print the commands instead of running them. Dry run can't see the existing cluster, so it always shows a fresh create |
| `--yes` | Answer yes to confirmation prompts |
| `OIS_MINIKUBE_DRIVER` | minikube driver to use (default `docker`) |
