// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package containers_test

import (
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/siderolabs/talos/internal/app/machined/pkg/controllers/ctest"
	"github.com/siderolabs/talos/pkg/machinery/resources/containers"
)

const (
	// netnsSourceContainer is the container whose network namespace is joined.
	netnsSourceContainer = "primary"
	// netnsJoinContainer is the container joining it.
	netnsJoinContainer = "sidecar"
)

// netnsCreatePair creates the source container and a container joining its network namespace.
func (suite *InstanceSuite) netnsCreatePair() {
	suite.createNamedSpec(netnsSourceContainer)
	suite.markNamedImageReady(netnsSourceContainer, testDigest)

	suite.createNamedSpec(netnsJoinContainer, func(spec *containers.ContainerSpecSpec) {
		spec.Network.NetnsContainer = netnsSourceContainer
	})
	suite.markNamedImageReady(netnsJoinContainer, testDigest)
}

// netnsSetInstanceStatus fakes the runtime controller's output for one generation of one container.
func (suite *InstanceSuite) netnsSetInstanceStatus(container string, generation uint64, phase containers.ContainerInstancePhase, pid uint32, finishedAt time.Time) {
	id := containers.InstanceID(container, generation)

	status := containers.NewContainerInstanceStatus(containers.NamespaceName, id)
	status.TypedSpec().ContainerID = container
	status.TypedSpec().Generation = generation
	status.TypedSpec().Phase = phase
	status.TypedSpec().PID = pid
	status.TypedSpec().FinishedAt = finishedAt

	if err := suite.State().Create(suite.Ctx(), status); err == nil {
		return
	}

	ctest.UpdateWithConflicts(suite, status, func(res *containers.ContainerInstanceStatus) error {
		*res.TypedSpec() = *status.TypedSpec()

		return nil
	})
}

// netnsAssertNoInstance asserts that a generation of a named container's instance does not exist.
func (suite *InstanceSuite) netnsAssertNoInstance(container string, generation uint64) {
	ctest.AssertNoResource[*containers.ContainerInstanceSpec](suite, containers.InstanceID(container, generation))
}

// TestNetnsJoinFollowsSource covers the netns sharing lifecycle at the controller level: a joining
// container does not start before its source runs, its instance pins exactly the source generation
// it joined, and a source restart replaces the joined instance into the source's new namespace.
func (suite *InstanceSuite) TestNetnsJoinFollowsSource() {
	suite.netnsCreatePair()

	// The source has no gates, so it starts on its own.
	ctest.AssertResource(suite, containers.InstanceID(netnsSourceContainer, 0),
		func(instance *containers.ContainerInstanceSpec, asrt *assert.Assertions) {
			asrt.Equal(netnsSourceContainer, instance.TypedSpec().ContainerID)
		})

	// The joiner must not: there is no network namespace to join until the source runs.
	suite.tick()
	suite.netnsAssertNoInstance(netnsJoinContainer, 0)

	// With the source running, the joiner starts and pins that exact generation.
	suite.netnsSetInstanceStatus(netnsSourceContainer, 0, containers.ContainerInstancePhaseRunning, 100, time.Time{})

	ctest.AssertResource(suite, containers.InstanceID(netnsJoinContainer, 0),
		func(instance *containers.ContainerInstanceSpec, asrt *assert.Assertions) {
			asrt.Equal(netnsSourceContainer, instance.TypedSpec().Network.NetnsContainer)
			asrt.False(instance.TypedSpec().Network.HostNetwork)
			asrt.Equal(containers.InstanceID(netnsSourceContainer, 0), instance.TypedSpec().NetnsSourceInstance)
		})

	// The source's task exits, which is what a source restart looks like from here: the instance
	// controller replaces its instance with the next generation once the restart interval passes.
	suite.netnsSetInstanceStatus(netnsSourceContainer, 0, containers.ContainerInstancePhaseTerminated, 0, time.Now())

	ctest.AssertResource(suite, containers.InstanceID(netnsSourceContainer, 1),
		func(instance *containers.ContainerInstanceSpec, asrt *assert.Assertions) {
			asrt.Equal(netnsSourceContainer, instance.TypedSpec().ContainerID)
		})

	suite.netnsSetInstanceStatus(netnsSourceContainer, 1, containers.ContainerInstancePhaseRunning, 200, time.Time{})

	// The joiner follows the source into the new namespace, as its own new generation: the netns it
	// had joined died with the source's previous task.
	ctest.AssertResource(suite, containers.InstanceID(netnsJoinContainer, 1),
		func(instance *containers.ContainerInstanceSpec, asrt *assert.Assertions) {
			asrt.Equal(containers.InstanceID(netnsSourceContainer, 1), instance.TypedSpec().NetnsSourceInstance)
		})
}
