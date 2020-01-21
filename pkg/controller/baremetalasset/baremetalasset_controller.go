package baremetalasset

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	appv1alpha1 "github.com/mhrivnak/multicluster-inventory/pkg/apis/app/v1alpha1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
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
	err = c.Watch(&source.Kind{Type: &appv1alpha1.BareMetalAsset{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Secrets and requeue the owner BareMetalAsset
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appv1alpha1.BareMetalAsset{},
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
	instance := &appv1alpha1.BareMetalAsset{}
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

	// Check if the secret exists
	secretName := instance.Spec.BMC.CredentialsName
	secret := &corev1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: request.Namespace}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, "Secret not found", "Namespace", request.Namespace, "Secret.Name", secretName)
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    appv1alpha1.ConditionCredentialsFound,
				Status:  corev1.ConditionFalse,
				Reason:  "SecretNotFound",
				Message: fmt.Sprintf("A secret with the name %v in namespace %v could not be found", secretName, request.Namespace),
			})
			return reconcile.Result{}, r.client.Status().Update(context.TODO(), instance)
		}
		return reconcile.Result{}, err
	}

	// Turn the secret into a reference we can use in status
	secretRef, err := reference.GetReference(r.scheme, secret)
	if err != nil {
		reqLogger.Error(err, "Failed to get reference from secret")
		return reconcile.Result{}, err
	}

	// Add the condition and relatedObject, but only update the status once
	conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    appv1alpha1.ConditionCredentialsFound,
		Status:  corev1.ConditionTrue,
		Reason:  "SecretFound",
		Message: fmt.Sprintf("A secret with the name %v in namespace %v was found", secretName, request.Namespace),
	})
	objectreferencesv1.SetObjectReference(&instance.Status.RelatedObjects, *secretRef)
	err = r.client.Status().Update(context.TODO(), instance)
	if err != nil {
		reqLogger.Error(err, "Failed to add secret to related objects")
		return reconcile.Result{}, err
	}

	// Set BaremetalAsset instance as the owner and controller
	if secret.OwnerReferences == nil || len(secret.OwnerReferences) == 0 {
		if err := controllerutil.SetControllerReference(instance, secret, r.scheme); err != nil {
			reqLogger.Error(err, "Failed to set ControllerReference")
			return reconcile.Result{}, err
		}
		if err := r.client.Update(context.TODO(), secret); err != nil {
			reqLogger.Error(err, "Failed to update secret with OwnerReferences")
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true}, nil
	}

	err = r.ensureHiveSyncSet(instance, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Reconciled")

	return reconcile.Result{}, nil
}

func (r *ReconcileBareMetalAsset) ensureHiveSyncSet(bma *appv1alpha1.BareMetalAsset, reqLogger logr.Logger) error {
	hsc := r.newHiveSyncSet(bma, reqLogger)

	found := &hivev1.SyncSet{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: hsc.Name, Namespace: bma.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			err := r.client.Create(context.TODO(), hsc)
			if err != nil {
				reqLogger.Error(err, "Failed to create Hive SyncSet")
				return err
			}
		} else {
			// other error. fail reconcile
			reqLogger.Error(err, "Failed to get Hive SyncSet")
			return err
		}
	} else {
		// Update Hive SyncSet CR if it is not in the desired state
		if !reflect.DeepEqual(hsc.Spec, found.Spec) {
			reqLogger.Info("Updating spec for Hive SyncSet")
			found.Spec = hsc.Spec
			err := r.client.Update(context.TODO(), found)
			if err != nil {
				reqLogger.Error(err, "Failed to update Hive SyncSet")
				return err
			}
		}
	}

	return nil
}

func (r *ReconcileBareMetalAsset) newHiveSyncSet(bma *appv1alpha1.BareMetalAsset, reqLogger logr.Logger) *hivev1.SyncSet {
	secretReferences := []hivev1.SecretReference{}
	secretReference := hivev1.SecretReference{
		Source: corev1.ObjectReference{
			Name:      bma.Spec.BMC.CredentialsName,
			Namespace: bma.Namespace,
		},
		Target: corev1.ObjectReference{
			Name:      bma.Spec.BMC.CredentialsName,
			Namespace: bma.Namespace,
		},
	}
	secretReferences = append(secretReferences, secretReference)

	bmhResource, err := json.Marshal(r.newBareMetalHost(bma, reqLogger))
	if err != nil {
		reqLogger.Error(err, "Error marshaling baremetalhost")
		return nil
	}

	hsc := &hivev1.SyncSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SyncSet",
			APIVersion: "hive.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      bma.Name,
			Namespace: bma.Namespace,
			Labels: map[string]string{
				"cluster": bma.Labels["cluster"],
			},
		},
		Spec: hivev1.SyncSetSpec{
			SyncSetCommonSpec: hivev1.SyncSetCommonSpec{
				Resources: []runtime.RawExtension{
					{
						Raw: bmhResource,
					},
				},
				Patches:           []hivev1.SyncObjectPatch{},
				ResourceApplyMode: hivev1.UpsertResourceApplyMode,
				SecretReferences:  secretReferences,
			},
			ClusterDeploymentRefs: []corev1.LocalObjectReference{
				{
					Name: bma.Labels["cluster"],
				},
			},
		},
	}
	return hsc
}

func (r *ReconcileBareMetalAsset) newBareMetalHost(bma *appv1alpha1.BareMetalAsset, reqLogger logr.Logger) *metal3v1alpha1.BareMetalHost {
	bmh := &metal3v1alpha1.BareMetalHost{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BareMetalHost",
			APIVersion: "metal3.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: bma.Name,
			Labels: map[string]string{
				"role": fmt.Sprintf("%v", bma.Spec.Role),
			},
		},
		Spec: metal3v1alpha1.BareMetalHostSpec{
			BMC: metal3v1alpha1.BMCDetails{
				Address:         bma.Spec.BMC.Address,
				CredentialsName: bma.Spec.BMC.CredentialsName,
			},
			HardwareProfile: bma.Spec.HardwareProfile,
			BootMACAddress:  bma.Spec.BootMACAddress,
		},
	}
	return bmh
}
