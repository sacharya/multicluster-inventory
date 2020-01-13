package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BareMetalAssetSpec defines the desired state of BareMetalAsset
type BareMetalAssetSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// BareMetalAssetStatus defines the observed state of BareMetalAsset
type BareMetalAssetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BareMetalAsset is the Schema for the baremetalassets API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=baremetalassets,scope=Namespaced
type BareMetalAsset struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BareMetalAssetSpec   `json:"spec,omitempty"`
	Status BareMetalAssetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BareMetalAssetList contains a list of BareMetalAsset
type BareMetalAssetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BareMetalAsset `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BareMetalAsset{}, &BareMetalAssetList{})
}
