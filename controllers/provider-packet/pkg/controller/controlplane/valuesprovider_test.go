// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controlplane

import (
	"context"
	"encoding/json"

	apispacket "github.com/gardener/gardener-extensions/controllers/provider-packet/pkg/apis/packet"
	"github.com/gardener/gardener-extensions/controllers/provider-packet/pkg/packet"
	extensionscontroller "github.com/gardener/gardener-extensions/pkg/controller"
	mockclient "github.com/gardener/gardener-extensions/pkg/mock/controller-runtime/client"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	v1alpha1constants "github.com/gardener/gardener/pkg/apis/core/v1alpha1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	namespace = "test"
)

var _ = Describe("ValuesProvider", func() {
	var (
		ctrl *gomock.Controller

		// Build scheme
		scheme = runtime.NewScheme()
		_      = apispacket.AddToScheme(scheme)

		cp = &extensionsv1alpha1.ControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "control-plane",
				Namespace: namespace,
			},
			Spec: extensionsv1alpha1.ControlPlaneSpec{
				Region: "EWR1",
				SecretRef: corev1.SecretReference{
					Name:      v1alpha1constants.SecretNameCloudProvider,
					Namespace: namespace,
				},
				ProviderConfig: &runtime.RawExtension{
					Raw: encode(&apispacket.ControlPlaneConfig{}),
				},
				InfrastructureProviderStatus: &runtime.RawExtension{
					Raw: encode(&apispacket.InfrastructureStatus{}),
				},
			},
		}

		cidr    = "10.250.0.0/19"
		cluster = &extensionscontroller.Cluster{
			CoreShoot: &gardencorev1alpha1.Shoot{
				Spec: gardencorev1alpha1.ShootSpec{
					Networking: gardencorev1alpha1.Networking{
						Pods: &cidr,
					},
					Kubernetes: gardencorev1alpha1.Kubernetes{
						Version: "1.13.4",
					},
				},
			},
		}

		cpSecretKey = client.ObjectKey{Namespace: namespace, Name: v1alpha1constants.SecretNameCloudProvider}
		cpSecret    = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      v1alpha1constants.SecretNameCloudProvider,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				packet.APIToken:  []byte("foo"),
				packet.ProjectID: []byte("bar"),
			},
		}

		checksums = map[string]string{
			v1alpha1constants.SecretNameCloudProvider: "8bafb35ff1ac60275d62e1cbd495aceb511fb354f74a20f7d06ecb48b3a68432",
			"cloud-controller-manager":                "3d791b164a808638da9a8df03924be2a41e34cd664e42231c00fe369e3588272",
			"csi-attacher":                            "2da58ad61c401a2af779a909d22fb42eed93a1524cbfdab974ceedb413fcb914",
			"csi-provisioner":                         "f75b42d40ab501428c383dfb2336cb1fc892bbee1fc1d739675171e4acc4d911",
		}

		controlPlaneChartValues = map[string]interface{}{
			"packet-cloud-controller-manager": map[string]interface{}{
				"replicas":          1,
				"clusterName":       namespace,
				"kubernetesVersion": "1.13.4",
				"podNetwork":        cidr,
				"podAnnotations": map[string]interface{}{
					"checksum/secret-cloud-controller-manager": "3d791b164a808638da9a8df03924be2a41e34cd664e42231c00fe369e3588272",
					"checksum/secret-cloudprovider":            "8bafb35ff1ac60275d62e1cbd495aceb511fb354f74a20f7d06ecb48b3a68432",
				},
			},
			"csi-packet": map[string]interface{}{
				"replicas":          1,
				"kubernetesVersion": "1.13.4",
				"regionID":          "EWR1",
				"podAnnotations": map[string]interface{}{
					"checksum/secret-csi-attacher":    "2da58ad61c401a2af779a909d22fb42eed93a1524cbfdab974ceedb413fcb914",
					"checksum/secret-csi-provisioner": "f75b42d40ab501428c383dfb2336cb1fc892bbee1fc1d739675171e4acc4d911",
					"checksum/secret-cloudprovider":   "8bafb35ff1ac60275d62e1cbd495aceb511fb354f74a20f7d06ecb48b3a68432",
				},
			},
		}

		controlPlaneShootChartValues = map[string]interface{}{
			"csi-packet": map[string]interface{}{
				"credential": map[string]interface{}{
					"apiToken":  "Zm9v",
					"projectID": "YmFy",
				},
				"kubernetesVersion": "1.13.4",
			},
		}

		logger = log.Log.WithName("test")
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("#GetConfigChartValues", func() {
		It("should return correct config chart values", func() {
			// Create valuesProvider
			vp := NewValuesProvider(logger)
			err := vp.(inject.Scheme).InjectScheme(scheme)
			Expect(err).NotTo(HaveOccurred())

			// Call GetConfigChartValues method and check the result
			values, err := vp.GetConfigChartValues(context.TODO(), cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(BeNil())
		})
	})

	Describe("#GetControlPlaneChartValues", func() {
		It("should return correct control plane chart values", func() {
			// Create valuesProvider
			vp := NewValuesProvider(logger)
			err := vp.(inject.Scheme).InjectScheme(scheme)
			Expect(err).NotTo(HaveOccurred())

			// Call GetControlPlaneChartValues method and check the result
			values, err := vp.GetControlPlaneChartValues(context.TODO(), cp, cluster, checksums, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(controlPlaneChartValues))
		})
	})

	Describe("#GetControlPlaneShootChartValues", func() {
		It("should return correct control plane shoot chart values", func() {
			// Create mock client
			client := mockclient.NewMockClient(ctrl)
			client.EXPECT().Get(context.TODO(), cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

			// Create valuesProvider
			vp := NewValuesProvider(logger)
			err := vp.(inject.Client).InjectClient(client)
			Expect(err).NotTo(HaveOccurred())

			// Call GetControlPlaneChartValues method and check the result
			values, err := vp.GetControlPlaneShootChartValues(context.TODO(), cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(controlPlaneShootChartValues))
		})
	})
})

func encode(obj runtime.Object) []byte {
	data, _ := json.Marshal(obj)
	return data
}

func clientGet(result runtime.Object) interface{} {
	return func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
		switch obj.(type) {
		case *corev1.Secret:
			*obj.(*corev1.Secret) = *result.(*corev1.Secret)
		}
		return nil
	}
}
