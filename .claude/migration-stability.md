# KubeVirt VM Migration Stability

## Problem Statement

When a KubeVirt Virtual Machine (VM) is live-migrated from one node to another, the DRA (Dynamic Resource Allocation) driver needs to maintain access to the same shared directories across the migration. This presents several challenges:

### Challenge 1: Ephemeral Pod Names

KubeVirt creates virt-launcher pods with names that include random hashes:
```
virt-launcher-<vmi-name>-<pod-hash>-<template-name>-<claim-hash>
```

Example: `virt-launcher-vm-a-drz4j-dummy-gpu-fngjv`

Where:
- `vm-a` = VMI name (stable)
- `drz4j` = pod hash (changes on migration)
- `dummy-gpu` = template name (stable)
- `fngjv` = claim hash (changes on migration)

**Problem**: If we use the full claim name for directory paths, the path changes during migration, breaking access to shared data.

### Challenge 2: Device IDs Vary Across Nodes

Device IDs allocated by the DRA driver (e.g., `network-0`, `network-1`) may differ between nodes:
- Source node: VM gets `network-0`
- Destination node: VM might get `network-1` (if `network-0` is already allocated)

**Problem**: If the device ID is part of the directory path, the path becomes node-specific and doesn't survive migration.

## Solution: Migration-Stable Directory Structure

### Stable Claim Name Extraction

Extract only the stable portions of the claim name by removing ephemeral hashes:

```go
// Input:  "virt-launcher-vm-a-drz4j-dummy-gpu-fngjv"
// Output: "vm-a-dummy-gpu"
```

Algorithm:
1. Strip `virt-launcher-` prefix
2. Identify the pod hash (5-character alphanumeric string)
3. Remove the pod hash and claim hash (last segment)
4. Keep: `<vmi-name>-<template-name>`

### Device-Agnostic Directory Path

Remove the device ID from the directory path structure:

**Old structure** (migration-unsafe):
```
/var/run/kubevirt/cdi/{claim-name}/{request-name}/{device-id}/
```

**New structure** (migration-stable):
```
/var/run/kubevirt/cdi/{stable-claim-name}/{request-name}/
```

The device ID is now stored in a metadata file:
```json
{
  "device_id": "network-0"
}
```

### Complete Example

#### Before Migration (node02):
```
Claim: virt-launcher-vm-a-abc12-dummy-gpu-xyz89
Device: network-0
Path: /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/
File: /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/device.json
      {"device_id": "network-0"}
```

#### After Migration (node03):
```
Claim: virt-launcher-vm-a-def34-dummy-gpu-uvw56  (different hashes!)
Device: network-1                                 (different device!)
Path: /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/  (SAME PATH!)
File: /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/device.json
      {"device_id": "network-1"}                  (updated metadata)
```

The directory path remains consistent, allowing the VM to maintain access to its shared directory across migration.

## Implementation Details

### Directory Creation

When preparing a device claim:
1. Extract stable claim name from full claim name
2. Create directory: `{base}/{stable-claim}/{request}/`
3. Write metadata file with current device ID
4. Set ownership to qemu (107:107)
5. Set SELinux label to `container_file_t`

### Directory Cleanup

When unpreparing a device claim:
1. Look up claim in checkpoint (contains original claim name)
2. Extract stable claim name
3. Delete entire directory: `{base}/{stable-claim}/{request}/`
4. Removes both directory and metadata file

### CDI Mount Configuration

The CDI (Container Device Interface) mounts the stable directory path into containers:
```yaml
mounts:
  - hostPath: /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/
    containerPath: /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/
    options: ["rbind", "z"]
```

Environment variables are set for applications to discover the device:
```bash
KUBEVIRT_NETWORK_DEVICE=network-1
KUBEVIRT_NETWORK_PATH=/var/run/kubevirt/cdi/vm-a-dummy-gpu/network/
KUBEVIRT_NETWORK_REQUEST=network
```

## Cleanup Behavior

### Normal Termination
When a VM is deleted normally:
1. Kubelet calls DRA driver's `Unprepare()` method
2. Driver deletes the directory using stable claim name
3. Directory and metadata are removed

### Orphaned Directories
If the driver crashes or is not running when a VM is deleted:
- Directories may be left orphaned on the node
- These need manual cleanup or a periodic cleanup job
- Can be identified by checking for directories with no corresponding running VMs

## Testing Migration

To verify migration stability:

1. **Deploy a VM** with DRA network claim
2. **Check directory** on source node:
   ```bash
   ls -la /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/
   ```
3. **Trigger migration**:
   ```bash
   virtctl migrate <vm-name>
   ```
4. **Verify on destination node** the path is the same:
   ```bash
   ls -la /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/
   ```
5. **Check metadata** reflects new device ID:
   ```bash
   cat /var/run/kubevirt/cdi/vm-a-dummy-gpu/network/device.json
   ```
6. **Delete VM** and verify cleanup on destination node

## Key Learnings

1. **Stable identifiers are critical** - Use VMI name + template name, not pod-generated hashes
2. **Separate metadata from structure** - Store changing data (device IDs) in files, not paths
3. **Checkpoint tracking** - Store device-to-request mappings for proper cleanup
4. **Test the lifecycle** - Create, migrate, delete - ensure cleanup works in all scenarios
5. **Permission and labeling** - SELinux labels must be set for container access across migrations
