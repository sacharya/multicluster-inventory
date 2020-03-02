# The multicluster-inventory operator

The multicluster-inventory operator provides a CRD that is used to hold inventory records in the hub cluster, and a controller that reconciles inventory with resources in the managed cluster. The actual synchronization of resources from hub to managed cluster is done using hive SyncSet.

# Documentation
* [Installation](./docs/install.md)
* [Using Inventory](./docs/using-inventory.md)
* [FAQs](./docs/FAQs.md)