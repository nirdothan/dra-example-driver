# DRA Driver Verification Skill

This skill performs comprehensive verification of the DRA driver deployment and functionality.

## What This Skill Does

Verifies the deployed DRA driver by checking:

1. **Driver Registration & Operation**
   - Driver name and version
   - gRPC sockets (DRA and registration)
   - Device advertisement in ResourceSlices
   - Driver logs for errors

2. **Resource Allocation**
   - DeviceClass configuration
   - ResourceSlice creation across nodes
   - Active ResourceClaims (if any)
   - Device allocation details

3. **Directory Structure & Permissions**
   - Base directory creation
   - Claim-specific directories
   - Migration-stable naming
   - Device metadata files

4. **Security Configuration**
   - File ownership (qemu 107:107)
   - Permissions (775 for directories)
   - SELinux labels (container_file_t)

5. **CDI Configuration**
   - CDI spec files
   - Device injection configuration

6. **Persistence**
   - Checkpoint files
   - Claim tracking

## Usage

```bash
# Run from repository root
./.claude/skills/verify-driver.sh

# Or let Claude invoke it via the skill system
```

## When to Use

- After initial deployment
- After driver updates or redeployments
- When troubleshooting issues
- Before running tests with VMs or pods
- To verify migration-stable directory naming
- To check if ResourceClaims are properly prepared

## Expected Output

The script provides:
- ✓/✗ status for each verification check
- Detailed information about allocated resources
- List of any issues found
- Summary of driver health

## Verification Steps

### 1. Driver Registration Check

```bash
# Check driver pods
kubectl get pods -n dra-network-driver

# Verify sockets on a node
POD=$(kubectl get pods -n dra-network-driver -o name | head -1 | cut -d/ -f2)
kubectl exec -n dra-network-driver $POD -- ls -la /var/lib/kubelet/plugins/hostpath.network.com/
kubectl exec -n dra-network-driver $POD -- ls -la /var/lib/kubelet/plugins_registry/ | grep hostpath
```

### 2. Device Advertisement Check

```bash
# Check DeviceClass
kubectl get deviceclass

# Check ResourceSlices
kubectl get resourceslices

# Verify device names
kubectl get resourceslice -o jsonpath='{.items[0].spec.devices[*].name}'
```

### 3. Resource Allocation Check

```bash
# List active claims
kubectl get resourceclaims --all-namespaces

# Check claim details (if any exist)
kubectl get resourceclaim <claim-name> -o yaml
```

### 4. Directory Structure Check

```bash
# Find node with active claim
NODE=$(kubectl get resourceclaim <claim-name> -o jsonpath='{.status.allocation.devices.results[0].pool}')
DRA_POD=$(kubectl get pods -n dra-network-driver -o wide | grep $NODE | awk '{print $1}')

# Check directory structure
kubectl exec -n dra-network-driver $DRA_POD -- ls -laR /var/run/kubevirt/cdi/

# Check device metadata
kubectl exec -n dra-network-driver $DRA_POD -- cat /var/run/kubevirt/cdi/<claim-dir>/<request>/device.json

# Check permissions and SELinux
kubectl exec -n dra-network-driver $DRA_POD -- stat -c "Perms: %a Owner: %u:%g SELinux: %C" /var/run/kubevirt/cdi/<claim-dir>/<request>
```

### 5. Checkpoint Verification

```bash
# Check checkpoint on the node
kubectl exec -n dra-network-driver $DRA_POD -- cat /var/lib/kubelet/plugins/hostpath.network.com/checkpoint.json
```

### 6. CDI Configuration Check

```bash
# List CDI specs
kubectl exec -n dra-network-driver $DRA_POD -- ls -la /etc/cdi/

# View CDI spec content
kubectl exec -n dra-network-driver $DRA_POD -- cat /etc/cdi/k8s.hostpath.network.com-hostpath_common.yaml
```

### 7. Log Analysis

```bash
# Check for errors in driver logs
kubectl logs -n dra-network-driver -l app=dra-network-driver --tail=100 | grep -i "error\|fatal\|panic"

# View recent activity
kubectl logs -n dra-network-driver -l app=dra-network-driver --tail=50
```

## Success Criteria

A healthy driver should show:

- ✓ All DaemonSet pods running (1 per node)
- ✓ Driver sockets created (dra.sock, registration socket)
- ✓ DeviceClass `hostpath.network.com` exists
- ✓ ResourceSlices created on all nodes
- ✓ Devices named `hostpath-0` through `hostpath-9` advertised
- ✓ No error/fatal/panic messages in logs
- ✓ Base directory `/var/run/kubevirt/cdi/` exists
- ✓ CDI common spec file created

If ResourceClaims are active:
- ✓ Checkpoint file exists with claim tracking
- ✓ Claim directory created with migration-stable naming
- ✓ Device metadata file (device.json) present
- ✓ Permissions: 775, Owner: 107:107
- ✓ SELinux label: container_file_t:s0
- ✓ CDI spec file for claim exists

## Troubleshooting Common Issues

### Driver Not Registered
**Symptom**: No registration socket found
**Check**: 
```bash
kubectl logs -n dra-network-driver <pod> | grep -i "registrar\|registration"
```
**Common causes**: Kubelet plugin directory misconfigured, RBAC issues

### Devices Not Advertised
**Symptom**: No ResourceSlices or empty device list
**Check**:
```bash
kubectl logs -n dra-network-driver <pod> | grep -i "resourceslice\|enumerat"
```
**Common causes**: Profile initialization failed, insufficient permissions

### Directory Not Created
**Symptom**: No claim directories under /var/run/kubevirt/cdi/
**Check**:
```bash
kubectl describe resourceclaim <claim-name>
kubectl logs -n dra-network-driver <pod> | grep -i "prepare\|directory"
```
**Common causes**: Claim not prepared yet, node mismatch, driver error

### Permission Denied
**Symptom**: VM cannot write to directory
**Check**:
```bash
# Verify ownership and permissions
kubectl exec -n dra-network-driver $POD -- stat /var/run/kubevirt/cdi/<path>
# Check SELinux labels
kubectl exec -n dra-network-driver $POD -- ls -laZ /var/run/kubevirt/cdi/<path>
```
**Common causes**: Wrong ownership (not 107:107), wrong permissions (not 775), incorrect SELinux label

### Checkpoint Missing
**Symptom**: No checkpoint.json file
**Check**:
```bash
kubectl logs -n dra-network-driver <pod> | grep -i "checkpoint"
```
**Common causes**: No claims prepared yet (normal), checkpoint write failed (check permissions)

## Reference: Expected Configuration

```yaml
# Driver Configuration
Driver Name: hostpath.network.com
Profile Name: hostpath
Device Names: hostpath-0, hostpath-1, ..., hostpath-9
Namespace: dra-network-driver
DaemonSet: dra-network-driver
Image: quay.io/rh-ee-ndothan/dra-test-driver:latest

# Paths
Base Directory: /var/run/kubevirt/cdi/
CDI Root: /etc/cdi/
Plugin Path: /var/lib/kubelet/plugins/hostpath.network.com/
Registry Path: /var/lib/kubelet/plugins_registry/

# Sockets
DRA Socket: /var/lib/kubelet/plugins/hostpath.network.com/dra.sock
Registration Socket: /var/lib/kubelet/plugins_registry/hostpath.network.com-reg.sock

# Directory Structure for Claims
Format: /var/run/kubevirt/cdi/{stable-claim-name}/{request-name}/
Example: /var/run/kubevirt/cdi/vm-a-dummy-gpu/hostpath/
Metadata: device.json with device_id field

# Security
Ownership: 107:107 (qemu user/group)
Permissions: 775 (drwxrwxr-x)
SELinux: system_u:object_r:container_file_t:s0

# CDI Files
Common: k8s.hostpath.network.com-hostpath_common.yaml
Per-Claim: k8s.hostpath.network.com-hostpath_{claim-uid}.yaml
```

## See Also

- `.claude/troubleshooting-cdi.md` - CDI directory troubleshooting
- `.claude/skills/dra-registration-troubleshooting.md` - Driver registration issues
- `.claude/migration-stability.md` - Migration-stable naming details
