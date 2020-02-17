# The multicluster-inventory Operator

This operator provides a CRD that is used to hold inventory records in the hub cluster, and a controller that reconciles inventory with resources in the managed cluster. The actual placement of resources from hub to managed cluster is done using either hive's SyncSet or MCM.

## Start a managed cluster named cluster0
```
minikube start -p cluster0
kubectl apply -f https://raw.githubusercontent.com/metal3-io/baremetal-operator/master/deploy/crds/metal3.io_baremetalhosts_crd.yaml
kubectl create namespace openshift-machine-api
```

## Start a hub cluster named hive
```
minikube start -p hive
```

Deploy hive to the hub cluster following [hive docs](https://github.com/openshift/hive/blob/master/docs/developing.md).
```
git clone https://github.com/openshift/hive
cd hive
git checkout master
make build
make deploy

rm -rf hiveadmission-certs
./hack/hiveadmission-dev-cert.sh

```
Adopt cluster0 as a managed cluster
```
kubectl create namespace cluster0
bin/hiveutil create-cluster --adopt --adopt-admin-kubeconfig=/tmp/cluster0.config --adopt-cluster-id=cluster0 --adopt-infra-id=cluster0 -n cluster0 cluster0
```
Note: You may need to add --include-secrets=false as documented [here](https://github.com/openshift/hive/issues/774) if you get an error message about missing AWS credentials.

/tmp/cluster0.config is the kubeconfig file for the managed cluster "cluster0" with embedded certs.

## Build

The operator can be built using:

```
make build
```

To install your own build of multicluster-inventory, build and push your image to your own image registry.
```
export REGISTRY_NAMESPACE=sacharya
export TAG=latest
make build-image
podman push quay.io/$REGISTRY_NAMESPACE/multicluster-inventory:$TAG
```

## Install operator on hub cluster
Once the image is pushed, edit the deploy/operator.yaml to point to your image.
```
MCINVENTORY_IMAGE="quay.io/$REGISTRY_NAMESPACE/multicluster-inventory:$TAG"
sed -i "s|REPLACE_IMAGE|$MCINVENTORY_IMAGE|" ./deploy/operator.yaml
```

Deploy the operator:
```
kubectl create -f deploy/service_account.yaml
kubectl create -f deploy/role.yaml
kubectl create -f deploy/role_binding.yaml
kubectl create -f deploy/crds/midas.io_baremetalassets_crd.yaml
kubectl create -f deploy/operator.yaml
```

Once you have the operator running, you can create an inventory record ie. BareMetalAsset using:
```
kubectl create -f deploy/crds/midas.io_v1alpha1_baremetalasset_cr.yaml
```

## Verify
On the hub cluster:
```
kubectl get baremetalassets -n default
kubectl get secrets -n default
kubectl get clusterdeployments -n cluster0
kubectl get syncsets -n cluster0
kubectl get syncsetinstances -n cluster0
```

On cluster0, verify the resources are properly propagated:
```
kubectl get baremetalhosts -n openshift-machine-api
kubectl get secrets -n openshift-machine-api
```
