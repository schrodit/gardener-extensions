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

package infrastructure

import (
	"encoding/json"

	openstackv1alpha1 "github.com/gardener/gardener-extensions/controllers/provider-openstack/pkg/apis/openstack/v1alpha1"
	"github.com/gardener/gardener-extensions/controllers/provider-openstack/pkg/internal"
	"github.com/gardener/gardener-extensions/pkg/controller"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = Describe("Terraform", func() {
	var (
		infra                  *extensionsv1alpha1.Infrastructure
		cloudProfileConfig     *openstackv1alpha1.CloudProfileConfig
		cloudProfileConfigJSON []byte
		config                 *openstackv1alpha1.InfrastructureConfig
		cluster                *controller.Cluster
		credentials            *internal.Credentials

		keystoneURL = "foo-bar.com"
		dnsServers  = []string{"a", "b"}
	)

	BeforeEach(func() {
		config = &openstackv1alpha1.InfrastructureConfig{
			Networks: openstackv1alpha1.Networks{
				Router: &openstackv1alpha1.Router{
					ID: "1",
				},
				Worker: "10.1.0.0/16",
			},
		}

		infra = &extensionsv1alpha1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
			},

			Spec: extensionsv1alpha1.InfrastructureSpec{
				Region: "de_1_1",
				SecretRef: corev1.SecretReference{
					Namespace: "foo",
					Name:      "openstack-credentials",
				},
				ProviderConfig: &runtime.RawExtension{
					Object: config,
				},
			},
		}

		podsCIDR := "11.0.0.0/16"
		servicesCIDR := "12.0.0.0/16"

		cloudProfileConfig = &openstackv1alpha1.CloudProfileConfig{
			DNSServers:  dnsServers,
			KeyStoneURL: keystoneURL,
		}
		cloudProfileConfigJSON, _ = json.Marshal(cloudProfileConfig)
		cluster = &controller.Cluster{
			CoreCloudProfile: &gardencorev1alpha1.CloudProfile{
				Spec: gardencorev1alpha1.CloudProfileSpec{
					ProviderConfig: &gardencorev1alpha1.ProviderConfig{
						RawExtension: runtime.RawExtension{
							Raw: cloudProfileConfigJSON,
						},
					},
				},
			},
			CoreShoot: &gardencorev1alpha1.Shoot{
				Spec: gardencorev1alpha1.ShootSpec{
					Networking: gardencorev1alpha1.Networking{
						Pods:     &podsCIDR,
						Services: &servicesCIDR,
					},
				},
			},
		}

		credentials = &internal.Credentials{Username: "user", Password: "secret"}
	})

	Describe("#ComputeTerraformerChartValues", func() {
		It("should correctly compute the terraformer chart values", func() {
			values, err := ComputeTerraformerChartValues(infra, credentials, config, cluster)
			Expect(err).To(BeNil())

			Expect(values).To(Equal(map[string]interface{}{
				"openstack": map[string]interface{}{
					"authURL":          keystoneURL,
					"domainName":       credentials.DomainName,
					"tenantName":       credentials.TenantName,
					"region":           infra.Spec.Region,
					"floatingPoolName": config.FloatingPoolName,
				},
				"create": map[string]interface{}{
					"router": false,
				},
				"dnsServers":   dnsServers,
				"sshPublicKey": string(infra.Spec.SSHPublicKey),
				"router": map[string]interface{}{
					"id": "1",
				},
				"clusterName": infra.Namespace,
				"networks": map[string]interface{}{
					"worker": config.Networks.Worker,
				},
				"outputKeys": map[string]interface{}{
					"routerID":          TerraformOutputKeyRouterID,
					"networkID":         TerraformOutputKeyNetworkID,
					"keyName":           TerraformOutputKeySSHKeyName,
					"securityGroupID":   TerraformOutputKeySecurityGroupID,
					"securityGroupName": TerraformOutputKeySecurityGroupName,
					"floatingNetworkID": TerraformOutputKeyFloatingNetworkID,
					"subnetID":          TerraformOutputKeySubnetID,
				},
			}))
		})

		It("should correctly compute the terraformer chart values with vpc creation", func() {
			config.Networks.Router = nil

			values, err := ComputeTerraformerChartValues(infra, credentials, config, cluster)
			Expect(err).To(BeNil())

			Expect(values).To(Equal(map[string]interface{}{
				"openstack": map[string]interface{}{
					"authURL":          keystoneURL,
					"domainName":       credentials.DomainName,
					"tenantName":       credentials.TenantName,
					"region":           infra.Spec.Region,
					"floatingPoolName": config.FloatingPoolName,
				},
				"create": map[string]interface{}{
					"router": true,
				},
				"dnsServers":   dnsServers,
				"sshPublicKey": string(infra.Spec.SSHPublicKey),
				"router": map[string]interface{}{
					"id": DefaultRouterID,
				},
				"clusterName": infra.Namespace,
				"networks": map[string]interface{}{
					"worker": config.Networks.Worker,
				},
				"outputKeys": map[string]interface{}{
					"routerID":          TerraformOutputKeyRouterID,
					"networkID":         TerraformOutputKeyNetworkID,
					"keyName":           TerraformOutputKeySSHKeyName,
					"securityGroupID":   TerraformOutputKeySecurityGroupID,
					"securityGroupName": TerraformOutputKeySecurityGroupName,
					"floatingNetworkID": TerraformOutputKeyFloatingNetworkID,
					"subnetID":          TerraformOutputKeySubnetID,
				},
			}))
		})
	})

	Describe("#StatusFromTerraformState", func() {
		var (
			SSHKeyName        string
			RouterID          string
			NetworkID         string
			SubnetID          string
			FloatingNetworkID string
			SecurityGroupID   string
			SecurityGroupName string

			state *TerraformState
		)

		BeforeEach(func() {
			SSHKeyName = "my-key"
			RouterID = "111"
			NetworkID = "222"
			SubnetID = "333"
			FloatingNetworkID = "444"
			SecurityGroupID = "555"
			SecurityGroupName = "my-sec-group"

			state = &TerraformState{
				SSHKeyName:        SSHKeyName,
				RouterID:          RouterID,
				NetworkID:         NetworkID,
				SubnetID:          SubnetID,
				FloatingNetworkID: FloatingNetworkID,
				SecurityGroupID:   SecurityGroupID,
				SecurityGroupName: SecurityGroupName,
			}
		})

		It("should correctly compute the status", func() {
			status := StatusFromTerraformState(state)

			Expect(status).To(Equal(&openstackv1alpha1.InfrastructureStatus{
				TypeMeta: metav1.TypeMeta{
					APIVersion: openstackv1alpha1.SchemeGroupVersion.String(),
					Kind:       "InfrastructureStatus",
				},
				Networks: openstackv1alpha1.NetworkStatus{
					ID: state.NetworkID,
					Router: openstackv1alpha1.RouterStatus{
						ID: state.RouterID,
					},
					FloatingPool: openstackv1alpha1.FloatingPoolStatus{
						ID: FloatingNetworkID,
					},
					Subnets: []openstackv1alpha1.Subnet{
						{
							Purpose: openstackv1alpha1.PurposeNodes,
							ID:      state.SubnetID,
						},
					},
				},
				SecurityGroups: []openstackv1alpha1.SecurityGroup{
					{
						Purpose: openstackv1alpha1.PurposeNodes,
						ID:      state.SecurityGroupID,
						Name:    state.SecurityGroupName,
					},
				},
				Node: openstackv1alpha1.NodeStatus{
					KeyName: state.SSHKeyName,
				},
			}))
		})
	})
})
