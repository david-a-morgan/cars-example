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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	api "example.com/cars/api/v1"
)

// CarReconciler reconciles a Car object
type CarReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Color  string
}

//+kubebuilder:rbac:groups=example.example.com,resources=cars,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=example.example.com,resources=cars/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=example.example.com,resources=cars/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Car object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *CarReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	car := &api.Car{}
	if err := r.Get(ctx, req.NamespacedName, car); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	color, ok := car.Labels["color"]
	if !ok {
		log.WithValues("car", fmt.Sprintf("%s/%s", car.Namespace, car.Name)).Info("missing color")
		return ctrl.Result{}, nil
	}
	log.WithValues("car color", color, "controller color", r.Color).Info("reconciling car")
	if color != r.Color {
		log.Error(nil, "!!!car color does not match controller color!!!")
		return ctrl.Result{}, nil
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: car.Namespace,
			Name:      car.Name,
		},
	}
	if err := ctrlutil.SetControllerReference(car, cm, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	_, err := ctrlutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = car.Labels
		return nil
	})
	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *CarReconciler) SetupWithManager(mgr ctrl.Manager) error {
	prd, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchLabels: map[string]string{"color": r.Color},
	})
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&api.Car{}, builder.WithPredicates(prd)).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
