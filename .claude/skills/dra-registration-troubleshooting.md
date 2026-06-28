# DRA Driver Registration Troubleshooting

## Symptom

Pods fail to start with error:
```
Failed to prepare dynamic resources: prepare dynamic resources: DRA driver hostpath.network.com is not registered
```

## Root Causes

### 1. Missing RegistrarDirectoryPath Configuration

**Problem**: The kubelet plugin library requires explicit registration directory path to create the registration socket.

**Code Location**: `cmd/dra-test-kubeletplugin/driver.go`

**Fix**: Ensure `kubeletplugin.Start()` includes the `RegistrarDirectoryPath` option:

```go
helper, err := kubeletplugin.Start(ctx, driver,
    kubeletplugin.KubeClient(config.coreclient),
    kubeletplugin.NodeName(config.nodeName),
    kubeletplugin.DriverName(config.driverName),
    kubeletplugin.RegistrarDirectoryPath(kubeletplugin.KubeletRegistryDir),  // REQUIRED
    kubeletplugin.PluginDataDirectoryPath(config.DriverPluginPath()),
)
```

**Diagnosis**: Check driver logs for socket creation:
```bash
kubectl logs <driver-pod> -n dra-network-driver | grep registrar
```

Should show:
```
GRPC server started logger="registrar" endpoint="/var/lib/kubelet/plugins_registry/hostpath.network.com-reg.sock"
```

**Verify sockets exist on host**:
```bash
# SSH to node or use debug pod
sudo ls -la /var/lib/kubelet/plugins_registry/
sudo ls -la /var/lib/kubelet/plugins/hostpath.network.com/
```

Should see:
- `hostpath.network.com-reg.sock` - registration socket
- `dra.sock` - DRA plugin socket

### 2. Sockets Disappear After Pod Creation

**Problem**: Sockets are created but disappear after several minutes, causing new pods to fail with "not registered" error.

**Possible Causes**:
- Unusual file ownership (`1001 hugetlbfs` instead of `root`)
- SELinux policies
- Kubelet cleanup operations
- Container user/group mismatch

**Workaround**: Delete and recreate the driver pod on affected node:
```bash
# Find driver pod on the node where VM is scheduled
kubectl get pods -n dra-network-driver -o wide

# Delete the pod on that node
kubectl delete pod <driver-pod-name> -n dra-network-driver

# Wait for new pod to start and sockets to be recreated
sleep 10
```

**Permanent Fix**: Full system redeploy ensures clean state:
```bash
# Delete DaemonSet
kubectl delete daemonset dra-network-driver -n dra-network-driver

# Redeploy
kubectl apply -f deployments/manifests/daemonset.yaml
```

## Quick Diagnosis Checklist

1. **Check driver pods are running**:
   ```bash
   kubectl get pods -n dra-network-driver -o wide
   ```

2. **Check driver logs for errors**:
   ```bash
   kubectl logs <driver-pod> -n dra-network-driver | grep -E "error|Error|fail"
   ```

3. **Verify registration socket exists**:
   ```bash
   ~/work/kubevirt/kubevirtci/cluster-up/ssh.sh <node> "sudo ls -la /var/lib/kubelet/plugins_registry/"
   ```

4. **Verify DRA socket exists**:
   ```bash
   ~/work/kubevirt/kubevirtci/cluster-up/ssh.sh <node> "sudo ls -la /var/lib/kubelet/plugins/hostpath.network.com/"
   ```

5. **Check driver can write to directories**:
   ```bash
   kubectl exec -n dra-network-driver <driver-pod> -- touch /var/lib/kubelet/plugins_registry/test.txt
   ```

## DaemonSet Configuration Requirements

The DaemonSet must mount these directories:

```yaml
volumeMounts:
  - name: plugins-registry
    mountPath: /var/lib/kubelet/plugins_registry
  - name: plugins
    mountPath: /var/lib/kubelet/plugins
  - name: cdi
    mountPath: /etc/cdi  # Standard CDI location

volumes:
  - name: plugins-registry
    hostPath:
      path: /var/lib/kubelet/plugins_registry
  - name: plugins
    hostPath:
      path: /var/lib/kubelet/plugins
  - name: cdi
    hostPath:
      path: /etc/cdi  # Standard CDI location
```

**Note**: `/etc/cdi` is the standard CDI location where the CDI library writes specs by default. Using `/var/run/cdi` will cause CDI injection to fail because specs won't be visible to the container runtime.

## Logging for Debugging

Enable verbose logging in DaemonSet:
```yaml
command: ["dra-test-kubeletplugin", "--logtostderr=true", "-v=5"]
```

Key log messages to look for:
- `NewDriver: Starting with registrar path=...` - confirms paths
- `GRPC server started logger="registrar"` - registration socket created
- `GRPC server started logger="dra"` - DRA socket created
- `PrepareResourceClaims is called` - driver handling requests
- `Creating CDI spec file for claim` - CDI specs being created

## Related Issues

- **CDI injection failures**: Check `/etc/cdi` directory has spec files (see CDI troubleshooting)
- **Checkpoint issues**: Check `/var/lib/kubelet/plugins/hostpath.network.com/checkpoint.json` exists
- **Permission errors**: Driver runs as privileged, verify `securityContext.privileged: true`
