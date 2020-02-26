package baremetalasset

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	midasv1alpha1 "github.com/open-cluster-management/multicluster-inventory/pkg/apis/midas/v1alpha1"
	bmaerrors "github.com/open-cluster-management/multicluster-inventory/pkg/errors"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	hiveconstants "github.com/openshift/hive/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	k8slabels "k8s.io/kubernetes/pkg/util/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_baremetalasset")

const (
	// RoleLabel is the key name for the role label associated with the asset
	RoleLabel = "metal3.io/role"
	// ClusterDeploymentNameLabel is the key name for the name label associated with the asset's clusterDeployment
	ClusterDeploymentNameLabel = "metal3.io/cluster-deployment-name"
	// ClusterDeploymentNamespaceLabel is the key name for the namespace label associated with the asset's clusterDeployment
	ClusterDeploymentNamespaceLabel = "metal3.io/cluster-deployment-namespace"
	// BareMetalHostKind contains the value of kind BareMetalHost
	BareMetalHostKind = "BareMetalHost"
	// BareMetalAssetFinalizer is the finalizer used on BareMetalAsset resource
	BareMetalAssetFinalizer = "baremetalasset.midas.io"
)

const (
	// assetSecretRequeueAfter specifies the amount of time, in seconds, before requeue
	assetSecretRequeueAfter int = 60
)

// Add creates a new BareMetalAsset Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileBareMetalAsset{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("baremetalasset-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource BareMetalAsset
	err = c.Watch(&source.Kind{Type: &midasv1alpha1.BareMetalAsset{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Secrets and requeue the owner BareMetalAsset
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &midasv1alpha1.BareMetalAsset{},
	})
	if err != nil {
		return err
	}

	// Watch for changes to SyncSets and requeue BareMetalAssets with the name and matching cluster-deployment-namespace label
	// (which is also the syncset namespace)
	err = c.Watch(
		&source.Kind{Type: &hivev1.SyncSet{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
				syncSet, ok := a.Object.(*hivev1.SyncSet)
				if !ok {
					// not a SyncSet, returning empty
					log.Error(nil, "SyncSet handler recieved non-SyncSet object")
					return []reconcile.Request{}
				}
				bmas := &midasv1alpha1.BareMetalAssetList{}
				err := mgr.GetClient().List(context.TODO(), bmas,
					client.MatchingFields{"metadata.name": syncSet.Name},
					client.MatchingLabels{
						ClusterDeploymentNamespaceLabel: syncSet.Namespace,
					})
				if err != nil {
					log.Error(err, "Could not list BareMetalAsset %v with label %v=%v", syncSet.Name, ClusterDeploymentNamespaceLabel, syncSet.Namespace)
				}
				var requests []reconcile.Request
				for _, bma := range bmas.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      bma.Name,
							Namespace: bma.Namespace,
						},
					})
				}
				return requests
			}),
		})

	if err != nil {
		return err
	}

	// Watch for changes to ClusterDeployments and requeue BareMetalAssets with labels set to
	// ClusterDeployment's name (which is expected to be the clusterName)
	err = c.Watch(
		&source.Kind{Type: &hivev1.ClusterDeployment{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
				clusterDeployment, ok := a.Object.(*hivev1.ClusterDeployment)
				if !ok {
					// not a Deployment, returning empty
					log.Error(nil, "ClusterDeployment handler recieved non-ClusterDeployment object")
					return []reconcile.Request{}
				}
				bmas := &midasv1alpha1.BareMetalAssetList{}
				err := mgr.GetClient().List(context.TODO(), bmas,
					client.MatchingLabels{
						ClusterDeploymentNameLabel:      clusterDeployment.Name,
						ClusterDeploymentNamespaceLabel: clusterDeployment.Namespace,
					})
				if err != nil {
					log.Error(err, "Could not list BareMetalAssets with label %v=%v", ClusterDeploymentNameLabel, clusterDeployment.Name)
				}
				var requests []reconcile.Request
				for _, bma := range bmas.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      bma.Name,
							Namespace: bma.Namespace,
						},
					})
				}
				return requests
			}),
		})

	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileBareMetalAsset implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileBareMetalAsset{}

// ReconcileBareMetalAsset reconciles a BareMetalAsset object
type ReconcileBareMetalAsset struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a BareMetalAsset object and makes changes based on the state read
// and what is in the BareMetalAsset.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileBareMetalAsset) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling BareMetalAsset")

	// Fetch the BareMetalAsset instance
	instance := &midasv1alpha1.BareMetalAsset{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Check DeletionTimestamp to determine if object is under deletion
	if instance.GetDeletionTimestamp().IsZero() {
		if !contains(instance.GetFinalizers(), BareMetalAssetFinalizer) {
			reqLogger.Info("Finalizer not found for BareMetalAsset. Adding finalizer")
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, BareMetalAssetFinalizer)
			if err := r.client.Update(context.TODO(), instance); err != nil {
				reqLogger.Error(err, "Failed to add finalizer to baremetalasset")
				return reconcile.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if contains(instance.GetFinalizers(), BareMetalAssetFinalizer) {
			err := r.deleteSyncSet(instance, reqLogger)
			if err != nil {
				return reconcile.Result{}, err
			}
			reqLogger.Info("Removing Finalizer")
			instance.ObjectMeta.Finalizers = remove(instance.ObjectMeta.Finalizers, BareMetalAssetFinalizer)
			if err := r.client.Update(context.TODO(), instance); err != nil {
				reqLogger.Error(err, "Failed to remove finalizer from baremetalasset")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	for _, f := range []func(*midasv1alpha1.BareMetalAsset, logr.Logger) error{
		r.checkAssetSecret,
		r.ensureLabels,
		r.checkClusterDeployment,
		r.ensureHiveSyncSet,
		r.checkHiveSyncSetInstance,
	} {
		err = f(instance, reqLogger)
		if err != nil {
			switch {
			case bmaerrors.IsNoClusterError(err):
				reqLogger.Info("No cluster specified")
				return reconcile.Result{}, r.client.Status().Update(context.TODO(), instance)
			case bmaerrors.IsAssetSecretNotFoundError(err):
				// since we won't be notified when the secret is created, requeue after some time
				reqLogger.Info("Secret not found", "RequeueAfter.Duration", fmt.Sprintf("%v seconds", assetSecretRequeueAfter))
				return reconcile.Result{RequeueAfter: time.Duration(assetSecretRequeueAfter) * time.Second}, r.client.Status().Update(context.TODO(), instance)
			}

			reqLogger.Error(err, "Failed reconcile")
			statusErr := r.client.Status().Update(context.TODO(), instance)
			if statusErr != nil {
				reqLogger.Error(statusErr, "Failed to update status")
			}

			return reconcile.Result{}, err
		}
	}

	reqLogger.Info("Reconciled")
	return reconcile.Result{}, r.client.Status().Update(context.TODO(), instance)
}

// checkAssetSecret verifies that we can find the secret listed in the BareMetalAsset
func (r *ReconcileBareMetalAsset) checkAssetSecret(instance *midasv1alpha1.BareMetalAsset, reqLogger logr.Logger) error {
	secretName := instance.Spec.BMC.CredentialsName

	secret := &corev1.Secret{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: instance.Namespace}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, "Secret not found", "Secret.Name", secretName, "Secret.Namespace", instance.Namespace)
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    midasv1alpha1.ConditionCredentialsFound,
				Status:  corev1.ConditionFalse,
				Reason:  "SecretNotFound",
				Message: err.Error(),
			})
			return bmaerrors.NewAssetSecretNotFoundError(secretName, instance.Namespace)
		}
		return err
	}

	// add secret reference to status
	secretRef, err := reference.GetReference(r.scheme, secret)
	if err != nil {
		reqLogger.Error(err, "Failed to get reference from secret")
		return err
	}
	objectreferencesv1.SetObjectReference(&instance.Status.RelatedObjects, *secretRef)

	// add condition to status
	conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    midasv1alpha1.ConditionCredentialsFound,
		Status:  corev1.ConditionTrue,
		Reason:  "SecretFound",
		Message: fmt.Sprintf("A secret with the name %v in namespace %v was found", secretName, instance.Namespace),
	})

	// Set BaremetalAsset instance as the owner and controller
	if secret.OwnerReferences == nil || len(secret.OwnerReferences) == 0 {
		if err := controllerutil.SetControllerReference(instance, secret, r.scheme); err != nil {
			reqLogger.Error(err, "Failed to set ControllerReference")
			return err
		}
		if err := r.client.Update(context.TODO(), secret); err != nil {
			reqLogger.Error(err, "Failed to update secret with OwnerReferences")
			return err
		}
	}
	return nil
}

func (r *ReconcileBareMetalAsset) ensureLabels(instance *midasv1alpha1.BareMetalAsset, reqLogger logr.Logger) error {
	labels := k8slabels.CloneAndAddLabel(instance.Labels, ClusterDeploymentNameLabel, instance.Spec.ClusterDeployment.Name)
	labels = k8slabels.AddLabel(labels, ClusterDeploymentNamespaceLabel, instance.Spec.ClusterDeployment.Namespace)
	labels = k8slabels.AddLabel(labels, RoleLabel, string(instance.Spec.Role))

	if !reflect.DeepEqual(labels, instance.Labels) {
		instance.Labels = labels
		return r.client.Update(context.TODO(), instance)
	}
	return nil
}

// checkClusterDeployment verifies that we can find the ClusterDeployment specified in the BareMetalAsset
func (r *ReconcileBareMetalAsset) checkClusterDeployment(instance *midasv1alpha1.BareMetalAsset, reqLogger logr.Logger) error {
	clusterDeploymentName := instance.Spec.ClusterDeployment.Name
	clusterDeploymentNamespace := instance.Spec.ClusterDeployment.Namespace

	// if the clusterDeploymentName is not specified, we need to handle the possibility
	// that it has been removed from the spec
	if clusterDeploymentName == "" {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    midasv1alpha1.ConditionClusterDeploymentFound,
			Status:  corev1.ConditionFalse,
			Reason:  "NoneSpecified",
			Message: "No cluster deployment specified",
		})
		conditionsv1.RemoveStatusCondition(&instance.Status.Conditions, midasv1alpha1.ConditionAssetSyncStarted)
		conditionsv1.RemoveStatusCondition(&instance.Status.Conditions, midasv1alpha1.ConditionAssetSyncCompleted)

		// Without a clusterName, we do not know what namespace the syncset is in.
		// Get the syncset from relatedobjects if it exists
		hscRef := corev1.ObjectReference{}
		for _, ro := range instance.Status.RelatedObjects {
			if ro.Name == instance.Name && ro.Kind == "SyncSet" && ro.APIVersion == hivev1.SchemeGroupVersion.String() {
				hscRef = ro
				break
			}
		}
		if hscRef == (corev1.ObjectReference{}) {
			// No syncset found in relatedObjects. Nothing to do.
			return bmaerrors.NewNoClusterError()
		}

		// If clusterName is not specified, delete the syncset if it exists
		reqLogger.Info("Cleaning up Hive SyncSet", "SyncSet.Name", hscRef.Name, "SyncSet.Namespace", hscRef.Namespace)
		err := r.client.Delete(context.TODO(), &hivev1.SyncSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hscRef.Namespace,
				Name:      hscRef.Name,
			},
		})
		if err != nil {
			if !errors.IsNotFound(err) {
				reqLogger.Error(err, "Failed to delete Hive SyncSet")
				return err
			}
		}

		// Remove SyncSet from related objects
		objectreferencesv1.RemoveObjectReference(&instance.Status.RelatedObjects, hscRef)
		return bmaerrors.NewNoClusterError()
	}

	// If a clusterDeployment is specified, we need to find it
	cd := &hivev1.ClusterDeployment{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: clusterDeploymentName, Namespace: clusterDeploymentNamespace}, cd)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, "ClusterDeployment not found", "ClusterDeployment.Name", clusterDeploymentName, "ClusterDeployment.Namespace", clusterDeploymentNamespace)
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    midasv1alpha1.ConditionClusterDeploymentFound,
				Status:  corev1.ConditionFalse,
				Reason:  "ClusterDeploymentNotFound",
				Message: err.Error(),
			})
			return err
		}
		return err
	}

	// add condition
	conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    midasv1alpha1.ConditionClusterDeploymentFound,
		Status:  corev1.ConditionTrue,
		Reason:  "ClusterDeploymentFound",
		Message: fmt.Sprintf("A ClusterDeployment with the name %v in namespace %v was found", cd.Name, cd.Namespace),
	})

	return nil
}

func (r *ReconcileBareMetalAsset) ensureHiveSyncSet(instance *midasv1alpha1.BareMetalAsset, reqLogger logr.Logger) error {
	hsc := r.newHiveSyncSet(instance, reqLogger)
	found := &hivev1.SyncSet{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: hsc.Name, Namespace: hsc.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			err := r.client.Create(context.TODO(), hsc)
			if err != nil {
				reqLogger.Error(err, "Failed to create Hive SyncSet")
				conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
					Type:    midasv1alpha1.ConditionAssetSyncStarted,
					Status:  corev1.ConditionFalse,
					Reason:  "SyncSetCreationFailed",
					Message: "Failed to create SyncSet",
				})
				return err
			}

			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    midasv1alpha1.ConditionAssetSyncStarted,
				Status:  corev1.ConditionTrue,
				Reason:  "SyncSetCreated",
				Message: "SyncSet created successfully",
			})
		}
		// other error. fail reconcile
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    midasv1alpha1.ConditionAssetSyncStarted,
			Status:  corev1.ConditionFalse,
			Reason:  "SyncSetGetFailed",
			Message: "Failed to get SyncSet",
		})
		reqLogger.Error(err, "Failed to get Hive SyncSet", "SyncSet.Name", hsc.Name, "SyncSet.Namespace", hsc.Namespace)
		return err
	}

	// Add SyncSet to related objects
	hscRef, err := reference.GetReference(r.scheme, found)
	if err != nil {
		reqLogger.Error(err, "Failed to get reference from SyncSet")
		return err
	}
	objectreferencesv1.SetObjectReference(&instance.Status.RelatedObjects, *hscRef)

	// Add labels to copy for comparison to minimize updates
	labels := k8slabels.CloneAndAddLabel(found.Labels, ClusterDeploymentNameLabel, instance.Spec.ClusterDeployment.Name)
	labels = k8slabels.AddLabel(labels, ClusterDeploymentNamespaceLabel, instance.Spec.ClusterDeployment.Namespace)
	labels = k8slabels.AddLabel(labels, RoleLabel, string(instance.Spec.Role))

	// Update Hive SyncSet CR if it is not in the desired state
	if !reflect.DeepEqual(hsc.Spec, found.Spec) || !reflect.DeepEqual(labels, found.Labels) {
		reqLogger.Info("Updating Hive SyncSet", "SyncSet.Name", hsc.Name, "SyncSet.Namespace", hsc.Namespace)
		found.Spec = hsc.Spec
		found.Labels = labels
		err := r.client.Update(context.TODO(), found)
		if err != nil {
			reqLogger.Error(err, "Failed to update Hive SyncSet", "SyncSet.Name", hsc.Name, "SyncSet.Namespace", hsc.Namespace)
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    midasv1alpha1.ConditionAssetSyncStarted,
				Status:  corev1.ConditionFalse,
				Reason:  "SyncSetUpdateFailed",
				Message: "Failed to update SyncSet",
			})
			return err
		}
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    midasv1alpha1.ConditionAssetSyncStarted,
			Status:  corev1.ConditionTrue,
			Reason:  "SyncSetUpdated",
			Message: "SyncSet updated successfully",
		})
	}
	return nil
}

func (r *ReconcileBareMetalAsset) newHiveSyncSet(instance *midasv1alpha1.BareMetalAsset, reqLogger logr.Logger) *hivev1.SyncSet {
	bmhJSON, err := json.Marshal(r.newBareMetalHost(instance, reqLogger))
	if err != nil {
		reqLogger.Error(err, "Error marshaling baremetalhost")
		return nil
	}

	hsc := &hivev1.SyncSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SyncSet",
			APIVersion: "hive.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Spec.ClusterDeployment.Namespace, // syncset should be created in the same namespace as the clusterdeployment
			Labels: map[string]string{
				ClusterDeploymentNameLabel:      instance.Spec.ClusterDeployment.Name,
				ClusterDeploymentNamespaceLabel: instance.Spec.ClusterDeployment.Namespace,
				RoleLabel:                       string(instance.Spec.Role),
			},
		},
		Spec: hivev1.SyncSetSpec{
			SyncSetCommonSpec: hivev1.SyncSetCommonSpec{
				Resources: []runtime.RawExtension{
					{
						Raw: bmhJSON,
					},
				},
				Patches:           []hivev1.SyncObjectPatch{},
				ResourceApplyMode: hivev1.SyncResourceApplyMode,
				Secrets: []hivev1.SecretMapping{
					hivev1.SecretMapping{
						SourceRef: hivev1.SecretReference{
							Name:      instance.Spec.BMC.CredentialsName,
							Namespace: instance.Namespace,
						},
						TargetRef: hivev1.SecretReference{
							Name:      instance.Spec.BMC.CredentialsName,
							Namespace: midasv1alpha1.ManagedClusterResourceNamespace,
						},
					},
				},
			},
			ClusterDeploymentRefs: []corev1.LocalObjectReference{
				{
					Name: instance.Spec.ClusterDeployment.Name,
				},
			},
		},
	}
	return hsc
}

func (r *ReconcileBareMetalAsset) newBareMetalHost(instance *midasv1alpha1.BareMetalAsset, reqLogger logr.Logger) *metal3v1alpha1.BareMetalHost {
	bmh := &metal3v1alpha1.BareMetalHost{
		TypeMeta: metav1.TypeMeta{
			Kind:       BareMetalHostKind,
			APIVersion: metal3v1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: midasv1alpha1.ManagedClusterResourceNamespace,
			Labels: map[string]string{
				ClusterDeploymentNameLabel:      instance.Spec.ClusterDeployment.Name,
				ClusterDeploymentNamespaceLabel: instance.Spec.ClusterDeployment.Namespace,
				RoleLabel:                       string(instance.Spec.Role),
			},
		},
		Spec: metal3v1alpha1.BareMetalHostSpec{
			BMC: metal3v1alpha1.BMCDetails{
				Address:         instance.Spec.BMC.Address,
				CredentialsName: instance.Spec.BMC.CredentialsName,
			},
			HardwareProfile: instance.Spec.HardwareProfile,
			BootMACAddress:  instance.Spec.BootMACAddress,
		},
	}
	return bmh
}

func (r *ReconcileBareMetalAsset) checkHiveSyncSetInstance(instance *midasv1alpha1.BareMetalAsset, reqLogger logr.Logger) error {
	found := &hivev1.SyncSetInstanceList{}

	err := r.client.List(context.TODO(), found, client.MatchingLabels{hiveconstants.SyncSetNameLabel: instance.Name})
	if err != nil {
		reqLogger.Error(err, "Problem getting Hive SyncSetInstanceList", "Label.Key", hiveconstants.SyncSetNameLabel, "Label.Value", instance.Name)
		return err
	}

	switch len(found.Items) {
	case 0:
		err = fmt.Errorf("No SyncSetInstances with label name %v and label value %v found", hiveconstants.SyncSetNameLabel, instance.Name)
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    midasv1alpha1.ConditionAssetSyncCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  "SyncSetInstanceNotFound",
			Message: err.Error(),
		})
		return err
	case 1:
		resourceCount := len(found.Items[0].Status.Resources)
		if resourceCount != 1 {
			err = fmt.Errorf("Unexpected number of resources found on SyncSetInstance status. Expected (%v) Found (%v)", 1, resourceCount)
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    midasv1alpha1.ConditionAssetSyncCompleted,
				Status:  corev1.ConditionFalse,
				Reason:  "UnexpectedResourceCount",
				Message: err.Error(),
			})
			return err
		}
		res := found.Items[0].Status.Resources[0]
		if res.APIVersion != metal3v1alpha1.SchemeGroupVersion.String() || res.Kind != BareMetalHostKind {
			err = fmt.Errorf("Unexpected resource found in SyncSetInstance status. Expected (Kind: %v APIVersion: %v) Found (Kind: %v APIVersion: %v)", BareMetalHostKind, metal3v1alpha1.SchemeGroupVersion.String(), res.Kind, res.APIVersion)
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    midasv1alpha1.ConditionAssetSyncCompleted,
				Status:  corev1.ConditionFalse,
				Reason:  "BareMetalHostResourceNotFound",
				Message: err.Error(),
			})
			return err
		}
		for _, condition := range res.Conditions {
			switch condition.Type {
			case hivev1.ApplySuccessSyncCondition:
				conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
					Type:    midasv1alpha1.ConditionAssetSyncCompleted,
					Status:  condition.Status,
					Reason:  condition.Reason,
					Message: condition.Message,
				})
			case hivev1.ApplyFailureSyncCondition:
				conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
					Type:    midasv1alpha1.ConditionAssetSyncCompleted,
					Status:  corev1.ConditionFalse,
					Reason:  condition.Reason,
					Message: condition.Message,
				})
				return fmt.Errorf("SyncSetInstance resource %v failed with message %v", res.Name, condition.Message)
			}
		}

		secretsCount := len(found.Items[0].Status.Secrets)
		if secretsCount != 1 {
			err = fmt.Errorf("Unexpected number of secrets found on SyncSetInstance. Expected: (%v) Actual: (%v)", 1, secretsCount)
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    midasv1alpha1.ConditionAssetSyncCompleted,
				Status:  corev1.ConditionFalse,
				Reason:  "UnexpectedSecretCount",
				Message: err.Error(),
			})
			return err
		}
		secret := found.Items[0].Status.Secrets[0]
		for _, condition := range secret.Conditions {
			switch condition.Type {
			case hivev1.ApplySuccessSyncCondition:
				conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
					Type:    midasv1alpha1.ConditionAssetSyncCompleted,
					Status:  condition.Status,
					Reason:  condition.Reason,
					Message: condition.Message,
				})
			case hivev1.ApplyFailureSyncCondition:
				conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
					Type:    midasv1alpha1.ConditionAssetSyncCompleted,
					Status:  corev1.ConditionFalse,
					Reason:  condition.Reason,
					Message: condition.Message,
				})
				return fmt.Errorf("SyncSetInstance resource %v failed with message %v", res.Name, condition.Message)
			}
		}
	default:
		err = fmt.Errorf("Found multiple Hive SyncSetInstances with same label")
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    midasv1alpha1.ConditionAssetSyncCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  "MultipleSyncSetInstancesFound",
			Message: err.Error(),
		})
		return err
	}
	return nil
}

func (r *ReconcileBareMetalAsset) deleteSyncSet(instance *midasv1alpha1.BareMetalAsset, reqLogger logr.Logger) error {
	if instance.Spec.ClusterDeployment.Namespace == "" {
		return nil
	}
	err := r.client.Delete(context.TODO(), &hivev1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Spec.ClusterDeployment.Namespace,
		},
	})
	if err != nil {
		if !errors.IsNotFound(err) {
			reqLogger.Error(err, "Failed to delete Hive SyncSet")
			return err
		}
	}
	return nil
}

// Checks whether a string is contained within a slice
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// Removes a given string from a slice and returns the new slice
func remove(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
