package controller

import (
	"github.com/open-cluster-management/multicluster-inventory/pkg/controller/baremetalasset"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, baremetalasset.Add)
}
