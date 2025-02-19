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

package main

import (
	"github.com/gardener/gardener-extensions/controllers/extension-shoot-cert-service/cmd/app"
	"github.com/gardener/gardener-extensions/pkg/controller"
	"github.com/gardener/gardener-extensions/pkg/log"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	controllercmd "github.com/gardener/gardener-extensions/pkg/controller/cmd"
)

func main() {
	runtimelog.SetLogger(log.ZapLogger(false))

	cmd := app.NewServiceControllerCommand(controller.SetupSignalHandlerContext())
	if err := cmd.Execute(); err != nil {
		controllercmd.LogErrAndExit(err, "error executing the main controller command")
	}
}
