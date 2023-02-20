/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

// GrpcKindParameters are the configurable fields of a GrpcKind.
type GrpcKindParameters struct {
	Name string `json:"name"`
	// +optional
	Description *string `json:"description,omitempty"`
	// +optional
	ListItems []int32 `json:"listItems,omitempty"`
}

// GrpcKindObservation are the observable fields of a GrpcKind.
type GrpcKindObservation struct {
	Status string `json:"status"`
}

// A GrpcKindSpec defines the desired state of a GrpcKind.
type GrpcKindSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       GrpcKindParameters `json:"forProvider"`
}

// A GrpcKindStatus represents the observed state of a GrpcKind.
type GrpcKindStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          GrpcKindObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true

// A GrpcKind is an example API type.
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="EXTERNAL-NAME",type="string",JSONPath=".metadata.annotations.crossplane\\.io/external-name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,grpc}
type GrpcKind struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrpcKindSpec   `json:"spec"`
	Status GrpcKindStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GrpcKindList contains a list of GrpcKind
type GrpcKindList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GrpcKind `json:"items"`
}

// GrpcKind type metadata.
var (
	GrpcKindKind             = reflect.TypeOf(GrpcKind{}).Name()
	GrpcKindGroupKind        = schema.GroupKind{Group: Group, Kind: GrpcKindKind}.String()
	GrpcKindKindAPIVersion   = GrpcKindKind + "." + SchemeGroupVersion.String()
	GrpcKindGroupVersionKind = SchemeGroupVersion.WithKind(GrpcKindKind)
)

func init() {
	SchemeBuilder.Register(&GrpcKind{}, &GrpcKindList{})
}
