package controller

import (
	"context"

	"github.com/champly/gateway-extension/pkg/kube"
	"github.com/symcn/api"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

type Controller struct {
	ctx context.Context
	cli api.MingleClient
}

func New(ctx context.Context) (*Controller, error) {
	ctrl := &Controller{
		ctx: ctx,
		cli: kube.ManagerPlaneClusterClient,
	}

	ctrl.cli.AddResourceEventHandler(&corev1.Service{}, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc, ok := convertToSvc(obj)
			if !ok {
				return
			}

			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				return ctrl.AddFunc(svc)
			})
			if err == nil {
				return
			}
			// create failed
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldSvc, ok1 := convertToSvc(oldObj)
			if !ok1 {
				return
			}
			newSvc, ok2 := convertToSvc(newObj)
			if !ok2 {
				return
			}

			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				return ctrl.UpdateFunc(oldSvc, newSvc)
			})
			if err == nil {
				return
			}
			// update failed
		},
		DeleteFunc: func(obj interface{}) {
			svc, ok := convertToSvc(obj)
			if !ok {
				return
			}

			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				return ctrl.DeleteFunc(svc)
			})
			if err == nil {
				return
			}
			// delete failed
		},
	})

	return ctrl, nil
}

func (ctrl *Controller) Start() error {
	return startHTTPServer(ctrl.ctx)
}

func (ctrl *Controller) AddFunc(svc *corev1.Service) error {
	var (
		ingress = &networkingv1.Ingress{}
	)
	err := ctrl.cli.Get(
		types.NamespacedName{Name: getIngressName(svc), Namespace: svc.Namespace},
		ingress,
	)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Get ingress failed:%+v", err)
			return err
		}

		// create ingress
		ingress = buildNewIngressWithSvc(svc)
		err = ctrl.cli.Create(ingress)
		if err != nil {
			klog.Warningf("Create ingress failed: %+v, maybe async create.", err)
			return err
		}
		klog.Infof("Create ingress %s/%s success.", ingress.Namespace, svc.Name)
		return nil
	}

	// update ingress
	newIngress := ingress.DeepCopy()
	ingressAddPath(newIngress, svc)
	if equality.Semantic.DeepEqual(newIngress.Spec, ingress.Spec) {
		// same with old ingress, skip, maybe controller restart
		klog.V(4).Infof("Ingress %s/%s spec not change, skip update.", ingress.Namespace, ingress.Name)
		return nil
	}

	err = ctrl.cli.Update(newIngress)
	if err != nil {
		klog.Errorf("Update ingress %s/%s failed: %+v", ingress.Namespace, ingress.Name, err)
		return err
	}
	klog.Infof("Update ingress %s/%s with domain:%s path:%s", ingress.Namespace, ingress.Name, getIngressHost(svc), getIngressPath(svc))
	return nil
}

func (ctrl *Controller) UpdateFunc(oldSvc, newSvc *corev1.Service) error {
	if oldSvc.Labels[svcDomainAnnotationKey] != newSvc.Labels[svcDomainAnnotationKey] {
		// TODO: support domain change
		klog.Errorf("Not support service.labels[%s] change, must unpublish then publish!", svcDomainAnnotationKey)
		return nil
	}

	var (
		ingress = &networkingv1.Ingress{}
	)
	err := ctrl.cli.Get(
		types.NamespacedName{Name: getIngressName(newSvc), Namespace: newSvc.Namespace},
		ingress,
	)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Get ingress failed:%+v", err)
			return err
		}

		// create ingress
		ingress = buildNewIngressWithSvc(newSvc)
		err = ctrl.cli.Create(ingress)
		if err != nil {
			klog.Warningf("Create ingress failed: %+v, maybe async create.", err)
			return err
		}
		klog.Infof("Create ingress %s/%s success.", ingress.Namespace, ingress.Name)
		return nil
	}

	// update ingress
	newIngress := ingress.DeepCopy()
	ingressRemovePath(newIngress, oldSvc)
	ingressAddPath(newIngress, newSvc)
	if equality.Semantic.DeepEqual(newIngress.Spec, ingress.Spec) {
		// same with old ingress, skip, maybe controller restart
		klog.V(4).Infof("Ingress %s/%s spec not change, skip update.", ingress.Namespace, ingress.Name)
		return nil
	}

	err = ctrl.cli.Update(newIngress)
	if err != nil {
		klog.Errorf("Update ingress %s/%s failed: %+v", ingress.Namespace, ingress.Name, err)
		return err
	}
	klog.Infof("Update ingress %s/%s with domain:%s path:%s to domain:%s path:%s", ingress.Namespace, ingress.Name, getIngressHost(oldSvc), getIngressPath(oldSvc), getIngressHost(newSvc), getIngressPath(newSvc))
	return nil
}

func (ctrl *Controller) DeleteFunc(svc *corev1.Service) error {
	var (
		ingress = &networkingv1.Ingress{}
	)
	err := ctrl.cli.Get(
		types.NamespacedName{Name: getIngressName(svc), Namespace: svc.Namespace},
		ingress,
	)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Get ingress failed:%+v", err)
			return err
		}

		klog.Warningf("Ingress %s/%s is not exist!", svc.Namespace, getIngressName(svc))
		return nil
	}

	// update ingress
	newIngress := ingress.DeepCopy()
	ingressRemovePath(newIngress, svc)
	if len(newIngress.Spec.Rules) == 0 {
		// delete ingress
		err = ctrl.cli.Delete(newIngress)
		if err != nil {
			klog.Errorf("Delete ingress %s/%s failed:%+v", ingress.Namespace, ingress.Name, err)
			return err
		}
		klog.Infof("Delete ingress %s/%s success.", ingress.Namespace, ingress.Name)
		return nil
	}

	if equality.Semantic.DeepEqual(newIngress.Spec, ingress.Spec) {
		klog.V(4).Infof("Ingress %s/%s spec not change, skip update.", ingress.Namespace, ingress.Name)
		return nil
	}

	err = ctrl.cli.Update(newIngress)
	if err != nil {
		klog.Errorf("Update ingress %s/%s failed: %+v", ingress.Namespace, ingress.Name, err)
		return err
	}
	klog.Infof("Remove ingress %s/%s with domain:%s path:%s success", ingress.Namespace, ingress.Name, getIngressHost(svc), getIngressPath(svc))
	return nil
}
