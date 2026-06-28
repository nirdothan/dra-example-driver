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
	"strings"
	"unicode"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	"sigs.k8s.io/dra-test-driver/internal/profiles"
)

const (
	ProfileName = "hostpath"

	// HostBaseDir is where directories are created on the host.
	HostBaseDir = "/var/run/kubevirt/cdi"

	// ContainerMountPath is where the directory is mounted in the container.
	ContainerMountPath = "/var/run/kubevirt/cdi"
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

// EnumerateDevices advertises the available network directory devices.
// This is called at driver startup (discovery time).
func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	// Create the base directory at discovery time
	if err := os.MkdirAll(HostBaseDir, 0755); err != nil {
		return resourceslice.DriverResources{}, fmt.Errorf("failed to create base directory %s: %w", HostBaseDir, err)
	}

	var devices []resourceapi.Device

	// Create N simple devices (just slots for directory claims)
	for i := 0; i < p.numDevices; i++ {
		devices = append(devices, resourceapi.Device{
			Name: fmt.Sprintf("hostpath-%d", i),
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

// SchemeBuilder implements profiles.ConfigHandler.
// No custom config needed for network directories.
func (p Profile) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder()
}

// Validate implements profiles.ConfigHandler.
// No custom config to validate.
func (p Profile) Validate(config runtime.Object) error {
	if config != nil {
		return fmt.Errorf("configuration not supported for network profile")
	}
	return nil
}

// extractStableClaimName extracts the migration-stable portion of a ResourceClaim name.
// For KubeVirt claims, the format is: virt-launcher-<vmi-name>-<pod-hash>-<template-name>-<claim-hash>
// We extract: <vmi-name>-<template-name>.
// Example: "virt-launcher-vm-a-drz4j-dummy-gpu-fngjv" -> "vm-a-dummy-gpu".
func extractStableClaimName(fullClaimName string) string {
	const virtLauncherPrefix = "virt-launcher-"

	// Check if this is a KubeVirt virt-launcher claim
	if !strings.HasPrefix(fullClaimName, virtLauncherPrefix) {
		// Not a virt-launcher claim, use the full name
		return fullClaimName
	}

	// Remove the "virt-launcher-" prefix
	withoutPrefix := strings.TrimPrefix(fullClaimName, virtLauncherPrefix)

	// Split by "-"
	parts := strings.Split(withoutPrefix, "-")

	// We need at least 4 parts
	if len(parts) < 4 {
		return fullClaimName
	}

	// Remove the last element (claim hash)
	withoutClaimHash := parts[:len(parts)-1]

	// Find pod hash (5-char alphanumeric)
	podHashIdx := -1
	for i, part := range withoutClaimHash {
		if len(part) == 5 && isAlphanumeric(part) {
			if i+1 < len(withoutClaimHash) {
				podHashIdx = i
				break
			}
		}
	}

	if podHashIdx == -1 {
		return fullClaimName
	}

	// VMI name is before pod hash
	vmiNameParts := withoutClaimHash[:podHashIdx]
	// Template name is after pod hash
	templateNameParts := withoutClaimHash[podHashIdx+1:]

	vmiName := strings.Join(vmiNameParts, "-")
	templateName := strings.Join(templateNameParts, "-")

	return vmiName + "-" + templateName
}

// isAlphanumeric checks if a string contains only alphanumeric characters.
func isAlphanumeric(s string) bool {
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// ApplyConfig creates a directory per claim and mounts it via CDI.
// Note: The actual directory creation happens in state.prepareDevices().
// This function only configures the CDI mount specification using the stable claim name.
func (p Profile) ApplyConfig(claimName string, config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	perDeviceEdits := make(profiles.PerDeviceCDIContainerEdits)

	// Extract migration-stable portion of claim name
	stableClaimName := extractStableClaimName(claimName)

	for _, result := range results {
		// Build directory path: {base}/{stable-claim-name}/{request-name}/
		// The device ID is stored in a device.json file inside this directory
		claimDir := filepath.Join(HostBaseDir, stableClaimName, result.Request)

		edits := &cdispec.ContainerEdits{
			Env: []string{
				fmt.Sprintf("KUBEVIRT_NETWORK_DEVICE=%s", result.Device),
				fmt.Sprintf("KUBEVIRT_NETWORK_PATH=%s", claimDir),
				fmt.Sprintf("KUBEVIRT_NETWORK_REQUEST=%s", result.Request),
			},
			Mounts: []*cdispec.Mount{
				{
					HostPath:      claimDir,
					ContainerPath: claimDir,
					Options:       []string{"rbind", "z"},
				},
			},
		}

		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}

	return perDeviceEdits, nil
}

// CreateClaimDirectory creates a subdirectory for the claim+device on the host.
// Directory path format: {base}/{claim-name}/{request-name}/{device-name}.
// This should be called during device preparation.
func CreateClaimDirectory(claimName string, requestName string, deviceName string) (string, error) {
	// Create directory path: {base}/{claim-name}/{request-name}/{device-name}
	claimDir := filepath.Join(HostBaseDir, claimName, requestName, deviceName)

	if err := os.MkdirAll(claimDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create claim directory %s: %w", claimDir, err)
	}

	return claimDir, nil
}

// DeleteClaimDirectory removes the claim directory.
// This should be called during device cleanup.
func DeleteClaimDirectory(claimName string, requestName string, deviceName string) error {
	claimDir := filepath.Join(HostBaseDir, claimName, requestName, deviceName)

	if err := os.RemoveAll(claimDir); err != nil {
		return fmt.Errorf("failed to delete claim directory %s: %w", claimDir, err)
	}
	return nil
}
