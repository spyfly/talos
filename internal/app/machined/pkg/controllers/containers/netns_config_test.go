// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package containers_test

import (
	"github.com/stretchr/testify/assert"

	"github.com/siderolabs/talos/internal/app/machined/pkg/controllers/ctest"
	containercfg "github.com/siderolabs/talos/pkg/machinery/config/types/container"
	"github.com/siderolabs/talos/pkg/machinery/resources/containers"
)

// TestResolvesNetnsContainer checks that a "container:<name>" network mode projects to the
// NetnsContainer field, and that the plain modes leave it empty.
func (suite *ConfigSuite) TestResolvesNetnsContainer() {
	primary := newDoc("primary", "nginx")
	sidecar := newDoc("sidecar", "nginx")
	sidecar.NetworkConfig = &containercfg.ContainerNetwork{NetworkMode: "container:primary"}

	suite.applyContainers(primary, sidecar)

	ctest.AssertResource(suite, "primary", func(spec *containers.ContainerSpec, asrt *assert.Assertions) {
		asrt.False(spec.TypedSpec().Network.HostNetwork)
		asrt.Empty(spec.TypedSpec().Network.NetnsContainer)
	})

	ctest.AssertResource(suite, "sidecar", func(spec *containers.ContainerSpec, asrt *assert.Assertions) {
		asrt.False(spec.TypedSpec().Network.HostNetwork)
		asrt.Equal("primary", spec.TypedSpec().Network.NetnsContainer)
	})
}
