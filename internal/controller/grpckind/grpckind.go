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

package grpckind

import (
	"context"
	"reflect"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	listServicepb "github.com/ashwinshirva/provider-grpc-server/proto"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/provider-grpc/apis/mygroup/v1alpha1"
	apisv1alpha1 "github.com/crossplane/provider-grpc/apis/v1alpha1"
	"github.com/crossplane/provider-grpc/internal/controller/features"
)

const (
	errNotGrpcKind  = "managed resource is not a GrpcKind custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"

	errNewClient = "cannot create new Service"
)

// A ListService does nothing.
type ListService struct {
	grpcClient listServicepb.ListServiceClient
}

var Address = ":50050"

var (
	newListService = func(creds []byte) (*ListService, error) {
		conn, err := grpc.Dial(Address, grpc.WithInsecure(), grpc.WithBlock())

		if err != nil {
			log.Fatalf("did not connect : %v", err)
		}

		//defer conn.Close()

		c := listServicepb.NewListServiceClient(conn)
		return &ListService{grpcClient: c}, nil
	}
)

// Setup adds a controller that reconciles GrpcKind managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.GrpcKindGroupKind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), apisv1alpha1.StoreConfigGroupVersionKind))
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.GrpcKindGroupVersionKind),
		managed.WithExternalConnecter(&connector{
			kube:         mgr.GetClient(),
			usage:        resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
			newServiceFn: newListService}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&v1alpha1.GrpcKind{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	kube         client.Client
	usage        resource.Tracker
	newServiceFn func(creds []byte) (*ListService, error)
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.GrpcKind)
	if !ok {
		return nil, errors.New(errNotGrpcKind)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cd := pc.Spec.Credentials
	data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}

	svc, err := c.newServiceFn(data)
	if err != nil {
		return nil, errors.Wrap(err, errNewClient)
	}

	return &external{service: svc}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	// A 'client' used to connect to the external resource API. In practice this
	// would be something like an AWS SDK client.
	service *ListService
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	// Check if the managed resource is of expected kind
	cr, ok := mg.(*v1alpha1.GrpcKind)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotGrpcKind)
	}

	// These fmt statements should be removed in the real implementation.
	log.Infof("Observing: %+v...", cr.Spec.ForProvider.Name)

	// Check if external resource exists
	// If managed resource exists and external resource does not exist then mark ResourceExists: false
	// so that crossplane calls the Create() method for that resource
	resp, getErr := c.service.grpcClient.GetList(ctx, &listServicepb.GetListReq{Name: cr.Spec.ForProvider.Name})
	if getErr != nil && strings.Contains(getErr.Error(), "does not exist") {
		log.Error("Observe::External resource does not : ", getErr)
		return managed.ExternalObservation{
			ResourceExists:    false,
			ResourceUpToDate:  true,
			ConnectionDetails: managed.ConnectionDetails{},
		}, nil
	}

	// If the Get()rpc returns status=SUCCESS it means external resource is created and is in ready state
	// So mark the CR status as AVAILABLE
	if resp != nil && resp.Status == "SUCCESS" {
		cr.Status.SetConditions(xpv1.Available())
	}

	// Check if the list has changed
	// If the list has changed return appropriate values in ExternalObservation so that crossplane update method for this resource
	if resp != nil && !reflect.DeepEqual(resp.Items, cr.Spec.ForProvider.ListItems) {
		return managed.ExternalObservation{
			ResourceExists:    true,
			ResourceUpToDate:  false,
			ConnectionDetails: managed.ConnectionDetails{},
		}, nil
	}

	return managed.ExternalObservation{
		// Return false when the external resource does not exist. This lets
		// the managed resource reconciler know that it needs to call Create to
		// (re)create the resource, or that it has successfully been deleted.
		ResourceExists: true,

		// Return false when the external resource exists, but it not up to date
		// with the desired managed resource state. This lets the managed
		// resource reconciler know that it needs to call Update.
		ResourceUpToDate: true,

		// Return any details that may be required to connect to the external
		// resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.GrpcKind)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotGrpcKind)
	}

	log.Infof("Creating: %+v", cr.GetName())

	// Check if description is populated since is optional field
	description := ""
	if cr.Spec.ForProvider.Description != nil {
		*cr.Spec.ForProvider.Description = description
	}

	createResp, err := c.service.grpcClient.CreateList(context.Background(), &listServicepb.CreateListReq{
		Name:        cr.Spec.ForProvider.Name,
		Description: description,
	})

	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		log.Error("Error creating list: ", err)
		return managed.ExternalCreation{
			// Optionally return any details that may be required to connect to the
			// external resource. These will be stored as the connection secret.
			ConnectionDetails: managed.ConnectionDetails{},
		}, nil
	}

	// Set the status (Observation field)
	cr.Status.AtProvider.Status = createResp.Status

	return managed.ExternalCreation{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.GrpcKind)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotGrpcKind)
	}

	log.Infof("Update::Update method called... Updating resource: %+v", cr.GetName())

	_, err := c.service.grpcClient.UpdateListItems(context.Background(), &listServicepb.UpdateListItemsReq{
		Name:     cr.Spec.ForProvider.Name,
		NewItems: cr.Spec.ForProvider.ListItems,
	})
	//fmt.Printf("Update:: Update Status for list %v: %v", cr.Spec.ForProvider.Name, addItemsResp.Status)

	if err != nil {
		log.Infof("Update:: Error updating list %v: %v", cr.Spec.ForProvider.Name, err)
		return managed.ExternalUpdate{
			ConnectionDetails: managed.ConnectionDetails{},
		}, err
	}

	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.GrpcKind)
	if !ok {
		return errors.New(errNotGrpcKind)
	}

	log.Infof("Delete::Deleting: %+v\n", cr.GetName())

	deleteResp, err := c.service.grpcClient.DeleteList(context.Background(), &listServicepb.DeleteListReq{
		Name: cr.Spec.ForProvider.Name,
	})

	if err != nil {
		log.Errorf("Delete:: Error deleting list %v: %v\n", cr.Spec.ForProvider.Name, err)
		return err
	}
	log.Infof("Delete:: Delete Status for list %v: %v\n", cr.Spec.ForProvider.Name, deleteResp.Status)

	return nil
}
