/*
Copyright 2022.

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

package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"svc-lb-sync/pkg/utills"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	InfraClient         client.Client
	HostedClient        client.Client
	Scheme              *runtime.Scheme
	ServiceEventChannel chan event.GenericEvent
}

//TODO tests
//TODO logs

//Reconcile gets events from Services from type LoadBalancer from both hosted and infra cluster
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("got:", req.Name, req.Namespace)

	//we are always fetching both SVCs
	infraSvc := &corev1.Service{}
	hostedSvc := &corev1.Service{}
	var err error

	//we check to see whether the object namespace is the namespace this controller is running is
	//if it is we assume the event came from the infra cluster, otherwise from the hosted cluster
	//we also assume there will not be a Service from type LoadBalancer in a namespace called clusters-{CLUSTERNAME}
	//in the hosted cluster, since we will think this event came from the infra cluster
	//this is really unlikely to happen but still needs to be in-mind
	if req.Namespace != utills.GetCurrentNs() {
		infraSvc, hostedSvc, err = r.GetSvcsFromHostedReconclie(ctx, req)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		infraSvc, hostedSvc, err = r.GetSvcsFromInfraReconclie(ctx, req)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	infraSvcMissing := utills.IsObjectMissing(infraSvc)
	hostedSvcMissing := utills.IsObjectMissing(hostedSvc)

	if infraSvcMissing && hostedSvcMissing {
		return ctrl.Result{}, nil
	}

	if infraSvcMissing {
		return r.ReconcileMissingInfra(ctx, hostedSvc)
	}

	if hostedSvcMissing {
		return r.ReconcileMissingHosted(ctx, infraSvc)
	}

	return r.ReconcileNormal(ctx, infraSvc, hostedSvc)

}

//GetSvcsFromInfraReconclie if we reconclie from infra cluster fetching both svcs is easy since we got annotations
//to help us get the source Service on the hosted cluster
func (r *ServiceReconciler) GetSvcsFromInfraReconclie(ctx context.Context, req ctrl.Request) (*corev1.Service, *corev1.Service, error) {
	infraSvc := &corev1.Service{}
	hostedSvc := &corev1.Service{}

	if err := r.InfraClient.Get(ctx, req.NamespacedName, infraSvc); err != nil {
		if !errors.IsNotFound(err) {
			return nil, nil, err
		}
	}

	hostedSvcKey := types.NamespacedName{
		Namespace: infraSvc.GetAnnotations()[utills.SyncSvcNamespaceAnnotation],
		Name:      infraSvc.GetAnnotations()[utills.SyncSvcNameAnnotation],
	}

	if err := r.HostedClient.Get(ctx, hostedSvcKey, hostedSvc); err != nil {
		if !errors.IsNotFound(err) {
			return nil, nil, err
		}
	}

	return infraSvc, hostedSvc, nil
}

//GetSvcsFromHostedReconclie if we reconclie from the hosted cluster is a bit more difficult to track the sync Service
//since we creates it with GenerateName to avoid name collisions to do so we use a helper func findSyncedSvc
//TODO this could maybe be solved by using an indexer
func (r *ServiceReconciler) GetSvcsFromHostedReconclie(ctx context.Context, req ctrl.Request) (*corev1.Service, *corev1.Service, error) {
	infraSvc := &corev1.Service{}
	hostedSvc := &corev1.Service{}

	if err := r.HostedClient.Get(ctx, req.NamespacedName, hostedSvc); err != nil {
		if !errors.IsNotFound(err) {
			return nil, nil, err
		}
	}

	infraSvc, err := r.findSyncedSvc(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return infraSvc, hostedSvc, nil

}

//findSyncedSvc iterates over all synced Services and finds our desired Service using annotations
func (r *ServiceReconciler) findSyncedSvc(ctx context.Context, req ctrl.Request) (*corev1.Service, error) {
	svcList := &corev1.ServiceList{}
	syncedSvc := &corev1.Service{}

	if err := r.InfraClient.List(ctx, svcList); err != nil {
		return nil, err
	}

	for _, svc := range svcList.Items {
		if svc.GetAnnotations()[utills.SyncSvcNamespaceAnnotation] == req.Namespace &&
			svc.GetAnnotations()[utills.SyncSvcNameAnnotation] == req.Name {
			syncedSvc = &svc
		}
	}
	return syncedSvc, nil
}

//ReconcileMissingInfra if sync service is missing, just create it
func (r *ServiceReconciler) ReconcileMissingInfra(ctx context.Context, hostedSvc *corev1.Service) (ctrl.Result, error) {

	clusterGenName, err := utills.GetClusterGenName(ctx, r.InfraClient) //cluster generated name is needed for selectors
	if err != nil {
		return ctrl.Result{}, err
	}
	infraSvc := utills.GetInfraSvcFromHostedSvc(hostedSvc, clusterGenName)

	if err := r.InfraClient.Create(ctx, infraSvc); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

//ReconcileMissingHosted if hosted service is missing someone is probably deleted it
//therefore delete the synced service
func (r *ServiceReconciler) ReconcileMissingHosted(ctx context.Context, infraSvc *corev1.Service) (ctrl.Result, error) {
	if err := r.InfraClient.Delete(ctx, infraSvc); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

//ReconcileNormal sync infra and hosted SVCs
func (r *ServiceReconciler) ReconcileNormal(ctx context.Context, infraSvc *corev1.Service, hostedSvc *corev1.Service) (ctrl.Result, error) {
	//TODO handle hosted cluster svc update
	if len(hostedSvc.Status.LoadBalancer.Ingress) != 0 {
		return ctrl.Result{}, nil
	}
	hostedSvc.Status.LoadBalancer.Ingress = infraSvc.Status.LoadBalancer.Ingress
	if err := r.HostedClient.Status().Update(ctx, hostedSvc); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {

	//we only want Services from type LoadBalancer to enter the reconcile function
	//in the ifra cluster the kube-apiserver is also a service from type LoadBalancer
	//we assume there will be no other Services from type LoadBalancer others then the Services this controller creates
	lbFilterFunc := func(o client.Object) bool {
		svc := o.(*corev1.Service)
		if svc.Spec.Type != "LoadBalancer" || svc.Name == "kube-apiserver" {
			return false
		}
		return true
	}
	//TODO pretty sure the LbFilters could look better
	LbFilters := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return lbFilterFunc(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return lbFilterFunc(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return lbFilterFunc(e.ObjectNew)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return lbFilterFunc(e.Object)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Watches(&source.Channel{
			Source: r.ServiceEventChannel,
		}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(LbFilters).
		Complete(r)
}
