# Demo Examples

This directory contains example workloads that demonstrate how to request and configure network devices using Dynamic Resource Allocation (DRA).

## Files

- **network-claim-template.yaml** - ResourceClaimTemplate for requesting network devices
- **network-test-pods.yaml** - Three test pods demonstrating network device usage
- **build-driver.sh** - Script to build the driver binary

## Running Examples

Deploy the test pods:

```bash
kubectl apply -f demo/network-test-pods.yaml
```

Check that pods are running and devices are mounted:

```bash
kubectl get pods -n network-test
kubectl logs -n network-test network-pod-1
```

To clean up:

```bash
kubectl delete -f demo/network-test-pods.yaml
```

## Notes

- The default configuration advertises **10 network devices** per node (configurable via `NUM_DEVICES` in `deployments/manifests/daemonset.yaml`)
- Each ResourceClaim creates a directory at `/var/run/kubevirt/network/{claim-name}-{device-name}`
- The directory is mounted into containers requesting the device
