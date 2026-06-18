# Troubleshooting CDI Directory and Environment Variable Visibility in Pods

## Problem
CDI directories and environment variables created by the DRA driver were not visible in virt-launcher pods, even though the driver was successfully preparing claims and creating CDI spec files.

## Root Causes (Multiple Compounding Issues)

### 1. Directory Path Conflict
- **Issue**: Original path `/var/run/kubevirt/network` conflicted with existing tmpfs mount
- **Symptom**: CDI bind mount failed, CRI-O created tmpfs instead
- **Solution**: Changed to `/var/run/kubevirt/cdi` with hierarchical structure

### 2. Missing Volume Mount in Driver DaemonSet
- **Issue**: DRA driver created directories but they weren't visible to container runtime
- **Symptom**: Directories existed on host but not accessible for mounting into containers
- **Solution**: Added volume mount with Bidirectional propagation:
```yaml
- name: kubevirt-cdi
  mountPath: /var/run/kubevirt/cdi
  mountPropagation: Bidirectional
```

### 3. Incorrect Directory Permissions
- **Issue**: `os.MkdirAll(dir, 0775)` was affected by umask, resulted in 0755
- **Symptom**: Group couldn't write even though ownership was correct
- **Solution**: Added explicit `os.Chmod(dir, 0775)` after directory creation

### 4. Wrong Directory Ownership
- **Issue**: Directories created by driver running as root
- **Symptom**: Containers running as uid 107 (qemu) couldn't access
- **Solution**: Added `os.Chown(dir, 107, 107)` for all directories in path

### 5. Incorrect SELinux Label
- **Issue**: Directory labeled `container_var_run_t:s0` (no MCS categories)
- **Symptom**: Containers run with `container_t:s0:c431,c834` (with MCS), SELinux MCS isolation prevented access
- **Solution**: Set label to `container_file_t:s0` using `selinux.SetFileLabel()`
- **Note**: CDI `:z` option didn't work (CRI-O didn't relabel), had to set in code

### 6. SELinux Library Quirk
- **Issue**: `selinux.GetEnabled()` returned false even with SELinux enabled
- **Solution**: Removed `GetEnabled()` check, just call `SetFileLabel()` directly and log errors

## Key Technical Insights

### Directory Path Structure
Changed from flat to hierarchical:
- Before: `/var/run/kubevirt/network/{claim-name}-{device-name}`
- After: `/var/run/kubevirt/cdi/{claim-name}/{request-name}/{device-name}`

Benefits:
- Includes request name for proper cleanup tracking
- Avoids name collisions
- Clearer organization

### SELinux Context Requirements
- Container processes run with: `container_t:s0:cXXX,cYYY` (with MCS categories)
- Shared files must use: `container_file_t:s0` (no MCS categories)
- This allows multiple containers to access the same files
- The `:z` mount option in CDI specs didn't work; explicit `SetFileLabel()` was required

### Mount Propagation
- `Bidirectional` propagation is critical for DRA drivers
- Allows mounts created in driver's namespace to be visible to kubelet/CRI-O
- Without it, directories exist but can't be bind-mounted into pods

### Checkpoint Data for Cleanup
Added `DeviceRequests map[string]string` to track device→request mapping:
```go
type PreparedClaim struct {
    UID            types.UID
    Name           string
    Devices        []string
    DeviceRequests map[string]string  // device name → request name
}
```
This enables proper cleanup of hierarchical paths during unprepare.

## Complete Fix Pattern

When creating directories for DRA/CDI:

```go
const qemuUID = 107
const qemuGID = 107

// 1. Create directory structure
if err := os.MkdirAll(claimDir, 0755); err != nil {
    return "", err
}

// 2. For each directory in the path:
for _, dir := range []string{claimRoot, requestDir, claimDir} {
    // Set permissions (0775 for group write)
    if err := os.Chmod(dir, 0775); err != nil {
        return "", fmt.Errorf("failed to chmod %s: %w", dir, err)
    }
    
    // Set ownership to container user/group
    if err := os.Chown(dir, qemuUID, qemuGID); err != nil {
        return "", fmt.Errorf("failed to chown %s: %w", dir, err)
    }
    
    // Set SELinux label for shared container access
    if err := selinux.SetFileLabel(dir, "system_u:object_r:container_file_t:s0"); err != nil {
        klog.Infof("Could not set SELinux label on %s: %v", dir, err)
    }
}
```

## Verification Steps

1. **Check directory exists and has correct attributes:**
```bash
ls -lZ /var/run/kubevirt/cdi/{claim}/{request}/{device}
# Should show: drwxrwxr-x. 107 107 container_file_t:s0
```

2. **Check CDI spec was created:**
```bash
cat /var/run/cdi/network.example.com-{device}.yaml
```

3. **Check environment variables in pod:**
```bash
kubectl exec -it {pod} -c {container} -- env | grep KUBEVIRT_NETWORK
```

4. **Test write access from container:**
```bash
kubectl exec -it {pod} -c {container} -- touch {path}/testfile
```

## Files Modified

- `deployments/manifests/daemonset.yaml` - Added volume mount with Bidirectional propagation, changed imagePullPolicy
- `internal/profiles/network/network.go` - Changed directory structure, updated CDI mount spec
- `cmd/dra-example-kubeletplugin/state.go` - Added permissions, ownership, SELinux labeling
- `internal/api/checkpoint/types.go` - Added DeviceRequests map for cleanup tracking

## Lessons Learned

1. **Multiple small issues compound**: Each issue alone (permissions, ownership, SELinux) could prevent access
2. **Test each layer independently**: mkdir → relabel → mount
3. **SELinux is subtle**: MCS categories, file type context, and relabeling timing all matter
4. **Docker build cache**: Use `--no-cache` when troubleshooting to ensure fresh builds
5. **imagePullPolicy matters**: Use `Always` during development to avoid cached images
6. **Mount propagation is critical**: DRA drivers need `Bidirectional` to share mounts with kubelet
