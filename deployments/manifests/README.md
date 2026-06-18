# DRA Network Driver Deployment Manifests

Static Kubernetes manifests for deploying the DRA network driver.

## Deploying from a Remote Machine to KubeVirt Cluster

### Prerequisites

- Remote machine with Docker or Podman installed
- `kubectl` configured to access your KubeVirt cluster
- Access to a container registry (quay.io, docker.io, or internal registry)
- Git to clone the repository

### Complete Deployment Workflow

1. **Clone the repository on your remote machine:**
   ```bash
   git clone https://github.com/YOUR_USERNAME/dra-example-driver.git
   cd dra-example-driver
   ```

2. **Build the container image:**
   ```bash
   make image VERSION=v0.1.0 CONTAINER_TOOL=podman
   ```
   This compiles the source code and builds the container image tagged as `registry.example.com/dra-example-driver:v0.1.0`.
   
   (Use `CONTAINER_TOOL=docker` if you have Docker instead of Podman)

3. **Tag and push to your registry:**
   ```bash
   # Example with quay.io
   docker tag registry.example.com/dra-example-driver:v0.1.0 quay.io/YOUR_USERNAME/dra-network-driver:v0.1.0
   docker push quay.io/YOUR_USERNAME/dra-network-driver:v0.1.0
   ```
   Replace `YOUR_USERNAME` with your registry username.

4. **Update daemonset.yaml to use your image:**
   ```bash
   sed -i 's|registry.example.com/dra-network-driver:latest|quay.io/YOUR_USERNAME/dra-network-driver:v0.1.0|' deployments/manifests/daemonset.yaml
   ```
   Or manually edit `deployments/manifests/daemonset.yaml` line ~25.

4. **Update manifests to use your image:**
   ```bash
   sed -i 's|registry.example.com/dra-network-driver:latest|quay.io/YOUR_USERNAME/dra-network-driver:v0.1.0|' deployments/manifests/install.yaml
   ```
   Or manually edit `deployments/manifests/install.yaml` line ~77.

5. **Verify kubectl access to your KubeVirt cluster:**
   ```bash
   kubectl get nodes
   kubectl cluster-info
   ```

6. **Deploy to the cluster:**
   ```bash
   ./deployments/manifests/deploy.sh
   ```
   
   Or apply individual manifests:
   ```bash
   kubectl apply -f deployments/manifests/
   ```

7. **Verify deployment:**
   ```bash
   kubectl get pods -n dra-network-driver -w
   kubectl get resourceslices
   kubectl describe resourceslice | grep -A 5 "network.example.com"
   ```

8. **Test with example pods:**
   ```bash
   kubectl apply -f demo/network-test-pods.yaml
   kubectl get pods -n network-test
   kubectl logs -n network-test network-pod-1
   ```

### Troubleshooting

- **Image pull errors**: Verify registry authentication and image name/tag
  ```bash
  kubectl describe pod -n dra-network-driver <pod-name>
  ```

- **Pod stuck pending**: Check node resources and image pull policy
  ```bash
  kubectl get events -n dra-network-driver
  ```

- **No devices advertised**: Check driver logs
  ```bash
  kubectl logs -n dra-network-driver -l app=dra-network-driver
  ```

- **Permission errors**: Verify RBAC is deployed
  ```bash
  kubectl get clusterrole dra-network-driver
  kubectl get clusterrolebinding dra-network-driver
  ```

### KubeVirt-Specific Notes

- The driver creates directories at `/var/run/kubevirt/network/` on cluster nodes
- Each ResourceClaim creates a subdirectory: `/var/run/kubevirt/network/{claim-name}-{device-name}`
- The driver runs as privileged pods (required for CDI and directory creation)
- Ensure nodes have sufficient disk space for device directories

### Multi-Architecture Builds

For multi-arch images (amd64 + arm64) using Docker:
```bash
make image PLATFORMS=linux/amd64,linux/arm64 VERSION=v0.1.0 CONTAINER_TOOL=docker
make push-image PLATFORMS=linux/amd64,linux/arm64 VERSION=v0.1.0 CONTAINER_TOOL=docker
```

## Local Development

For local testing (kind/minikube), you can load the image directly without a registry:

## What Gets Deployed

- **Namespace**: `dra-network-driver`
- **ServiceAccount**: For the driver pods
- **ClusterRole/ClusterRoleBinding**: RBAC permissions for managing ResourceClaims and ResourceSlices
- **DaemonSet**: Runs the kubelet plugin on every node
- **DeviceClass**: `network.example.com` for requesting network devices
- **ValidatingAdmissionPolicy**: Restricts ResourceSlice modifications to node-local service accounts

## Configuration

Edit `daemonset.yaml` to customize:

- **NUM_DEVICES**: Number of network devices per node (default: 10)
- **Image**: Driver container image
- **HostBaseDir**: Directory path in `internal/profiles/network/network.go`

## Individual Components

You can also deploy components separately:

```bash
kubectl apply -f namespace.yaml
kubectl apply -f serviceaccount.yaml
kubectl apply -f rbac.yaml
kubectl apply -f daemonset.yaml
kubectl apply -f deviceclass.yaml
kubectl apply -f validating-admission-policy.yaml
```

## Uninstall

```bash
kubectl delete -f install.yaml
```
