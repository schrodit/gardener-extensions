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

package worker

import (
	"context"
	"fmt"
	"path/filepath"

	apisazure "github.com/gardener/gardener-extensions/controllers/provider-azure/pkg/apis/azure"
	azureapi "github.com/gardener/gardener-extensions/controllers/provider-azure/pkg/apis/azure"
	azureapihelper "github.com/gardener/gardener-extensions/controllers/provider-azure/pkg/apis/azure/helper"
	"github.com/gardener/gardener-extensions/controllers/provider-azure/pkg/azure"
	"github.com/gardener/gardener-extensions/controllers/provider-azure/pkg/internal"
	extensionscontroller "github.com/gardener/gardener-extensions/pkg/controller"
	"github.com/gardener/gardener-extensions/pkg/controller/worker"
	"github.com/gardener/gardener-extensions/pkg/util"

	machinev1alpha1 "github.com/gardener/machine-controller-manager/pkg/apis/machine/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

// MachineClassKind yields the name of the AWS machine class.
func (w *workerDelegate) MachineClassKind() string {
	return "AzureMachineClass"
}

// MachineClassList yields a newly initialized AzureMachineClassList object.
func (w *workerDelegate) MachineClassList() runtime.Object {
	return &machinev1alpha1.AzureMachineClassList{}
}

// DeployMachineClasses generates and creates the Azure specific machine classes.
func (w *workerDelegate) DeployMachineClasses(ctx context.Context) error {
	if w.machineClasses == nil {
		if err := w.generateMachineConfig(ctx); err != nil {
			return err
		}
	}
	return w.seedChartApplier.ApplyChart(ctx, filepath.Join(azure.InternalChartsPath, "machineclass"), w.worker.Namespace, "machineclass", map[string]interface{}{"machineClasses": w.machineClasses}, nil)
}

// GenerateMachineDeployments generates the configuration for the desired machine deployments.
func (w *workerDelegate) GenerateMachineDeployments(ctx context.Context) (worker.MachineDeployments, error) {
	if w.machineDeployments == nil {
		if err := w.generateMachineConfig(ctx); err != nil {
			return nil, err
		}
	}
	return w.machineDeployments, nil
}

func (w *workerDelegate) generateMachineClassSecretData(ctx context.Context) (map[string][]byte, error) {
	credentials, err := internal.GetClientAuthData(ctx, w.client, w.worker.Spec.SecretRef)
	if err != nil {
		return nil, err
	}

	return map[string][]byte{
		machinev1alpha1.AzureClientID:       []byte(credentials.ClientID),
		machinev1alpha1.AzureClientSecret:   []byte(credentials.ClientSecret),
		machinev1alpha1.AzureSubscriptionID: []byte(credentials.SubscriptionID),
		machinev1alpha1.AzureTenantID:       []byte(credentials.TenantID),
	}, nil
}

type zoneInfo struct {
	name  string
	index int
	count int
}

func (w *workerDelegate) generateMachineConfig(ctx context.Context) error {
	var (
		machineDeployments   = worker.MachineDeployments{}
		machineClasses       []map[string]interface{}
		machineImages        []apisazure.MachineImage
		nodesAvailabilitySet *azureapi.AvailabilitySet
	)

	machineClassSecretData, err := w.generateMachineClassSecretData(ctx)
	if err != nil {
		return err
	}

	shootVersionMajorMinor, err := util.VersionMajorMinor(extensionscontroller.GetKubernetesVersion(w.cluster))
	if err != nil {
		return err
	}

	infrastructureStatus := &azureapi.InfrastructureStatus{}
	if _, _, err := w.decoder.Decode(w.worker.Spec.InfrastructureProviderStatus.Raw, nil, infrastructureStatus); err != nil {
		return err
	}

	nodesSubnet, err := azureapihelper.FindSubnetByPurpose(infrastructureStatus.Networks.Subnets, azureapi.PurposeNodes)
	if err != nil {
		return err
	}

	// The AvailabilitySet will be only used for non zoned Shoots.
	if !infrastructureStatus.Zoned {
		nodesAvailabilitySet, err = azureapihelper.FindAvailabilitySetByPurpose(infrastructureStatus.AvailabilitySets, azureapi.PurposeNodes)
		if err != nil {
			return err
		}
	}

	for _, pool := range w.worker.Spec.Pools {
		publisher, sku, offer, urn, err := w.findMachineImage(pool.MachineImage.Name, pool.MachineImage.Version)
		if err != nil {
			return err
		}
		machineImages = appendMachineImage(machineImages, apisazure.MachineImage{
			Name:      pool.MachineImage.Name,
			Version:   pool.MachineImage.Version,
			Publisher: publisher,
			SKU:       sku,
			Offer:     offer,
			URN:       urn,
		})

		volumeSize, err := worker.DiskSize(pool.Volume.Size)
		if err != nil {
			return err
		}

		image := map[string]interface{}{
			"publisher": publisher,
			"offer":     offer,
			"sku":       sku,
			"version":   pool.MachineImage.Version,
		}
		if urn != nil {
			image["urn"] = *urn
		}

		generateMachineClassAndDeployment := func(zone *zoneInfo, availabilitySetID *string) (worker.MachineDeployment, map[string]interface{}) {
			var (
				machineDeployment = worker.MachineDeployment{
					Minimum:        pool.Minimum,
					Maximum:        pool.Maximum,
					MaxSurge:       pool.MaxSurge,
					MaxUnavailable: pool.MaxUnavailable,
					Labels:         pool.Labels,
					Annotations:    pool.Annotations,
					Taints:         pool.Taints,
				}

				machineClassSpec = map[string]interface{}{
					"region":        w.worker.Spec.Region,
					"resourceGroup": infrastructureStatus.ResourceGroup.Name,
					"vnetName":      infrastructureStatus.Networks.VNet.Name,
					"subnetName":    nodesSubnet.Name,
					"tags": map[string]interface{}{
						"Name": w.worker.Namespace,
						fmt.Sprintf("kubernetes.io-cluster-%s", w.worker.Namespace): "1",
						"kubernetes.io-role-node":                                   "1",
					},
					"secret": map[string]interface{}{
						"cloudConfig": string(pool.UserData),
					},
					"machineType":  pool.MachineType,
					"image":        image,
					"volumeSize":   volumeSize,
					"sshPublicKey": string(w.worker.Spec.SSHPublicKey),
				}
			)
			if zone != nil {
				machineDeployment.Minimum = worker.DistributeOverZones(zone.index, pool.Minimum, zone.count)
				machineDeployment.Maximum = worker.DistributeOverZones(zone.index, pool.Maximum, zone.count)
				machineDeployment.MaxSurge = worker.DistributePositiveIntOrPercent(zone.index, pool.MaxSurge, zone.count, pool.Maximum)
				machineDeployment.MaxUnavailable = worker.DistributePositiveIntOrPercent(zone.index, pool.MaxUnavailable, zone.count, pool.Minimum)

				machineClassSpec["zone"] = zone.name
			}
			if availabilitySetID != nil {
				machineClassSpec["availabilitySetID"] = *availabilitySetID
			}

			var (
				machineClassSpecHash = worker.MachineClassHash(machineClassSpec, shootVersionMajorMinor)
				deploymentName       = fmt.Sprintf("%s-%s", w.worker.Namespace, pool.Name)
				className            = fmt.Sprintf("%s-%s", deploymentName, machineClassSpecHash)
			)
			if zone != nil {
				deploymentName = fmt.Sprintf("%s-z%s", deploymentName, zone.name)
				className = fmt.Sprintf("%s-z%s", className, zone.name)
			}

			machineDeployment.Name = deploymentName
			machineDeployment.ClassName = className
			machineDeployment.SecretName = className

			machineClassSpec["name"] = className
			machineClassSpec["secret"].(map[string]interface{})[azure.ClientIDKey] = string(machineClassSecretData[machinev1alpha1.AzureClientID])
			machineClassSpec["secret"].(map[string]interface{})[azure.ClientSecretKey] = string(machineClassSecretData[machinev1alpha1.AzureClientSecret])
			machineClassSpec["secret"].(map[string]interface{})[azure.SubscriptionIDKey] = string(machineClassSecretData[machinev1alpha1.AzureSubscriptionID])
			machineClassSpec["secret"].(map[string]interface{})[azure.TenantIDKey] = string(machineClassSecretData[machinev1alpha1.AzureTenantID])

			return machineDeployment, machineClassSpec
		}

		// Availability Set
		if !infrastructureStatus.Zoned {
			machineDeployment, machineClassSpec := generateMachineClassAndDeployment(nil, &nodesAvailabilitySet.ID)
			machineDeployments = append(machineDeployments, machineDeployment)
			machineClasses = append(machineClasses, machineClassSpec)
			continue
		}

		// Availability Zones
		zoneCount := len(pool.Zones)
		for zoneIndex, zone := range pool.Zones {
			info := &zoneInfo{
				name:  zone,
				index: zoneIndex,
				count: zoneCount,
			}

			machineDeployment, machineClassSpec := generateMachineClassAndDeployment(info, nil)
			machineDeployments = append(machineDeployments, machineDeployment)
			machineClasses = append(machineClasses, machineClassSpec)
		}
	}

	w.machineDeployments = machineDeployments
	w.machineClasses = machineClasses
	w.machineImages = machineImages

	return nil
}
