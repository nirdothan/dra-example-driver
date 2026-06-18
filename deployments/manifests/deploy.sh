#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Deploying DRA Network Driver..."
kubectl apply -f "${SCRIPT_DIR}/namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}/serviceaccount.yaml"
kubectl apply -f "${SCRIPT_DIR}/rbac.yaml"
kubectl apply -f "${SCRIPT_DIR}/daemonset.yaml"
kubectl apply -f "${SCRIPT_DIR}/deviceclass.yaml"
kubectl apply -f "${SCRIPT_DIR}/validating-admission-policy.yaml"

echo
echo "Waiting for driver to be ready..."
kubectl wait --for=condition=ready pod -l app=dra-network-driver -n dra-network-driver --timeout=60s

echo
echo "Deployment successful!"
echo
echo "Check driver status:"
echo "  kubectl get pods -n dra-network-driver"
echo
echo "Check available devices:"
echo "  kubectl get resourceslices"
echo
echo "Deploy test pods:"
echo "  kubectl apply -f ../../demo/network-test-pods.yaml"
