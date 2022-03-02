package cacerts

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pivotal/kpack/pkg/reconciler/testhelpers"
	"github.com/sclevine/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"knative.dev/pkg/controller"
	pkgreconciler "knative.dev/pkg/reconciler"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
	certresources "knative.dev/pkg/webhook/certificates/resources"
	wtesting "knative.dev/pkg/webhook/testing"
)

func TestReconciler(t *testing.T) {
	spec.Run(t, "Reconciler", testReconciler)
}

func testReconciler(t *testing.T, when spec.G, it spec.S) {
	const (
		name         = "some-webhook"
		caSecretName = "some-secret"
		caCertData = "some-ca-certs-data"
		setupCaCertsImage = "zezaeoh/setup-ca-certs"
	)
	var (
		key = types.NamespacedName{Name: "some-webhook-config"}
		path     = "/some-path"
		certData = []byte("some-cert")
	)

	when("#Reconcile", func() {
		rt := testhelpers.ReconcilerTester(t,
			func(t *testing.T, row *rtesting.TableRow) (controller.Reconciler, rtesting.ActionRecorderList, rtesting.EventList) {
				listers := wtesting.NewListers(row.Objects)
				secretLister := listers.GetSecretLister()
				mwhcLister := listers.GetMutatingWebhookConfigurationLister()

				k8sfakeClient := k8sfake.NewSimpleClientset(listers.GetKubeObjects()...)

				eventRecorder := record.NewFakeRecorder(10)
				actionRecorderList := rtesting.ActionRecorderList{k8sfakeClient}
				eventList := rtesting.EventList{Recorder: eventRecorder}

				r := &reconciler{
					LeaderAwareFuncs: pkgreconciler.LeaderAwareFuncs{
						// Have this reconciler enqueue our singleton whenever it becomes leader.
						PromoteFunc: func(bkt pkgreconciler.Bucket, enq func(pkgreconciler.Bucket, types.NamespacedName)) error {
							enq(bkt, key)
							return nil
						},
					},

					key:  key,
					name: name,
					path: path,

					client:       k8sfakeClient,
					mwhlister:    mwhcLister,
					secretlister: secretLister,

					secretName:        caSecretName,
					caCertData:        caCertData,
					setupCaCertsImage: setupCaCertsImage,
				}
				r.Promote(pkgreconciler.UniversalBucket(), func(pkgreconciler.Bucket, types.NamespacedName) {})

				return r, actionRecorderList, eventList
			})

		it("Updates the webhook config with the ca cert secret", func() {
			caSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caSecretName,
					Namespace: system.Namespace(),
				},
				Data: map[string][]byte{
					certresources.CACert: certData,
				},
			}

			webhookConfig := &admissionregistrationv1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: key.Name,
				},
				Webhooks: []admissionregistrationv1.MutatingWebhook{
					{
						Name: name,
						ClientConfig: admissionregistrationv1.WebhookClientConfig{
							Service: &admissionregistrationv1.ServiceReference{},
						},
					},
				},
			}

			rt.Test(rtesting.TableRow{
				Key: "some-namespace/pod-webhook",
				Objects: []runtime.Object{
					caSecret,
					webhookConfig,
				},
				WantErr: false,
				WantUpdates: []clientgotesting.UpdateActionImpl{
					{
						Object: &admissionregistrationv1.MutatingWebhookConfiguration{
							ObjectMeta: metav1.ObjectMeta{
								Name: key.Name,
							},
							Webhooks: []admissionregistrationv1.MutatingWebhook{
								{
									Name: name,
									ClientConfig: admissionregistrationv1.WebhookClientConfig{
										Service: &admissionregistrationv1.ServiceReference{
											Path: &path,
										},
										CABundle: certData,
									},
								},
							},
						},
					},
				},
			})
		})
	})

	when("#Admit", func() {
		testPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "object-meta",
			},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  "any-init-container",
						Image: "image",
						Env:   nil,
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "any-container",
						Image: "image",
						Env:   nil,
					},
				},
			},
		}

		ctx := context.TODO()

		it("sets the ca certs on all containers on the pod", func() {
			bytes, err := json.Marshal(testPod)
			require.NoError(t, err)

			admissionRequest := &admissionv1.AdmissionRequest{
				Name: "testAdmissionRequest",
				Object: runtime.RawExtension{
					Raw: bytes,
				},
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Version: "v1", Resource: "pods"},
			}

			r := &reconciler{
				LeaderAwareFuncs: pkgreconciler.LeaderAwareFuncs{
					// Have this reconciler enqueue our singleton whenever it becomes leader.
					PromoteFunc: func(bkt pkgreconciler.Bucket, enq func(pkgreconciler.Bucket, types.NamespacedName)) error {
						enq(bkt, key)
						return nil
					},
				},

				key:  key,
				name: name,
				path: path,

				secretName:        caSecretName,
				caCertData:        caCertData,
				setupCaCertsImage: setupCaCertsImage,
			}
			r.Promote(pkgreconciler.UniversalBucket(), func(pkgreconciler.Bucket, types.NamespacedName) {})

			response := r.Admit(ctx, admissionRequest)
			wtesting.ExpectAllowed(t, response)

			var actualPatch []jsonpatch.JsonPatchOperation
			err = json.Unmarshal(response.Patch, &actualPatch)
			require.NoError(t, err)

			expectedJSON := `[
  {
    "op": "add",
    "path": "/spec/volumes",
    "value": [
      {
        "emptyDir": {},
        "name": "ca-certs"
      }
    ]
  },
  {
    "op": "add",
    "path": "/spec/initContainers/1",
    "value": {
      "image": "image",
      "name": "any-init-container",
      "resources": {},
      "volumeMounts": [
        {
          "mountPath": "/etc/ssl/certs",
          "name": "ca-certs",
          "readOnly": true
        }
      ]
    }
  },
  {
    "op": "replace",
    "path": "/spec/initContainers/0/name",
    "value": "setup-ca-certs"
  },
  {
    "op": "replace",
    "path": "/spec/initContainers/0/image",
    "value": "zezaeoh/setup-ca-certs"
  },
  {
    "op": "add",
    "path": "/spec/initContainers/0/workingDir",
    "value": "/workspace"
  },
  {
    "op": "add",
    "path": "/spec/initContainers/0/env",
    "value": [
      {
         "name": "CA_CERTS_DATA",
         "value": "some-ca-certs-data"
      }
    ]
  },
  {
    "op": "add",
    "path": "/spec/initContainers/0/volumeMounts",
    "value": [
      {
        "mountPath": "/workspace",
        "name": "ca-certs"
      }
    ]
  },
  {
    "op": "add",
    "path": "/spec/initContainers/0/imagePullPolicy",
    "value": "IfNotPresent"
  },
  {
    "op": "add",
    "path": "/spec/containers/0/volumeMounts",
    "value": [
      {
        "mountPath": "/etc/ssl/certs",
        "name": "ca-certs",
        "readOnly": true
      }
    ]
  }
]
`
			var expectedPatch []jsonpatch.JsonPatchOperation
			err = json.Unmarshal([]byte(expectedJSON), &expectedPatch)
			require.NoError(t, err)
			assert.ElementsMatch(t, expectedPatch, actualPatch)
		})
	})
}
