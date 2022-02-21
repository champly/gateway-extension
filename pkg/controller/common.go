package controller

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
)

var (
	// TODO: support multi-domain
	svcDomainAnnotationKey    = "ingressDomain0"
	svcPathAnnotationKey      = "ingressPath"
	validationExistAnnotation = []string{
		svcDomainAnnotationKey,
		svcPathAnnotationKey,
	}

	ingressClassName            = "apisix"
	ingressDomainAnnotationsKey = "symcn.gateway.extension/domain"
	ingressBackendName          = "http"
)

func convertToSvc(obj interface{}) (*corev1.Service, bool) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		// don't exec this
		klog.Warningf("receive not *corev1.Service: %+v", obj)
		return nil, false
	}

	if !validationSvc(svc) {
		klog.Warningf("svc validation failed.")
		return nil, false
	}

	return svc, true
}

func validationSvc(svc *corev1.Service) bool {
	// check lables
	if len(svc.Annotations) < 1 {
		klog.Errorf("Validation %s/%s failed: service annotation is empty", svc.Namespace, svc.Name)
		return false
	}
	for _, k := range validationExistAnnotation {
		if v, ok := svc.Annotations[k]; !ok || v == "" {
			klog.Errorf("Validation %s/%s failed: annotation.%s not exist or is empty", svc.Namespace, svc.Name, k)
			return false
		}
	}

	// check domain as name
	errs := validation.IsDNS1123Label(getIngressName(svc))
	if len(errs) > 0 {
		klog.Errorf("Validation %s/%s failed: labels.%s=%s as ingress name invalid:%v", svc.Namespace, svc.Name, svcDomainAnnotationKey, svc.Annotations[svcDomainAnnotationKey], errs)
		return false
	}

	// check ports
	if len(svc.Spec.Ports) < 1 {
		klog.Errorf("Validation %s/%s failed: service ports is empty", svc.Namespace, svc.Name)
		return false
	}

	return true
}

func getIngressName(svc *corev1.Service) string {
	return strings.ReplaceAll(svc.Annotations[svcDomainAnnotationKey], ".", "-")
}

func getIngressHost(svc *corev1.Service) string {
	return svc.Annotations[svcDomainAnnotationKey]
}

func getIngressPath(svc *corev1.Service) string {
	// Paths must begin with a '/'.
	path := svc.Annotations[svcPathAnnotationKey]
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func buildNewIngressWithSvc(svc *corev1.Service) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getIngressName(svc),
			Namespace: svc.Namespace,
			Annotations: map[string]string{
				v1beta1.AnnotationIngressClass: ingressClassName,
				// "k8s.apisix.apache.org/rewrite-target": "/",
				// "k8s.apisix.apache.org/http-to-https":  "true",
				ingressDomainAnnotationsKey: getIngressHost(svc),
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host:             getIngressHost(svc),
					IngressRuleValue: buildIngressRuleValue(svc),
				},
			},
		},
	}
}

func ingressAddPath(ingress *networkingv1.Ingress, svc *corev1.Service) {
	if len(ingress.Spec.Rules) == 0 {
		ingress.Spec.Rules = []networkingv1.IngressRule{
			{
				Host:             getIngressHost(svc),
				IngressRuleValue: buildIngressRuleValue(svc),
			},
		}
		return
	}
	if ingress.Spec.Rules[0].Host != getIngressHost(svc) {
		// TODO: single ingress resource support mutli hosts
		return
	}

	path := ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths
	for i := 0; i < len(path); i++ {
		if path[i].Backend.Service.Name == svc.Name {
			// maybe service port name changed
			path[i] = buildHTTPIngressPath(svc)
			return
		}
	}

	// add new path
	path = append(path, buildHTTPIngressPath(svc))
	ingress.Spec.Rules[0].HTTP.Paths = path
}

func ingressRemovePath(ingress *networkingv1.Ingress, svc *corev1.Service) {
	// !import single ingress must have only rules with one hosts
	// TODO: single ingress resource support mutli hosts
	if len(ingress.Spec.Rules) == 0 {
		return
	}
	if ingress.Spec.Rules[0].Host != getIngressHost(svc) {
		return
	}
	if len(ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths) == 0 {
		return
	}

	path := ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths
	index := -1
	for i := 0; i < len(path); i++ {
		if path[i].Backend.Service.Name == svc.Name {
			index = i
			break
		}
	}
	if index == -1 {
		// not found
		return
	}

	// remove element
	path = append(path[:index], path[index+1:]...)
	if len(path) == 0 {
		// remove only one svc
		ingress.Spec.Rules = nil
		return
	}
	ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths = path
	return
}

func buildIngressRuleValue(svc *corev1.Service) networkingv1.IngressRuleValue {
	return networkingv1.IngressRuleValue{
		HTTP: &networkingv1.HTTPIngressRuleValue{
			Paths: []networkingv1.HTTPIngressPath{
				buildHTTPIngressPath(svc),
			},
		},
	}
}

func buildHTTPIngressPath(svc *corev1.Service) networkingv1.HTTPIngressPath {
	ingressPathType := networkingv1.PathType("Prefix")
	return networkingv1.HTTPIngressPath{
		Path:     getIngressPath(svc),
		PathType: &ingressPathType,
		Backend: networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: svc.Name,
				Port: getIngresssServiceBackend(svc),
			},
		},
	}
}

func getIngresssServiceBackend(svc *corev1.Service) networkingv1.ServiceBackendPort {
	if svc.Spec.Ports[0].Name == "" {
		return networkingv1.ServiceBackendPort{
			Number: svc.Spec.Ports[0].Port,
		}
	}
	return networkingv1.ServiceBackendPort{
		Name: svc.Spec.Ports[0].Name,
	}
}
