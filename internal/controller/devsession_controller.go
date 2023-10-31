/*
Copyright 2023.

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

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sessionv1 "github.com/autodev-cloud/autodev-operator/api/v1"
)

// DevSessionReconciler reconciles a DevSession object
type DevSessionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=session.autodev,resources=devsessions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=session.autodev,resources=devsessions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=session.autodev,resources=devsessions/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deloyments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deloyments/status,verbs=get
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services/status,verbs=get
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DevSession object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.0/pkg/reconcile
func (r *DevSessionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconcile function was called", "namespacedname", req.NamespacedName)

	// fetch devsession resource
	var devSession sessionv1.DevSession
	if err := r.Get(ctx, req.NamespacedName, &devSession); err != nil {
		logger.Error(err, "failed to fetch devsession", "devsession", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// logger.Info("fetched devsession succesfully", "devSession", devSession)

	var deploy appsv1.Deployment

	// if devsession inactive delete deployment
	if !devSession.Spec.Active {
		logger.Info("devsession is inactive", "devSession", devSession)
		if err := r.Get(ctx, req.NamespacedName, &deploy); err != nil {
			if client.IgnoreNotFound(err) != nil {
				logger.Error(err, "failed to fetch deloyment", "devsession", req.NamespacedName)
				return ctrl.Result{}, err
			}
		} else {
			// delete existing deloyment
			logger.Info("deleting deployment for inactive devsession", "devsession", req.NamespacedName)
			if err := r.Delete(ctx, &deploy); err != nil {
				logger.Error(err, "failed to delete deloyment", "devsession", req.NamespacedName)
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// ensure deployment resource is created
	if err := r.Get(ctx, req.NamespacedName, &deploy); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("deloyment not found", "devsession", req.NamespacedName)
			logger.Info("creating deployment", "devsession", req.NamespacedName)
			replicaCount := int32(1)
			deploy = appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:        req.Name,
					Namespace:   req.Namespace,
					Labels:      map[string]string{"session": req.Name},
					Annotations: map[string]string{"session": req.Name},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicaCount,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"session": req.Name},
					},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"session": req.Name},
						},
						Spec: *devSession.Spec.PodSpec.DeepCopy(),
					},
				},
			}
			if err := ctrl.SetControllerReference(&devSession, &deploy, r.Scheme); err != nil {
				logger.Error(err, "unable to set controller reference", "devsession", req.NamespacedName)
				return ctrl.Result{RequeueAfter: time.Minute}, err
			}

			if err := r.Create(ctx, &deploy); err != nil {
				logger.Error(err, "unable to create deloyment", "devsession", req.NamespacedName)
				return ctrl.Result{}, err
			}
			logger.Info("created deployment for devsession", "devsession", req.NamespacedName)
		} else {
			logger.Error(err, "failed to fetch deloyment", "devsession", req.NamespacedName)
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}

	// update devsession status (fields Ready and ContainerStatuses)
	r.Get(ctx, req.NamespacedName, &deploy)
	devSession.Status.Ready = deploy.Status.ReadyReplicas == 1

	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(req.Namespace), client.MatchingLabels(map[string]string{"session": req.Name})); err != nil {
		logger.Error(err, "failed to fetch pod", "devsession", req.NamespacedName)
		return ctrl.Result{Requeue: true}, client.IgnoreNotFound(err)
	}
	devSession.Status.ContainerStatuses = pods.Items[0].Status.ContainerStatuses
	if err := r.Status().Update(ctx, &devSession); err != nil {
		logger.Error(err, "failed to update devsession status", "devsession", req.NamespacedName)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DevSessionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sessionv1.DevSession{}).
		Complete(r)
}
