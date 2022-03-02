package cacerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmp"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	"knative.dev/pkg/webhook"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	admissionlisters "k8s.io/client-go/listers/admissionregistration/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	pkgreconciler "knative.dev/pkg/reconciler"
	certresources "knative.dev/pkg/webhook/certificates/resources"

	"github.com/zezaeoh/knurse/internal/enum"
)

const (
	initContainerName = "setup-ca-certs"
	caCertsVolumeName = "ca-certs"
	caCertsMountPath  = "/etc/ssl/certs"
)

var (
	universalDeserializer = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
	errMissingNewObject   = errors.New("the new object may not be nil")
	podResource           = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
)

type reconciler struct {
	pkgreconciler.LeaderAwareFuncs

	key  types.NamespacedName
	name string
	path string

	withContext func(context.Context) context.Context

	client       kubernetes.Interface
	mwhlister    admissionlisters.MutatingWebhookConfigurationLister
	secretlister corelisters.SecretLister

	secretName        string
	caCertData        string
	setupCaCertsImage string
}

// Reconcile implements controller.Reconciler
func (ac *reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)

	if !ac.IsLeaderFor(ac.key) {
		return controller.NewSkipKey(key)
	}

	// Look up the webhook secret, and fetch the CA cert bundle.
	secret, err := ac.secretlister.Secrets(system.Namespace()).Get(ac.secretName)
	if err != nil {
		logger.Errorw("Error fetching secret", zap.Error(err))
		return err
	}
	caCert, ok := secret.Data[certresources.CACert]
	if !ok {
		return fmt.Errorf("secret %q is missing %q key", ac.secretName, certresources.CACert)
	}

	// Reconcile the webhook configuration.
	return ac.reconcileMutatingWebhook(ctx, caCert)
}

// Path implements AdmissionController
func (ac *reconciler) Path() string {
	return ac.path
}

// Admit implements AdmissionController
func (ac *reconciler) Admit(ctx context.Context, request *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	if ac.withContext != nil {
		ctx = ac.withContext(ctx)
	}

	logger := logging.FromContext(ctx)
	if request.Resource != podResource {
		logger.Infof("expected resource to be %v", podResource)
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	switch request.Operation {
	case admissionv1.Create:
	default:
		logger.Info("Unhandled webhook operation, letting it through ", request.Operation)
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	raw := request.Object.Raw
	pod := corev1.Pod{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &pod); err != nil {
		reason := fmt.Sprintf("could not deserialize pod object: %v", err)
		logger.Error(reason)
		result := apierrors.NewBadRequest(reason).Status()
		return &admissionv1.AdmissionResponse{
			Result:  &result,
			Allowed: true,
		}
	}

	if pod.Spec.NodeSelector["kubernetes.io/os"] == "windows" {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	patchBytes, err := ac.mutate(ctx, request)
	if err != nil {
		return webhook.MakeErrorStatus("mutation failed: %v", err)
	}

	return &admissionv1.AdmissionResponse{
		Patch:   patchBytes,
		Allowed: true,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

func (ac *reconciler) reconcileMutatingWebhook(ctx context.Context, caCert []byte) error {
	logger := logging.FromContext(ctx)

	configuredWebhook, err := ac.mwhlister.Get(ac.key.Name)
	if err != nil {
		return fmt.Errorf("error retrieving webhook: %w", err)
	}

	current := configuredWebhook.DeepCopy()
	for i, wh := range current.Webhooks {
		if wh.Name != ac.name {
			continue
		}
		cur := &current.Webhooks[i]
		cur.ClientConfig.CABundle = caCert
		if cur.ClientConfig.Service == nil {
			return fmt.Errorf("missing service reference for webhook: %s", wh.Name)
		}
		cur.ClientConfig.Service.Path = ptr.String(ac.Path())
	}

	if ok, err := kmp.SafeEqual(configuredWebhook, current); err != nil {
		return fmt.Errorf("error diffing webhooks: %w", err)
	} else if !ok {
		logger.Info("Updating webhook")
		mwhclient := ac.client.AdmissionregistrationV1().MutatingWebhookConfigurations()
		if _, err := mwhclient.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update webhook: %w", err)
		}
	} else {
		logger.Info("Webhook is valid")
	}
	return nil
}

func (ac *reconciler) mutate(ctx context.Context, req *admissionv1.AdmissionRequest) ([]byte, error) {
	newBytes := req.Object.Raw
	oldBytes := req.OldObject.Raw

	// nil values denote absence of `old` (create) or `new` (delete) objects.
	var oldObj, newObj corev1.Pod

	if len(newBytes) != 0 {
		newDecoder := json.NewDecoder(bytes.NewBuffer(newBytes))
		if err := newDecoder.Decode(&newObj); err != nil {
			return nil, fmt.Errorf("cannot decode incoming new object: %v", err)
		}
	}
	if len(oldBytes) != 0 {
		oldDecoder := json.NewDecoder(bytes.NewBuffer(oldBytes))
		if err := oldDecoder.Decode(&oldObj); err != nil {
			return nil, fmt.Errorf("cannot decode incoming old object: %v", err)
		}
	}

	var patches duck.JSONPatch
	var err error

	if &oldObj != nil {
		if req.SubResource == "" {
			ctx = apis.WithinUpdate(ctx, oldObj)
		} else {
			ctx = apis.WithinSubResourceUpdate(ctx, oldObj, req.SubResource)
		}
	} else {
		ctx = apis.WithinCreate(ctx)
	}
	ctx = apis.WithUserInfo(ctx, &req.UserInfo)

	if patches, err = ac.setInitContainerForCaCerts(ctx, patches, newObj); err != nil {
		return nil, errors.Wrap(err, "failed to set init container for ca certs on pod")
	}
	if &newObj == nil {
		return nil, errMissingNewObject
	}
	return json.Marshal(patches)
}

func (ac *reconciler) setInitContainerForCaCerts(ctx context.Context, patches duck.JSONPatch, pod corev1.Pod) (duck.JSONPatch, error) {
	before, after := pod.DeepCopyObject(), pod
	ac.setCaCerts(ctx, &after)

	patch, err := duck.CreatePatch(before, after)
	if err != nil {
		return nil, err
	}

	return append(patches, patch...), nil
}

func (ac *reconciler) setCaCerts(ctx context.Context, obj *corev1.Pod) {
	if ac.caCertData == "" {
		return
	}

	volume := corev1.Volume{
		Name: caCertsVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	obj.Spec.Volumes = append(obj.Spec.Volumes, volume)

	mount := corev1.VolumeMount{
		Name:      caCertsVolumeName,
		MountPath: caCertsMountPath,
		ReadOnly:  true,
	}
	for i := range obj.Spec.InitContainers {
		obj.Spec.InitContainers[i].VolumeMounts = append(obj.Spec.InitContainers[i].VolumeMounts, mount)
	}
	for i := range obj.Spec.Containers {
		obj.Spec.Containers[i].VolumeMounts = append(obj.Spec.Containers[i].VolumeMounts, mount)
	}

	container := corev1.Container{
		Name:  initContainerName,
		Image: ac.setupCaCertsImage,
		Env: []corev1.EnvVar{
			{
				Name:  enum.SETUP_CA_CERT_DATA,
				Value: ac.caCertData,
			},
		},
		ImagePullPolicy: corev1.PullIfNotPresent,
		WorkingDir:      enum.SETUP_WORKSPACE,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      caCertsVolumeName,
				MountPath: enum.SETUP_WORKSPACE,
			},
		},
	}
	obj.Spec.InitContainers = append([]corev1.Container{container}, obj.Spec.InitContainers...)
}
