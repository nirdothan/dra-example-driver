# KubeVirt Network Device Driver for Dynamic Resource Allocation (DRA)

This repository contains a DRA resource driver for use with the [Dynamic
Resource Allocation
(DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
feature of Kubernetes.

It provides network device isolation for KubeVirt testing by creating
directory-based resources that can be mounted into containers via CDI.

## Quickstart and Demo

The driver manages network devices and creates migration-stable directory paths
for KubeVirt VM network isolation testing.

### Prerequisites

* [GNU Make 3.81+](https://www.gnu.org/software/make/)
* [docker v20.10+ (including buildx)](https://docs.docker.com/engine/install/) or [Podman v4.9+](https://podman.io/docs/installation)
* [kubectl v1.18+](https://kubernetes.io/docs/reference/kubectl/)
* A Kubernetes cluster with DRA support (v1.31+)

### Building and Installing the Driver

First clone this repository:
```bash
git clone https://github.com/kubernetes-sigs/dra-example-driver.git
cd dra-example-driver
```

Build the driver image:
```bash
./demo/build-driver.sh
```

**Note**: The scripts will automatically use either `docker` or `podman` as the container tool command, whichever
can be found in the PATH. To override this behavior, set the `CONTAINER_TOOL` environment variable.

### Container image make recipes

The image build logic lives in `deployments/container/Makefile`.
If variables are not provided, defaults are:

- `IMAGE_NAME=registry.example.com/dra-example-driver`
- `VERSION=latest`
- `PLATFORMS=<current host platform>` (for example `linux/amd64`, `linux/arm64`, or `linux/ppc64le`) when `PLATFORMS` is **unset**
- `CONTAINER_TOOL=docker`

For demo scripts, `PLATFORMS` is the canonical variable and `DRIVER_IMAGE_PLATFORMS`
is only a backward compatible fallback. Setting both is treated as an error.

`cloudbuild.yaml` and `demo/scripts/push-driver-image.sh` may provide different
`PLATFORMS` defaults depending on the workflow:

1. **If `PLATFORMS` is set (e.g. by `cloudbuild.yaml`)**, the Makefile uses it as-is.
2. **If `PLATFORMS` is unset**, `demo/scripts/push-driver-image.sh` fills a fallback
   (currently `linux/amd64,linux/arm64,linux/ppc64le`) and `deployments/container/Makefile`
   falls back to the host platform.
3. **If `PLATFORMS` is set to an empty string**, `deployments/container/Makefile` fails
   with a clear error (to avoid confusing silent buildx behavior).

- Build a single-arch image with the standard Docker/Podman build flow:
  ```bash
  make -f deployments/container/Makefile build VERSION=<tag> IMAGE_NAME=<name|registry/name> CONTAINER_TOOL=<docker|podman>
  ```
- Build for specific platform(s):
  ```bash
  make -f deployments/container/Makefile build VERSION=<tag> IMAGE_NAME=<name|registry/name> CONTAINER_TOOL=docker PLATFORMS='linux/amd64,linux/arm64'
  ```
- Push for current platform:
  ```bash
  make -f deployments/container/Makefile push VERSION=<tag> IMAGE_NAME=<registry/name> CONTAINER_TOOL=<docker|podman>
  ```
- Push for specific platform(s):
  ```bash
  make -f deployments/container/Makefile push VERSION=<tag> IMAGE_NAME=<registry/name> CONTAINER_TOOL=docker PLATFORMS='linux/amd64,linux/arm64'
  ```

For Docker, `build` with multiple platforms performs a Buildx build without loading an image into the local Docker daemon; use `push` to publish multi-arch images.

#### Multi-platform builds on Linux (amd64)

Building for a platform other than your host CPU (for example `linux/arm64` on an
`x86_64` machine) requires Docker Buildx to **run** container steps for that
architecture during the image build. That needs either native hardware or
userspace emulation via [QEMU and `binfmt_misc`](https://docs.docker.com/build/building/multi-platform/#qemu).

- **Docker Desktop** (macOS/Windows) and many CI images ship with this enabled.
- **Linux on amd64** often does not. The same applies to an explicit single
  platform that does not match the host (for example `PLATFORMS=linux/arm64` on
  x86_64): the Makefile uses buildx for that case. If a build fails with
  `exec format error` on an `linux/arm64` step, install emulation support:

  ```bash
  docker run --privileged --rm tonistiigi/binfmt --install all
  ```

  Then create or bootstrap a buildx builder (`build` and `push` do this
  automatically via `ensure-buildx-builder` for multi-platform or cross-platform
  single-platform builds):

  ```bash
  make -f deployments/container/Makefile ensure-buildx-builder
  ```

  Verify with:

  ```bash
  docker run --rm --platform linux/arm64 alpine uname -m
  ```

  The output should be `aarch64`.

On Apple Silicon, single-arch `linux/arm64` builds work natively; building
`linux/amd64` uses emulation the same way.

Deploy the driver to your cluster using static manifests:
```bash
kubectl apply -f deployments/manifests/
```

Verify the driver components are running:
```console
$ kubectl get pod -n dra-network-driver
NAME                                   READY   STATUS    RESTARTS   AGE
dra-network-driver-kubeletplugin-xxxxx 1/1     Running   0          1m
```

Check that network devices are advertised:
```console
$ kubectl get resourceslice
NAME                                                    DRIVER                NODE       ALL ALLOCATED
worker-node-network.example.com-xxxxx                   network.example.com   worker-1   false
```

### Run example workloads

Deploy example test pods that request network devices:
```bash
kubectl apply -f demo/network-test-pods.yaml
```

Verify the pods are running:
```console
$ kubectl get pod -n network-test
NAME             READY   STATUS    RESTARTS   AGE
network-pod-1    1/1     Running   0          30s
network-pod-2    1/1     Running   0          30s
network-pod-3    1/1     Running   0          30s
```

Check that network device directories are mounted:
```bash
kubectl logs -n network-test network-pod-1
```

The driver creates migration-stable directories at `/var/run/kubevirt/cdi/{claim-name}/{request}/`
with proper ownership (qemu 107:107) and SELinux labels for KubeVirt container isolation.


### Cleanup

Delete the test pods:
```bash
kubectl delete -f demo/network-test-pods.yaml
```

Remove the driver:
```bash
kubectl delete -f deployments/manifests/
```

## Network Device Management

The driver manages network devices using a directory-based approach optimized for
KubeVirt VM migration:

- Creates stable directory paths using VMI and template names
- Stores device metadata in `device.json` files within each directory
- Sets proper ownership (qemu 107:107) and SELinux labels
- Mounts directories via CDI for container isolation
- Supports cross-node migration by excluding device IDs from paths

Default configuration advertises 10 network devices per node (configurable via
`NUM_DEVICES` environment variable in the DaemonSet).

## Architecture

The driver consists of:

- **Kubelet plugin**: DaemonSet running on each node that manages network device resources
- **Device profile**: Network-specific implementation handling directory creation and lifecycle
- **CDI integration**: Mounts device directories into containers requesting network resources

See [`demo/README.md`](demo/README.md) for detailed examples.

## References

For more information on DRA and KubeVirt integration:

* [Dynamic Resource Allocation in Kubernetes](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
* [KubeVirt Documentation](https://kubevirt.io/user-guide/)
* [Container Device Interface (CDI)](https://github.com/cncf-tags/container-device-interface)
