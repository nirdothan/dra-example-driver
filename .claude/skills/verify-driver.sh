#!/bin/bash
# DRA Driver Verification Script
# Performs comprehensive checks of the DRA driver deployment

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
DRIVER_NAME="hostpath.network.com"
NAMESPACE="dra-network-driver"
PROFILE_NAME="hostpath"
BASE_DIR="/var/run/kubevirt/cdi"
CDI_ROOT="/etc/cdi"

# Counters
PASS=0
FAIL=0
WARN=0

# Helper functions
print_header() {
    echo -e "\n${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}\n"
}

print_section() {
    echo -e "\n${YELLOW}=== $1 ===${NC}"
}

pass() {
    echo -e "${GREEN}✓${NC} $1"
    ((PASS++))
}

fail() {
    echo -e "${RED}✗${NC} $1"
    ((FAIL++))
}

warn() {
    echo -e "${YELLOW}⚠${NC} $1"
    ((WARN++))
}

info() {
    echo -e "  $1"
}

# Verification functions
check_namespace() {
    print_section "1. Checking Namespace"
    if kubectl get namespace "$NAMESPACE" &>/dev/null; then
        pass "Namespace '$NAMESPACE' exists"
    else
        fail "Namespace '$NAMESPACE' not found"
        exit 1
    fi
}

check_driver_pods() {
    print_section "2. Checking Driver Pods"

    PODS=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    if [ "$PODS" -eq 0 ]; then
        fail "No driver pods found in namespace '$NAMESPACE'"
        exit 1
    fi

    READY=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -c "Running" || true)
    if [ "$READY" -eq "$PODS" ]; then
        pass "All $PODS driver pods are Running"
    else
        fail "Only $READY/$PODS pods are Running"
    fi

    # Show pod details
    kubectl get pods -n "$NAMESPACE" -o wide 2>/dev/null | tail -n +2 | head -10
}

check_deviceclass() {
    print_section "3. Checking DeviceClass"

    if kubectl get deviceclass "$DRIVER_NAME" &>/dev/null; then
        pass "DeviceClass '$DRIVER_NAME' exists"
    else
        fail "DeviceClass '$DRIVER_NAME' not found"
    fi
}

check_resourceslices() {
    print_section "4. Checking ResourceSlices"

    SLICES=$(kubectl get resourceslices -o jsonpath="{.items[?(@.spec.driver=='$DRIVER_NAME')].metadata.name}" 2>/dev/null)
    SLICE_COUNT=$(echo "$SLICES" | wc -w)

    if [ "$SLICE_COUNT" -gt 0 ]; then
        pass "Found $SLICE_COUNT ResourceSlice(s) for driver '$DRIVER_NAME'"
        # Show slice details
        for slice in $SLICES; do
            NODE=$(kubectl get resourceslice "$slice" -o jsonpath='{.spec.pool}' 2>/dev/null)
            info "$slice (node: $NODE)"
        done
    else
        fail "No ResourceSlices found for driver '$DRIVER_NAME'"
    fi
}

check_device_names() {
    print_section "5. Checking Device Names"

    SLICE=$(kubectl get resourceslices -o jsonpath="{.items[?(@.spec.driver=='$DRIVER_NAME')].metadata.name}" 2>/dev/null | awk '{print $1}')

    if [ -n "$SLICE" ]; then
        DEVICES=$(kubectl get resourceslice "$SLICE" -o jsonpath='{.spec.devices[*].name}' 2>/dev/null)
        if [ -n "$DEVICES" ]; then
            DEVICE_COUNT=$(echo "$DEVICES" | wc -w)
            pass "Found $DEVICE_COUNT devices advertised"
            info "Devices: $DEVICES"

            # Check if devices follow expected naming pattern
            if echo "$DEVICES" | grep -q "^hostpath-"; then
                pass "Device names follow 'hostpath-*' pattern"
            else
                warn "Device names don't follow expected 'hostpath-*' pattern"
            fi
        else
            fail "No devices found in ResourceSlice"
        fi
    else
        fail "Could not retrieve device names"
    fi
}

check_driver_sockets() {
    print_section "6. Checking Driver Sockets"

    POD=$(kubectl get pods -n "$NAMESPACE" -o name 2>/dev/null | head -1 | cut -d/ -f2)

    if [ -z "$POD" ]; then
        fail "No driver pod found"
        return
    fi

    # Check DRA socket
    if kubectl exec -n "$NAMESPACE" "$POD" -- test -S "/var/lib/kubelet/plugins/$DRIVER_NAME/dra.sock" 2>/dev/null; then
        pass "DRA socket exists: /var/lib/kubelet/plugins/$DRIVER_NAME/dra.sock"
    else
        fail "DRA socket not found"
    fi

    # Check registration socket
    if kubectl exec -n "$NAMESPACE" "$POD" -- test -S "/var/lib/kubelet/plugins_registry/$DRIVER_NAME-reg.sock" 2>/dev/null; then
        pass "Registration socket exists: $DRIVER_NAME-reg.sock"
    else
        fail "Registration socket not found"
    fi
}

check_base_directory() {
    print_section "7. Checking Base Directory"

    POD=$(kubectl get pods -n "$NAMESPACE" -o name 2>/dev/null | head -1 | cut -d/ -f2)

    if [ -z "$POD" ]; then
        fail "No driver pod found"
        return
    fi

    if kubectl exec -n "$NAMESPACE" "$POD" -- test -d "$BASE_DIR" 2>/dev/null; then
        pass "Base directory exists: $BASE_DIR"
    else
        fail "Base directory not found: $BASE_DIR"
    fi
}

check_cdi_specs() {
    print_section "8. Checking CDI Configuration"

    POD=$(kubectl get pods -n "$NAMESPACE" -o name 2>/dev/null | head -1 | cut -d/ -f2)

    if [ -z "$POD" ]; then
        fail "No driver pod found"
        return
    fi

    # Check common CDI spec
    COMMON_SPEC="k8s.$DRIVER_NAME-${PROFILE_NAME}_common.yaml"
    if kubectl exec -n "$NAMESPACE" "$POD" -- test -f "$CDI_ROOT/$COMMON_SPEC" 2>/dev/null; then
        pass "Common CDI spec exists: $COMMON_SPEC"
    else
        fail "Common CDI spec not found: $COMMON_SPEC"
    fi
}

check_logs_for_errors() {
    print_section "9. Checking Driver Logs for Errors"

    ERRORS=$(kubectl logs -n "$NAMESPACE" -l app=dra-network-driver --tail=200 2>/dev/null | grep -i "error\|fatal\|panic" | head -5 || true)

    if [ -z "$ERRORS" ]; then
        pass "No errors found in recent logs"
    else
        warn "Found errors in logs:"
        info "$ERRORS"
    fi
}

check_active_claims() {
    print_section "10. Checking Active ResourceClaims"

    CLAIMS=$(kubectl get resourceclaims --all-namespaces -o json 2>/dev/null | \
        jq -r ".items[] | select(.status.allocation.devices.results[]?.driver==\"$DRIVER_NAME\") | \"\(.metadata.namespace)/\(.metadata.name)\"" || true)

    if [ -z "$CLAIMS" ]; then
        info "No active ResourceClaims found (this is normal if no VMs/pods are using devices)"
    else
        CLAIM_COUNT=$(echo "$CLAIMS" | wc -l)
        pass "Found $CLAIM_COUNT active ResourceClaim(s) using driver '$DRIVER_NAME'"
        for claim in $CLAIMS; do
            info "$claim"
        done

        # Check details of first claim
        FIRST_CLAIM_NS=$(echo "$CLAIMS" | head -1 | cut -d/ -f1)
        FIRST_CLAIM_NAME=$(echo "$CLAIMS" | head -1 | cut -d/ -f2)

        if [ -n "$FIRST_CLAIM_NAME" ]; then
            check_claim_details "$FIRST_CLAIM_NS" "$FIRST_CLAIM_NAME"
        fi
    fi
}

check_claim_details() {
    local ns=$1
    local name=$2

    print_section "11. Checking Claim Details: $ns/$name"

    DEVICE=$(kubectl get resourceclaim -n "$ns" "$name" -o jsonpath='{.status.allocation.devices.results[0].device}' 2>/dev/null)
    NODE=$(kubectl get resourceclaim -n "$ns" "$name" -o jsonpath='{.status.allocation.devices.results[0].pool}' 2>/dev/null)
    REQUEST=$(kubectl get resourceclaim -n "$ns" "$name" -o jsonpath='{.status.allocation.devices.results[0].request}' 2>/dev/null)

    if [ -n "$DEVICE" ]; then
        pass "Device allocated: $DEVICE"
        info "Node: $NODE"
        info "Request: $REQUEST"

        # Get DRA pod on that node
        DRA_POD=$(kubectl get pods -n "$NAMESPACE" -o wide 2>/dev/null | grep "$NODE" | awk '{print $1}' | head -1)

        if [ -n "$DRA_POD" ]; then
            # Check checkpoint
            if kubectl exec -n "$NAMESPACE" "$DRA_POD" -- test -f "/var/lib/kubelet/plugins/$DRIVER_NAME/checkpoint.json" 2>/dev/null; then
                pass "Checkpoint file exists on node $NODE"

                # Verify claim is tracked
                if kubectl exec -n "$NAMESPACE" "$DRA_POD" -- cat "/var/lib/kubelet/plugins/$DRIVER_NAME/checkpoint.json" 2>/dev/null | grep -q "$name"; then
                    pass "Claim tracked in checkpoint"
                else
                    warn "Claim not found in checkpoint"
                fi
            else
                warn "Checkpoint file not found on node $NODE"
            fi

            # Check for claim directory
            check_claim_directory "$DRA_POD" "$name" "$REQUEST"
        fi
    else
        fail "Could not retrieve device allocation details"
    fi
}

check_claim_directory() {
    local pod=$1
    local claim_name=$2
    local request=$3

    print_section "12. Checking Claim Directory Structure"

    # Extract stable claim name (simplified - assumes KubeVirt naming)
    STABLE_NAME=$(echo "$claim_name" | sed -E 's/virt-launcher-([^-]+-[^-]+)-[^-]+-(.+)-[^-]+$/\1-\2/')

    CLAIM_DIR="$BASE_DIR/$STABLE_NAME/$request"

    if kubectl exec -n "$NAMESPACE" "$pod" -- test -d "$CLAIM_DIR" 2>/dev/null; then
        pass "Claim directory exists: $CLAIM_DIR"

        # Check permissions
        PERMS=$(kubectl exec -n "$NAMESPACE" "$pod" -- stat -c "%a" "$CLAIM_DIR" 2>/dev/null || echo "")
        OWNER=$(kubectl exec -n "$NAMESPACE" "$pod" -- stat -c "%u:%g" "$CLAIM_DIR" 2>/dev/null || echo "")
        SELINUX=$(kubectl exec -n "$NAMESPACE" "$pod" -- stat -c "%C" "$CLAIM_DIR" 2>/dev/null || echo "")

        if [ "$PERMS" = "775" ]; then
            pass "Permissions correct: $PERMS"
        else
            warn "Permissions: $PERMS (expected 775)"
        fi

        if [ "$OWNER" = "107:107" ]; then
            pass "Ownership correct: $OWNER (qemu)"
        else
            warn "Ownership: $OWNER (expected 107:107)"
        fi

        if echo "$SELINUX" | grep -q "container_file_t"; then
            pass "SELinux label correct: $SELINUX"
        else
            warn "SELinux label: $SELINUX (expected container_file_t)"
        fi

        # Check device.json
        if kubectl exec -n "$NAMESPACE" "$pod" -- test -f "$CLAIM_DIR/device.json" 2>/dev/null; then
            pass "Device metadata file exists: device.json"
            METADATA=$(kubectl exec -n "$NAMESPACE" "$pod" -- cat "$CLAIM_DIR/device.json" 2>/dev/null || echo "")
            info "Content: $METADATA"
        else
            warn "Device metadata file (device.json) not found"
        fi
    else
        fail "Claim directory not found: $CLAIM_DIR"
    fi
}

print_summary() {
    print_header "VERIFICATION SUMMARY"

    echo -e "${GREEN}Passed:${NC} $PASS"
    echo -e "${YELLOW}Warnings:${NC} $WARN"
    echo -e "${RED}Failed:${NC} $FAIL"
    echo ""

    if [ "$FAIL" -eq 0 ]; then
        if [ "$WARN" -eq 0 ]; then
            echo -e "${GREEN}✓ All checks passed!${NC}"
            exit 0
        else
            echo -e "${YELLOW}⚠ All checks passed with $WARN warning(s)${NC}"
            exit 0
        fi
    else
        echo -e "${RED}✗ Verification failed with $FAIL error(s)${NC}"
        exit 1
    fi
}

# Main execution
main() {
    print_header "DRA DRIVER VERIFICATION"
    echo "Driver: $DRIVER_NAME"
    echo "Namespace: $NAMESPACE"
    echo "Profile: $PROFILE_NAME"

    check_namespace
    check_driver_pods
    check_deviceclass
    check_resourceslices
    check_device_names
    check_driver_sockets
    check_base_directory
    check_cdi_specs
    check_logs_for_errors
    check_active_claims

    print_summary
}

# Run main function
main "$@"
