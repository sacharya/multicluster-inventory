# The multicluster-inventory Operator

This operator provides a CRD that is used to hold inventory records in the hub cluster, and a controller that reconciles inventory with resources in the managed cluster. The actual placement of resources from hub to managed cluster is done using either hive's SyncSet or MCM.

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

## Install
Once the image is pushed, edit the deploy/operator.yaml to point to your image.
```
MCINVENTORY_IMAGE="quay.io/$REGISTRY_NAMESPACE/multicluster-inventory:$TAG"
sed -i "s|REPLACE|$MCINVENTORY_IMAGE" ./deploy/operator.yaml
```

Deploy the operator:
```
kubectl create -f deploy/service_account.yaml
kubectl create -f deploy/role.yaml
kubectl create -f deploy/role_binding.yaml
kubectl create -f deploy/crds/app.ibm.com_baremetalassets_crd.yaml
kubectl create -f deploy/operator.yaml
```

Once you have the operator running, you can create an inventory record ie. BareMetalAsset using:
```
kubectl create -f deploy/crds/app.ibm.com_v1alpha1_baremetalasset_cr.yaml
```