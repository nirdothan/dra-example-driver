/*
 * Copyright The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package network

import (
	"fmt"
	"os"
	"path/filepath"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

const (
	ProfileName = "network"

	// HostBaseDir is where directories are created on the host
	HostBaseDir = "/var/run/kubevirt/network"

	// ContainerMountPath is where the directory is mounted in the container
	ContainerMountPath = "/var/run/kubevirt/network"
)

type Profile struct {
	nodeName   string
	numDevices int
}

func NewProfile(nodeName string, numDevices int) Profile {
	return Profile{
		nodeName:   nodeName,
		numDevices: numDevices,
	}
}

// EnumerateDevices advertises the available network directory devices
// This is called at driver startup (discovery time)
func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	// Create the base directory at discovery time
	if err := os.MkdirAll(HostBaseDir, 0755); err != nil {
		return resourceslice.DriverResources{}, fmt.Errorf("failed to create base directory %s: %w", HostBaseDir, err)
	}

	var devices []resourceapi.Device

	// Create N simple devices (just slots for directory claims)
	for i := 0; i < p.numDevices; i++ {
		devices = append(devices, resourceapi.Device{
			Name: fmt.Sprintf("network-%d", i),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"index": {
					IntValue: ptr.To(int64(i)),
				},
				"type": {
					StringValue: ptr.To("kubevirt-network-directory"),
				},
			},
		})
	}

	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			p.nodeName: {
				Slices: []resourceslice.Slice{{Devices: devices}},
			},
		},
	}

	return resources, nil
}

// SchemeBuilder implements profiles.ConfigHandler
// No custom config needed for network directories
func (p Profile) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder()
}

// Validate implements profiles.ConfigHandler
// No custom config to validate
func (p Profile) Validate(config runtime.Object) error {
	if config != nil {
		return fmt.Errorf("configuration not supported for network profile")
	}
	return nil
}

// ApplyConfig creates a directory per claim and mounts it via CDI
// Note: The actual directory creation happens in state.prepareDevices()
// This function only configures the CDI mount specification
func (p Profile) ApplyConfig(claimName string, config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	perDeviceEdits := make(profiles.PerDeviceCDIContainerEdits)

	for _, result := range results {
		// Build subdirectory name: {claim-name}-{device-name}
		subdirName := fmt.Sprintf("%s-%s", claimName, result.Device)
		claimDir := filepath.Join(HostBaseDir, subdirName)

		edits := &cdispec.ContainerEdits{
			Env: []string{
				fmt.Sprintf("KUBEVIRT_NETWORK_DEVICE=%s", result.Device),
				fmt.Sprintf("KUBEVIRT_NETWORK_PATH=%s", ContainerMountPath),
				fmt.Sprintf("KUBEVIRT_NETWORK_SUBDIR=%s", subdirName),
			},
			Mounts: []*cdispec.Mount{
				{
					HostPath:      claimDir,
					ContainerPath: ContainerMountPath,
					Options:       []string{"rbind"},
				},
			},
		}

		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}

	return perDeviceEdits, nil
}

// CreateClaimDirectory creates a subdirectory for the claim+device on the host
// Directory name format: {claim-name}-{device-name}
// This should be called during device preparation
func CreateClaimDirectory(claimName string, deviceName string) (string, error) {
	// Create subdirectory named {claim-name}-{device-name}
	subdirName := fmt.Sprintf("%s-%s", claimName, deviceName)
	claimDir := filepath.Join(HostBaseDir, subdirName)

	if err := os.MkdirAll(claimDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create claim directory %s: %w", claimDir, err)
	}

	return claimDir, nil
}

// DeleteClaimDirectory removes the claim directory
// This should be called during device cleanup
func DeleteClaimDirectory(claimName string, deviceName string) error {
	subdirName := fmt.Sprintf("%s-%s", claimName, deviceName)
	claimDir := filepath.Join(HostBaseDir, subdirName)

	if err := os.RemoveAll(claimDir); err != nil {
		return fmt.Errorf("failed to delete claim directory %s: %w", claimDir, err)
	}
	return nil
}
